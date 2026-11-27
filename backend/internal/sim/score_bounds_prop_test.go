package sim_test

import (
	"math"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

func checkScoreBounds(t *rapid.T, sc sim.Scenario, m sim.Metrics) {
	s := sim.ComputeScore(sc, m)
	for name, v := range map[string]int64{
		"financial": int64(s.Financial), "fulfilment": int64(s.FulfilmentRate),
		"onTime": int64(s.OnTime), "composite": s.Composite,
	} {
		if v < 0 || v > sim.BpsScale {
			t.Fatalf("%s = %d out of [0,%d] (metrics %+v)", name, v, sim.BpsScale, m)
		}
	}
}

// Feature: parallel-simulation-mvp, Property 34: Composite_Score bounds
//
// **Validates: Requirements 21.11**
func TestPropScoreBounds(t *testing.T) {
	t.Run("random metrics", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			const lim = math.MaxInt64 / 2
			floor := rapid.Int64Range(-lim, lim-1).Draw(t, "floor")
			target := rapid.Int64Range(floor+1, lim).Draw(t, "target")
			wf := rapid.IntRange(0, 10000).Draw(t, "wf")
			wl := rapid.IntRange(0, 10000-wf).Draw(t, "wl")
			sc := sim.Scenario{
				ProfitFloorCents:  sim.Cents(floor),
				ProfitTargetCents: sim.Cents(target),
				ScoreWeights: sim.ScoreWeights{
					FinancialWeightBps:  sim.Bps(wf),
					FulfilmentWeightBps: sim.Bps(wl),
					OnTimeWeightBps:     sim.Bps(10000 - wf - wl),
				},
			}
			profit := rapid.OneOf(
				rapid.Int64Range(-lim, lim),
				rapid.Int64Range(floor-5, floor+5),
				rapid.Int64Range(target-5, target+5),
				rapid.SampledFrom([]int64{math.MinInt64 + 1, math.MaxInt64, 0, floor, target}),
			).Draw(t, "profit")
			m := sim.Metrics{
				NetProfit:    sim.Cents(profit),
				FillRate:     sim.Bps(rapid.Int64Range(0, 10000).Draw(t, "fill")),
				ServiceLevel: sim.Bps(rapid.Int64Range(0, 10000).Draw(t, "service")),
			}
			checkScoreBounds(t, sc, m)
		})
	})

	t.Run("real runs every tick", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			c := simtest.Cases(48).Draw(t, "case")
			run, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			for _, st := range run.States {
				checkScoreBounds(t, *c.Scenario, sim.ComputeMetrics(*c.Scenario, st))
			}
		})
	})
}
