package sim

import (
	"errors"
	"os"
	"testing"
)

func surgeScenario(onset, dur, mag int64) Scenario {
	sc := *validScenario()
	m := Bps(mag)
	sc.DisruptionScript = []DisruptionSpec{{Kind: kindNameDemandSurge, OnsetTick: onset, DurationTicks: dur, MagnitudeBps: &m}}
	return sc
}

// potentialAt returns the Potential_Arrivals of SKU 0 in the step that
// computes the given tick, stepping a fresh run to it.
func potentialRun(t *testing.T, sc Scenario, upto int) (potentials []int64, states []State) {
	t.Helper()
	st := mustInit(t, sc, 7)
	for i := 0; i < upto; i++ {
		var j []Outcome
		st, j = mustStep(t, sc, st)
		for _, o := range j {
			if o.Kind == OutcomeDemand && o.SKU == 0 {
				potentials = append(potentials, o.Qty)
			}
		}
		states = append(states, st)
	}
	return potentials, states
}

func TestDemandSurge(t *testing.T) {
	base := *validScenario()
	surge := surgeScenario(3, 2, 20000)
	pb, _ := potentialRun(t, base, 6)
	ps, states := potentialRun(t, surge, 6)

	// Same demand draws everywhere; only ticks 3 and 4 differ, by about 2x
	// (the severity jitter is at most +/-5% of the magnitude).
	for i := range pb {
		tick := i + 1
		lo, hi := pb[i]*19000/10000-1, pb[i]*21000/10000+1
		switch {
		case tick == 3 || tick == 4:
			if ps[i] < lo || ps[i] > hi {
				t.Errorf("tick %d potential %d, want about 2x of %d", tick, ps[i], pb[i])
			}
		case ps[i] != pb[i]:
			t.Errorf("tick %d potential %d, want unchanged %d", tick, ps[i], pb[i])
		}
	}
	// Active from the onset Tick through its last Tick, gone afterwards.
	if len(states[1].Disruptions) != 0 {
		t.Errorf("tick 2 has disruptions %+v", states[1].Disruptions)
	}
	if len(states[2].Disruptions) != 1 {
		t.Fatalf("tick 3 disruptions = %+v", states[2].Disruptions)
	}
	if m := states[2].Disruptions[0].MagnitudeBps; m < 19000 || m > 21000 {
		t.Errorf("jittered magnitude %d outside 19000..21000", m)
	}
	if len(states[4].Disruptions) != 0 {
		t.Errorf("tick 5 has disruptions %+v", states[4].Disruptions)
	}
}

func TestWorkforceAbsence(t *testing.T) {
	sc := *validScenario()
	m := Bps(5000)
	sc.DisruptionScript = []DisruptionSpec{{Kind: kindNameWorkforceAbsence, OnsetTick: 2, DurationTicks: 2, MagnitudeBps: &m}}
	_, states := potentialRun(t, sc, 5)
	d := states[1].Disruptions
	if len(d) != 1 || d[0].Kind != DisruptionWorkforceAbsence {
		t.Fatalf("tick 2 disruptions = %+v", d)
	}
	if d[0].MagnitudeBps < 4750 || d[0].MagnitudeBps > 5250 {
		t.Errorf("jittered magnitude %d outside 4750..5250", d[0].MagnitudeBps)
	}
	// The absence applies during its ticks (minutes 60..179), then ends.
	want := int64(10) - RoundHalfEven(10*int64(d[0].MagnitudeBps), BpsScale)
	if got := regularOnShift(&states[1], 60); got != want {
		t.Errorf("on shift = %d, want %d", got, want)
	}
	if got := regularOnShift(&states[1], 180); got != 10 {
		t.Errorf("after the absence on shift = %d, want 10", got)
	}

	// Half-to-even: 5 workers, 50% absent is 2.5 -> 2 absent, 3 on shift;
	// 7 workers, 50% is 3.5 -> 4 absent, 3 on shift.
	for _, c := range []struct{ regular, onShift int64 }{{5, 3}, {7, 3}} {
		st := State{Workforce: Workforce{Regular: c.regular}, Disruptions: []ActiveDisruption{
			{Kind: DisruptionWorkforceAbsence, Onset: 0, End: 60, MagnitudeBps: 5000, Target: -1}}}
		if got := regularOnShift(&st, 0); got != c.onShift {
			t.Errorf("regular %d: on shift %d, want %d", c.regular, got, c.onShift)
		}
	}
	// Jitter keeps an absence share within 0..10000.
	for draw := uint64(0); draw <= 2*SeverityJitterBps; draw++ {
		if got := jitterMagnitude(DisruptionWorkforceAbsence, BpsScale, draw); got > BpsScale {
			t.Fatalf("draw %d: absence %d exceeds 10000", draw, got)
		}
		if got := jitterMagnitude(DisruptionWorkforceAbsence, 0, draw); got != 0 {
			t.Fatalf("draw %d: zero absence became %d", draw, got)
		}
	}
	if got := jitterMagnitude(DisruptionSupplierOutage, 0, 123); got != 0 {
		t.Errorf("outage magnitude = %d", got)
	}
}

func dstream(st State) StreamState { return streamState(st, StreamDisruption) }

func TestInjectedDisruption(t *testing.T) {
	sc := *validScenario()
	m := Bps(15000)
	sc.DisruptionScript = []DisruptionSpec{{Kind: kindNameDemandSurge, OnsetTick: 3, DurationTicks: 2, MagnitudeBps: &m}}
	inj := func(seq uint64, tick Tick, d DisruptionInput) Input {
		return Input{EffectiveTick: tick, Seq: seq, Kind: InputDisruption, Disruption: d}
	}

	t.Run("ordering after script entries", func(t *testing.T) {
		st := mustInit(t, sc, 1)
		q, err := st.Queue()
		if err != nil {
			t.Fatal(err)
		}
		// StepTick pushes in ascending sequence; push 4 then 9.
		for _, seq := range []uint64{4, 9} {
			if err := scheduleInjected(&sc, q, 120, inj(seq, 3, DisruptionInput{Kind: DisruptionDemandSurge, Target: -1, DurationTicks: 1, MagnitudeBps: 12000})); err != nil {
				t.Fatal(err)
			}
		}
		var order []int64
		for {
			ev, ok := q.Peek()
			if !ok || ev.At > 120 {
				break
			}
			q.Pop()
			if ev.Kind == EvDisruptionOnset && ev.At == 120 {
				order = append(order, ev.Payload.Ref)
			}
		}
		if len(order) != 3 || order[0] != 0 || order[1] != 4 || order[2] != 9 {
			t.Errorf("activation order = %v, want script 0 then injected 4, 9", order)
		}

		// Input order in the slice does not matter: StepTick sorts by sequence.
		a := inj(9, 3, DisruptionInput{Kind: DisruptionWorkforceAbsence, Target: -1, DurationTicks: 2, MagnitudeBps: 2000})
		b := inj(4, 3, DisruptionInput{Kind: DisruptionDemandSurge, Target: -1, DurationTicks: 1, MagnitudeBps: 12000})
		s2 := mustStepTo(t, sc, mustInit(t, sc, 1), 2)
		x, _, _, err := StepTick(sc, s2, []Input{a, b})
		if err != nil {
			t.Fatal(err)
		}
		y, _, _, err := StepTick(sc, s2, []Input{b, a})
		if err != nil {
			t.Fatal(err)
		}
		if Hash(x) != Hash(y) {
			t.Error("input slice order changed the hash")
		}
		var got []uint64
		for _, d := range x.Disruptions {
			got = append(got, uint64(d.Origin)<<32|d.InputSeq)
		}
		if len(got) != 3 || got[0] != 0 || got[1] != 1<<32|4 || got[2] != 1<<32|9 {
			t.Errorf("active disruptions (origin<<32|seq) = %v", got)
		}
	})

	t.Run("fixed draw count per tick", func(t *testing.T) {
		plain := mustInit(t, sc, 5)
		noScript := sc
		noScript.DisruptionScript = []DisruptionSpec{}
		quiet := mustInit(t, noScript, 5)
		withInj := mustInit(t, sc, 5)
		for tick := Tick(1); tick <= 8; tick++ {
			var in []Input
			if tick == 4 {
				in = []Input{inj(1, 4, DisruptionInput{Kind: DisruptionSupplierOutage, Target: -1, Role: SupplierPrimary, DurationTicks: 2})}
			}
			var err error
			plain, _, _, err = StepTick(sc, plain, nil)
			if err != nil {
				t.Fatal(err)
			}
			quiet, _, _, err = StepTick(noScript, quiet, nil)
			if err != nil {
				t.Fatal(err)
			}
			withInj, _, _, err = StepTick(sc, withInj, in)
			if err != nil {
				t.Fatal(err)
			}
			if dstream(plain) != dstream(quiet) || dstream(plain) != dstream(withInj) {
				t.Fatalf("tick %d: disruption stream positions differ with/without disruptions", tick)
			}
			if streamState(plain, StreamDemand) != streamState(withInj, StreamDemand) {
				t.Fatalf("tick %d: injection shifted the demand stream", tick)
			}
		}
		fresh := NewStream(5, StreamDisruption)
		for i := 0; i < 8*DisruptionDrawsPerTick; i++ {
			fresh.Next()
		}
		if fresh.State() != dstream(plain) {
			t.Error("disruption stream did not advance by exactly DisruptionDrawsPerTick per tick")
		}
	})

	t.Run("injected outage starts, expires, opens a window", func(t *testing.T) {
		st := mustStepTo(t, sc, mustInit(t, sc, 1), 3)
		step, err := Advance(sc, st, []Input{inj(1, 4, DisruptionInput{Kind: DisruptionSupplierOutage, Target: -1, Role: SupplierPrimary, DurationTicks: 2})})
		if err != nil {
			t.Fatal(err)
		}
		var found *ActiveDisruption
		for i := range step.State.Disruptions {
			if step.State.Disruptions[i].Origin == OriginInjected {
				found = &step.State.Disruptions[i]
			}
		}
		if found == nil || found.Role != SupplierPrimary || found.MagnitudeBps != 0 || found.Onset != 180 || found.End != 300 {
			t.Fatalf("injected disruption = %+v", found)
		}
		if !step.Window || len(step.WindowReasons) == 0 || step.WindowReasons[len(step.WindowReasons)-1] != WindowDisruptionOnset {
			t.Errorf("window = %v %v, want an onset window at tick 4", step.Window, step.WindowReasons)
		}
		if open, r := DecisionWindowOpen(sc, 4); open && len(r) > 0 && r[len(r)-1] == WindowDisruptionOnset {
			t.Error("script-only window must not know the injected onset")
		}
		if open, _ := DecisionWindowOpenInjected(sc, 5, []Tick{5}); !open {
			t.Error("injected onset at tick 5 opens no window")
		}
		end := step.State
		for end.Tick() < 6 {
			end, _ = mustStep(t, sc, end)
		}
		for _, d := range end.Disruptions {
			if d.Origin == OriginInjected {
				t.Errorf("injected disruption still active at tick 6: %+v", d)
			}
		}
	})

	t.Run("bounds and payload encoding", func(t *testing.T) {
		bad := []DisruptionInput{
			{Kind: DisruptionDemandSurge, Target: -1, DurationTicks: 0, MagnitudeBps: 12000},
			{Kind: DisruptionDemandSurge, Target: -1, DurationTicks: 121, MagnitudeBps: 12000},
			{Kind: DisruptionDemandSurge, Target: -1, DurationTicks: 1, MagnitudeBps: MaxSurgeBps + 1},
			{Kind: DisruptionDemandSurge, Target: 5, DurationTicks: 1, MagnitudeBps: 12000},
			{Kind: DisruptionSupplierOutage, Target: -1, Role: 2, DurationTicks: 1},
			{Kind: DisruptionWorkforceAbsence, Target: -1, DurationTicks: 1, MagnitudeBps: BpsScale + 1},
			{Kind: 9, Target: -1, DurationTicks: 1},
		}
		for _, d := range bad {
			if err := ValidateDisruptionInput(&sc, d, 3); !errors.Is(err, ErrBadDisruption) {
				t.Errorf("%+v accepted: %v", d, err)
			}
			if _, _, _, err := StepTick(sc, mustStepTo(t, sc, mustInit(t, sc, 1), 2), []Input{inj(1, 3, d)}); !errors.Is(err, ErrBadDisruption) {
				t.Errorf("engine accepted %+v: %v", d, err)
			}
		}
		if err := ValidateDisruptionInput(&sc, DisruptionInput{Kind: DisruptionDemandSurge, Target: -1, DurationTicks: 1, MagnitudeBps: 12000}, 0); !errors.Is(err, ErrBadDisruption) {
			t.Errorf("start tick 0 accepted: %v", err)
		}
		d := DisruptionInput{Kind: DisruptionSupplierOutage, Target: 0, Role: SupplierBackup, DurationTicks: 7, MagnitudeBps: 0}
		got, err := DecodeDisruptionInput(EncodeDisruptionInput(d))
		if err != nil || got != d {
			t.Errorf("round trip = %+v, %v", got, err)
		}
		if _, err := DecodeDisruptionInput([]byte{1, 2}); err == nil {
			t.Error("short payload accepted")
		}
	})
}

func mustStepTo(t *testing.T, sc Scenario, st State, tick Tick) State {
	t.Helper()
	for st.Tick() < tick {
		st, _ = mustStep(t, sc, st)
	}
	return st
}

func TestFlagshipRunsAllScriptedDisruptions(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	st := mustInit(t, *sc, 2026)
	seen := map[uint8]bool{}
	windowAtOnset := map[Tick]bool{}
	for int64(st.Tick()) < sc.HorizonTicks {
		step, err := Advance(*sc, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		st = step.State
		for _, d := range st.Disruptions {
			if d.End > st.Clock {
				seen[d.Kind] = true
			}
		}
		windowAtOnset[st.Tick()] = step.Window
	}
	for _, k := range []uint8{DisruptionDemandSurge, DisruptionWorkforceAbsence, DisruptionSupplierOutage} {
		if !seen[k] {
			t.Errorf("flagship never had disruption kind %d active", k)
		}
	}
	for _, d := range sc.DisruptionScript {
		if !windowAtOnset[Tick(d.OnsetTick)] {
			t.Errorf("no Decision_Window at the onset tick %d of %s", d.OnsetTick, d.Kind)
		}
	}
}
