package sim_test

import (
	"bytes"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// Feature: parallel-simulation-mvp, Property 38: Snapshot completeness and resume equivalence
//
// Engine-level (pure) property: it needs only sim.Encode/Decode and the engine,
// so it lives in package sim_test rather than internal/app. A snapshot is the
// canonical encoding of the State at a random Tick N, taken when the
// uninterrupted run reaches N. Decoding it and applying the same Input suffix
// (all six Command kinds and injected Disruptions) must reproduce, at every
// later Tick, the hash, the canonical encoding, EventSeq, OrderSeq, POSeq, the
// purchase-order identifiers and every random stream state of the
// uninterrupted run. States already returned by Advance must never change
// afterwards (a cached snapshot is only trustworthy if Advance does not modify
// its input).
//
// **Validates: Requirements 14.6, 28.6**

// resumeCoverage counts how often generated suffixes exercised each feature
// after the snapshot Tick, so the property cannot pass vacuously.
type resumeCoverage struct {
	poIDsAfter, po, expedite, hireTemp, overtime, marketing, setPrice, disruption int
}

// tickRecord is what the uninterrupted run looked like at one Tick, captured
// at the time the Tick was computed.
type tickRecord struct {
	enc                       []byte
	hash                      [32]byte
	eventSeq, orderSeq, poSeq uint64
	poIDs                     []uint64
	streams                   []sim.NamedStream
}

func recordTick(st sim.State) tickRecord {
	r := tickRecord{
		enc: sim.Encode(st), hash: sim.Hash(st),
		eventSeq: st.EventSeq, orderSeq: st.OrderSeq, poSeq: st.POSeq,
		streams: append([]sim.NamedStream(nil), st.Streams...),
	}
	for _, po := range st.PurchaseOs {
		r.poIDs = append(r.poIDs, po.ID)
	}
	return r
}

func genResumeCommands(t *rapid.T, sc *sim.Scenario, obs sim.Observation) []sim.Command {
	n := rapid.IntRange(0, int(sc.CommandBudget)+2).Draw(t, "ncmds")
	nSKU := int64(len(sc.SKUs))
	var cmds []sim.Command
	for i := 0; i < n; i++ {
		sku := rapid.Int64Range(0, nSKU-1).Draw(t, "sku")
		switch rapid.IntRange(0, 5).Draw(t, "kind") {
		case 0:
			lo, hi := sim.PriceBounds(*sc, sc.SKUs[sku].ReferencePriceCents)
			cmds = append(cmds, sim.Command{Kind: sim.CmdSetPrice, SKU: sku,
				AmountCents: sim.Cents(rapid.Int64Range(int64(lo), int64(hi)).Draw(t, "price"))})
		case 1, 2: // weighted: purchase orders drive the PO identifier counter
			cmds = append(cmds, sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: sku,
				Role: uint8(rapid.IntRange(0, 1).Draw(t, "role")),
				Qty:  rapid.Int64Range(1, 400).Draw(t, "qty")})
		case 3:
			if len(obs.PurchaseOrders) > 0 {
				po := obs.PurchaseOrders[rapid.IntRange(0, len(obs.PurchaseOrders)-1).Draw(t, "po")]
				cmds = append(cmds, sim.Command{Kind: sim.CmdExpeditePurchaseOrder, PORef: po.ID})
			}
		case 4:
			if rapid.Bool().Draw(t, "temp") {
				cmds = append(cmds, sim.Command{Kind: sim.CmdHireTempWorkers,
					Qty: rapid.Int64Range(1, max(1, sc.Workforce.MaxTempWorkers)).Draw(t, "temps")})
			} else {
				cmds = append(cmds, sim.Command{Kind: sim.CmdScheduleOvertime,
					Qty: rapid.Int64Range(0, max(0, sc.Workforce.MaxOvertimePerWorker)).Draw(t, "ot")})
			}
		case 5:
			cmds = append(cmds, sim.Command{Kind: sim.CmdSetMarketingSpend, SKU: sku,
				AmountCents: sim.Cents(rapid.Int64Range(0, max(0, int64(sc.Demand.MaxMarketingSpendCentsPerHour))).Draw(t, "mkt"))})
		}
	}
	return cmds
}

func genResumeDisruption(t *rapid.T, sc *sim.Scenario) sim.DisruptionInput {
	dur := rapid.Int64Range(1, min(12, sc.HorizonTicks)).Draw(t, "dur")
	target := rapid.Int64Range(-1, int64(len(sc.SKUs))-1)
	switch rapid.IntRange(0, 2).Draw(t, "dkind") {
	case 0:
		return sim.DisruptionInput{Kind: sim.DisruptionDemandSurge, Target: target.Draw(t, "dtarget"),
			DurationTicks: dur, MagnitudeBps: sim.Bps(rapid.Int64Range(10000, 40000).Draw(t, "surge"))}
	case 1:
		return sim.DisruptionInput{Kind: sim.DisruptionSupplierOutage, Target: target.Draw(t, "dtarget"),
			Role: uint8(rapid.IntRange(0, 1).Draw(t, "drole")), DurationTicks: dur}
	}
	return sim.DisruptionInput{Kind: sim.DisruptionWorkforceAbsence, Target: -1, DurationTicks: dur,
		MagnitudeBps: sim.Bps(rapid.Int64Range(0, 6000).Draw(t, "absent"))}
}

func (c *resumeCoverage) count(step []sim.Input) {
	for _, in := range step {
		if in.Kind == sim.InputDisruption {
			c.disruption++
			continue
		}
		switch in.Command.Kind {
		case sim.CmdPlacePurchaseOrder:
			c.po++
		case sim.CmdExpeditePurchaseOrder:
			c.expedite++
		case sim.CmdHireTempWorkers:
			c.hireTemp++
		case sim.CmdScheduleOvertime:
			c.overtime++
		case sim.CmdSetMarketingSpend:
			c.marketing++
		case sim.CmdSetPrice:
			c.setPrice++
		}
	}
}

func TestPropSnapshotResume(t *testing.T) {
	var cov resumeCoverage
	rapid.Check(t, func(t *rapid.T) {
		sc := simtest.Scenarios(simtest.ShortHorizon).Draw(t, "scenario")
		seed := simtest.Seeds().Draw(t, "seed")
		snapAt := sim.Tick(rapid.Int64Range(1, sc.HorizonTicks-1).Draw(t, "snapshotTick"))

		// Uninterrupted run with a validator-accepted Input log generated online.
		st, err := sim.Init(*sc, seed)
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		states := []sim.State{st}
		records := []tickRecord{recordTick(st)}
		byTick := map[sim.Tick][]sim.Input{}
		var seq uint64
		for int64(st.Tick()) < sc.HorizonTicks {
			tick := st.Tick()
			var step []sim.Input
			if open, _ := sim.DecisionWindowOpenState(*sc, st); open {
				obs := sim.Observe(*sc, st)
				for _, r := range sim.Validate(*sc, obs, genResumeCommands(t, sc, obs), sc.CommandBudget) {
					if r.OK {
						seq++
						step = append(step, sim.Input{EffectiveTick: tick + 1, Seq: seq, Kind: sim.InputCommand, Command: r.Command})
					}
				}
			}
			if rapid.IntRange(0, 9).Draw(t, "inject") == 0 {
				seq++
				step = append(step, sim.Input{EffectiveTick: tick + 1, Seq: seq, Kind: sim.InputDisruption,
					Disruption: genResumeDisruption(t, sc)})
			}
			adv, err := sim.Advance(*sc, st, step)
			if err != nil {
				t.Fatalf("advance to tick %d: %v", tick+1, err)
			}
			if tick >= snapAt {
				byTick[tick+1] = step
				cov.count(step)
			}
			st = adv.State
			states = append(states, st)
			records = append(records, recordTick(st))
		}

		// A State returned by Advance must not be modified by later Advance calls.
		for i := range states {
			if !bytes.Equal(sim.Encode(states[i]), records[i].enc) {
				t.Fatalf("state of tick %d changed after later ticks were computed (Advance modified its input); differing fields: %v",
					i, stateFieldDiff(states[i], mustDecode(t, records[i].enc)))
			}
		}

		// Resume from the snapshot taken at snapAt.
		res := mustDecode(t, records[snapAt].enc)
		if sim.Hash(res) != records[snapAt].hash {
			t.Fatalf("decoded snapshot hash differs at tick %d", snapAt)
		}
		for res.Tick() < sim.Tick(sc.HorizonTicks) {
			next := res.Tick() + 1
			adv, err := sim.Advance(*sc, res, byTick[next])
			if err != nil {
				t.Fatalf("resumed advance to tick %d: %v", next, err)
			}
			res = adv.State
			got, want := recordTick(res), records[next]
			if got.hash != want.hash || !bytes.Equal(got.enc, want.enc) {
				t.Fatalf("tick %d hash differs after resuming from %d; differing fields: %v",
					next, snapAt, stateFieldDiff(res, mustDecode(t, want.enc)))
			}
			if got.eventSeq != want.eventSeq || got.orderSeq != want.orderSeq || got.poSeq != want.poSeq {
				t.Fatalf("tick %d counters differ: event %d/%d order %d/%d po %d/%d", next,
					got.eventSeq, want.eventSeq, got.orderSeq, want.orderSeq, got.poSeq, want.poSeq)
			}
			if !reflect.DeepEqual(got.poIDs, want.poIDs) {
				t.Fatalf("tick %d purchase-order ids differ: %v vs %v", next, got.poIDs, want.poIDs)
			}
			if !reflect.DeepEqual(got.streams, want.streams) {
				t.Fatalf("tick %d random stream states differ: %v vs %v", next, got.streams, want.streams)
			}
		}
		if records[snapAt].poSeq < records[len(records)-1].poSeq {
			cov.poIDsAfter++
		}
	})
	if cov.poIDsAfter == 0 || cov.po == 0 || cov.expedite == 0 || cov.hireTemp == 0 ||
		cov.overtime == 0 || cov.marketing == 0 || cov.setPrice == 0 || cov.disruption == 0 {
		t.Fatalf("vacuous property, post-snapshot coverage: %+v", cov)
	}
}

func mustDecode(t *rapid.T, b []byte) sim.State {
	s, err := sim.Decode(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return s
}

// stateFieldDiff names the top-level State fields that differ, for triage.
func stateFieldDiff(got, want sim.State) []string {
	var out []string
	gv, wv := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := 0; i < gv.NumField(); i++ {
		if !reflect.DeepEqual(gv.Field(i).Interface(), wv.Field(i).Interface()) {
			out = append(out, gv.Type().Field(i).Name)
		}
	}
	return out
}
