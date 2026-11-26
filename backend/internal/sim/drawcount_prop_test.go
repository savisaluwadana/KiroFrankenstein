package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// dcTrace is the exogenous trace of a run: after each Tick, the state of the
// demand, supplier and workforce streams and the per-SKU potential arrivals.
type dcTrace struct {
	streams  [][3]sim.StreamState
	arrivals [][]int64
}

func dcStream(st sim.State, name sim.StreamName) sim.StreamState {
	for _, ns := range st.Streams {
		if ns.Name == name {
			return ns.State
		}
	}
	return 0
}

// dcRun steps the engine to the horizon applying inputs, recording the trace.
func dcRun(t *rapid.T, sc *sim.Scenario, seed sim.Seed, inputs []sim.Input) dcTrace {
	st, err := sim.Init(*sc, seed)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	byTick := map[sim.Tick][]sim.Input{}
	for _, in := range inputs {
		byTick[in.EffectiveTick] = append(byTick[in.EffectiveTick], in)
	}
	var tr dcTrace
	for int64(st.Tick()) < sc.HorizonTicks {
		next, outs, _, err := sim.StepTick(*sc, st, byTick[st.Tick()+1])
		if err != nil {
			t.Fatalf("step to tick %d: %v", st.Tick()+1, err)
		}
		st = next
		tr.streams = append(tr.streams, [3]sim.StreamState{
			dcStream(st, sim.StreamDemand),
			dcStream(st, sim.StreamSupplier),
			dcStream(st, sim.StreamWorkforce),
		})
		var arr []int64
		for _, o := range outs {
			if o.Kind == sim.OutcomeDemand {
				arr = append(arr, o.Qty)
			}
		}
		tr.arrivals = append(tr.arrivals, arr)
	}
	return tr
}

// Feature: parallel-simulation-mvp, Property 12: Demand draw count independent of commands
// **Validates: Requirements 7.2**
func TestPropDemandDrawCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(36).Draw(t, "case")
		props := simtest.GenProposals(c.Scenario).Draw(t, "otherProposals")
		other, err := simtest.BuildInputLog(c.Scenario, c.Seed, props)
		if err != nil {
			t.Fatalf("build other log: %v", err)
		}

		base := dcRun(t, c.Scenario, c.Seed, nil)
		for name, log := range map[string][]sim.Input{"generated": c.Inputs, "other": other} {
			got := dcRun(t, c.Scenario, c.Seed, log)
			for i := range base.streams {
				if got.streams[i] != base.streams[i] {
					t.Fatalf("%s log: stream states differ after tick %d: %v vs %v", name, i+1, got.streams[i], base.streams[i])
				}
				if len(got.arrivals[i]) != len(base.arrivals[i]) {
					t.Fatalf("%s log: arrival count differs at tick %d", name, i+1)
				}
				for k := range base.arrivals[i] {
					if got.arrivals[i][k] != base.arrivals[i][k] {
						t.Fatalf("%s log: potential arrivals differ at tick %d sku %d: %d vs %d", name, i+1, k, got.arrivals[i][k], base.arrivals[i][k])
					}
				}
			}
		}
	})
}
