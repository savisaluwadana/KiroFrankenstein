package sim

import (
	"sort"
	"testing"

	"pgregory.net/rapid"
)

// Feature: parallel-simulation-mvp, Property 39: Same-Sim_Time event ordering
//
// **Validates: Requirements 5.7, 5.8**

// ordPriorities are the engine version 1 priorities in their documented order:
// commands, disruption expiries, activations, then the other events.
var ordPriorities = []uint8{
	PriorityCommandApply, PriorityDisruptionEnd, PriorityDisruptionStart,
	PriorityPurchaseReceipt, PriorityDemandArrivals, PriorityFulfilment,
	PriorityOrderCancel, PriorityAccrual,
}

// ordShift returns a copy of st whose queue looks as if shift unrelated events
// had been pushed (and consumed) before every pending event: all insertion
// sequences move up uniformly.
func ordShift(t *rapid.T, st State, shift uint64) State {
	c := st.clone()
	for i := range c.Pending {
		c.Pending[i].Seq += shift
	}
	c.EventSeq += shift
	if _, err := c.Queue(); err != nil {
		t.Fatalf("shifted queue invalid: %v", err)
	}
	return c
}

// ordObservable hashes the state without the queue insertion sequences, which
// are the only thing that legitimately differs between the two runs.
func ordObservable(st State) [32]byte {
	c := st.clone()
	for i := range c.Pending {
		c.Pending[i].Seq = 0
	}
	c.EventSeq = 0
	return Hash(c)
}

func TestPropSameMinuteOrdering(t *testing.T) {
	t.Run("queue pops by priority then insertion sequence", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			at := SimMinute(rapid.Int64Range(0, 7200).Draw(t, "at"))
			n := rapid.IntRange(1, 40).Draw(t, "n")
			var q EventQueue
			type rec struct {
				prio uint8
				ins  int64
			}
			var want []rec
			// Unrelated events at other minutes must not disturb the order.
			noise := rapid.IntRange(0, 10).Draw(t, "noise")
			for i := 0; i < noise; i++ {
				q.Push(Event{At: at + 1 + SimMinute(i), Priority: PriorityCommandApply, Kind: EvDemandArrivals})
			}
			for i := 0; i < n; i++ {
				p := ordPriorities[rapid.IntRange(0, len(ordPriorities)-1).Draw(t, "prio")]
				q.Push(Event{At: at, Priority: p, Kind: EvPurchaseReceipt, Payload: EventPayload{Ref: int64(i)}})
				want = append(want, rec{p, int64(i)})
			}
			sort.SliceStable(want, func(i, j int) bool { return want[i].prio < want[j].prio })
			for i, w := range want {
				ev, ok := q.Pop()
				if !ok || ev.At != at || ev.Priority != w.prio || ev.Payload.Ref != w.ins {
					t.Fatalf("pop %d = %+v (ok %v), want prio %d insertion %d at %d", i, ev, ok, w.prio, w.ins, at)
				}
			}
		})
	})

	t.Run("coinciding expiry, activation and command are independent of queue history", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			primary := "primary"
			sc := fulfilScenario()
			// Entry 0 expires at minute 240 where entries 1 and 2 activate.
			sc.DisruptionScript = []DisruptionSpec{
				{Kind: "supplier_outage", OnsetTick: 3, DurationTicks: 2, Target: &primary},
				{Kind: "supplier_outage", OnsetTick: 5, DurationTicks: 1, Target: &primary},
				{Kind: "supplier_outage", OnsetTick: 5, DurationTicks: 3, Target: &primary},
			}
			price := rapid.Int64Range(100, 100000).Draw(t, "price")
			shift := rapid.Uint64Range(1, 1000).Draw(t, "shift")
			in := []Input{{
				EffectiveTick: 5, Seq: 1, Kind: InputCommand,
				Command: Command{Kind: CmdSetPrice, SKU: 0, AmountCents: Cents(price)},
			}}

			run := func(shifted bool) State {
				st, err := Init(sc, 7)
				if err != nil {
					t.Fatal(err)
				}
				if shifted {
					st = ordShift(t, st, shift)
				}
				for st.Tick() < 5 {
					var inputs []Input
					if st.Tick() == 4 {
						inputs = in
					}
					st, _, _, err = StepTick(sc, st, inputs)
					if err != nil {
						t.Fatal(err)
					}
				}
				return st
			}
			base, moved := run(false), run(true)

			if ordObservable(base) != ordObservable(moved) {
				t.Fatalf("state after tick 5 depends on queue history (shift %d)", shift)
			}
			if base.Prices[0].PriceCents != Cents(price) {
				t.Fatalf("command not applied: price %d, want %d", base.Prices[0].PriceCents, price)
			}
			// Expiry ran before the activations: entry 0 is gone, 1 and 2 are active.
			if len(base.Disruptions) != 2 {
				t.Fatalf("active disruptions = %+v, want entries 1 and 2", base.Disruptions)
			}
		})
	})
}
