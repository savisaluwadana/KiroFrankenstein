package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// Compile-time check: ComputeScore takes exactly a Scenario and the branch's
// own Metrics and has no opponent parameter.
var _ func(sim.Scenario, sim.Metrics) sim.Score = sim.ComputeScore

func siMetrics() *rapid.Generator[sim.Metrics] {
	return rapid.Custom(func(t *rapid.T) sim.Metrics {
		return sim.Metrics{
			Tick:         sim.Tick(rapid.Int64Range(0, 200).Draw(t, "tick")),
			NetProfit:    sim.Cents(rapid.Int64Range(-100_000_000, 100_000_000).Draw(t, "profit")),
			FillRate:     sim.Bps(rapid.Int64Range(0, 10000).Draw(t, "fill")),
			ServiceLevel: sim.Bps(rapid.Int64Range(0, 10000).Draw(t, "service")),
		}
	})
}

// siDuelScore scores branch A in a duel against an opponent with metrics opp,
// also exercising Compare with the opponent's score.
func siDuelScore(sc sim.Scenario, a, opp sim.Metrics) sim.Score {
	sa := sim.ComputeScore(sc, a)
	sb := sim.ComputeScore(sc, opp)
	_ = sim.Compare(sa, sb, 10)
	return sa
}

// siFinalMetrics runs the branch to the horizon and returns its final Metrics.
func siFinalMetrics(t *rapid.T, sc *sim.Scenario, seed sim.Seed, in []sim.Input) sim.Metrics {
	run, err := simtest.RunToHorizon(sc, seed, in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return sim.ComputeMetrics(*sc, run.Final())
}

// Feature: parallel-simulation-mvp, Property 35: Composite_Score competitor independence
//
// **Validates: Requirements 21.12**
func TestPropScoreCompetitorIndependence(t *testing.T) {
	t.Run("random metrics", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			sc := simtest.Scenarios(simtest.MinHorizon).Draw(t, "scenario")
			a := siMetrics().Draw(t, "a")
			b1 := siMetrics().Draw(t, "b1")
			b2 := siMetrics().Draw(t, "b2")

			s1 := siDuelScore(*sc, a, b1)
			s2 := siDuelScore(*sc, a, b2)
			alone := sim.ComputeScore(*sc, a)
			if s1 != s2 || s1 != alone {
				t.Fatalf("A's score depends on opponent: %+v %+v %+v", s1, s2, alone)
			}
		})
	})

	t.Run("engine branches", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			sc := simtest.Scenarios(32).Draw(t, "scenario")
			seed := simtest.Seeds().Draw(t, "seed")
			inA, err := simtest.BuildInputLog(sc, seed, simtest.GenProposals(sc).Draw(t, "propsA"))
			if err != nil {
				t.Fatalf("log A: %v", err)
			}
			inB1, err := simtest.BuildInputLog(sc, seed, simtest.GenProposals(sc).Draw(t, "propsB1"))
			if err != nil {
				t.Fatalf("log B1: %v", err)
			}
			inB2, err := simtest.BuildInputLog(sc, seed, simtest.GenProposals(sc).Draw(t, "propsB2"))
			if err != nil {
				t.Fatalf("log B2: %v", err)
			}

			mA1 := siFinalMetrics(t, sc, seed, inA)
			mB1 := siFinalMetrics(t, sc, seed, inB1)
			s1 := siDuelScore(*sc, mA1, mB1)

			mA2 := siFinalMetrics(t, sc, seed, inA)
			mB2 := siFinalMetrics(t, sc, seed, inB2)
			s2 := siDuelScore(*sc, mA2, mB2)

			if s1 != s2 {
				t.Fatalf("A's score differs between duels with identical A inputs: %+v vs %+v", s1, s2)
			}
		})
	})
}
