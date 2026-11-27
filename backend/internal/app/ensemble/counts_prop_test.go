package ensemble_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// wcResult builds a synthetic two-Arm Result with the given composite scores.
func wcResult(a, b []int64) ensemble.Result {
	res := ensemble.Result{
		ScenarioHash: "wc", EngineVersion: 1, HorizonTicks: 120,
		Arms: []ensemble.Arm{
			{Kind: ensemble.KindAdaptiveEvaluation, Name: "a"},
			{Kind: ensemble.KindAdaptiveEvaluation, Name: "b"},
		},
	}
	for i := range a {
		seed := sim.Seed(100 + i)
		res.Seeds = append(res.Seeds, seed)
		res.Runs = append(res.Runs,
			ensemble.RunResult{Seed: seed, ArmIndex: 0, Arm: "a", Score: sim.Score{Composite: a[i]}},
			ensemble.RunResult{Seed: seed, ArmIndex: 1, Arm: "b", Score: sim.Score{Composite: b[i]}},
		)
	}
	return res
}

// wcCounts recomputes (firstWins, secondWins, ties) independently.
func wcCounts(a, b []int64, tol int64) (int, int, int) {
	var fw, sw, ties int
	for i := range a {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		switch {
		case d <= tol:
			ties++
		case a[i] > b[i]:
			fw++
		default:
			sw++
		}
	}
	return fw, sw, ties
}

// Feature: parallel-simulation-mvp, Property 31: Win/tie count conservation
// **Validates: Requirements 25.11**
func TestPropWinTieConservation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 50).Draw(t, "seedCount")
		tol := rapid.Int64Range(0, 1000).Draw(t, "tolerance")
		// A narrow band around the tolerance makes exact-tolerance and tie cases frequent.
		span := rapid.SampledFrom([]int64{20, 1000, 2500, 10000}).Draw(t, "span")
		score := rapid.Int64Range(0, span)
		a := make([]int64, n)
		b := make([]int64, n)
		for i := 0; i < n; i++ {
			a[i] = score.Draw(t, "a")
			if rapid.Bool().Draw(t, "exactEdge") {
				off := tol
				if rapid.Bool().Draw(t, "negEdge") {
					off = -tol
				}
				b[i] = a[i] + off
				if b[i] < 0 || b[i] > 10000 {
					b[i] = a[i]
				}
			} else {
				b[i] = score.Draw(t, "b")
			}
		}

		rep := ensemble.BuildReport(wcResult(a, b), tol)
		if rep.Paired == nil {
			t.Fatal("paired section missing")
		}
		p := rep.Paired
		if p.FirstArmWins+p.SecondArmWins+p.Ties != rep.SeedCount || rep.SeedCount != n {
			t.Fatalf("conservation: %d+%d+%d != %d", p.FirstArmWins, p.SecondArmWins, p.Ties, n)
		}
		fw, sw, ties := wcCounts(a, b, tol)
		if p.FirstArmWins != fw || p.SecondArmWins != sw || p.Ties != ties {
			t.Fatalf("counts %d/%d/%d, want %d/%d/%d", p.FirstArmWins, p.SecondArmWins, p.Ties, fw, sw, ties)
		}
		if len(p.Differences) != n {
			t.Fatalf("differences len %d, want %d", len(p.Differences), n)
		}
		for i, d := range p.Differences {
			if d.Difference != b[i]-a[i] || d.FirstScore != a[i] || d.SecondScore != b[i] {
				t.Fatalf("diff row %d = %+v, want %d", i, d, b[i]-a[i])
			}
		}

		sw2 := ensemble.BuildReport(wcResult(b, a), tol).Paired
		if sw2.FirstArmWins != p.SecondArmWins || sw2.SecondArmWins != p.FirstArmWins || sw2.Ties != p.Ties {
			t.Fatalf("swap: %d/%d/%d vs %d/%d/%d", sw2.FirstArmWins, sw2.SecondArmWins, sw2.Ties,
				p.FirstArmWins, p.SecondArmWins, p.Ties)
		}
	})
}
