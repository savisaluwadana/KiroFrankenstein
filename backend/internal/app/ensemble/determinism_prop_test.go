package ensemble_test

import (
	"bytes"
	"context"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

const (
	edScenarioPath = "../../../scenarios/black-friday.json"
	edDemoPath     = "../../../scenarios/demo-duel-human.json"
)

// edBytes runs the ensemble and returns the canonical JSON and CSV bytes.
func edBytes(t *rapid.T, cfg ensemble.Config) ([]byte, []byte) {
	res, err := ensemble.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rep := ensemble.BuildReport(res, cfg.Scenario.TieToleranceHundredths)
	j, err := rep.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	c, err := rep.CSV()
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	return j, c
}

// Feature: parallel-simulation-mvp, Property 29: Ensemble determinism under any execution order
// **Validates: Requirements 24.16**
func TestPropEnsembleDeterminism(t *testing.T) {
	base, hash, err := replayfile.LoadScenario(edScenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	demo, err := replayfile.LoadInputsAnySeed(edDemoPath, hash)
	if err != nil {
		t.Fatal(err)
	}

	rapid.Check(t, func(t *rapid.T) {
		horizon := rapid.Int64Range(24, 48).Draw(t, "horizon")
		sc, err := sim.ApplyOverridesAndValidate(base, map[string]int64{"horizonTicks": horizon})
		if err != nil {
			t.Fatalf("overrides: %v", err)
		}

		// 1..8 distinct seeds in a drawn order (duplicates excluded).
		seeds := rapid.SliceOfNDistinct(rapid.Uint64Range(1, 1000), 1, 8,
			func(v uint64) uint64 { return v }).Draw(t, "seeds")
		given := make([]sim.Seed, len(seeds))
		for i, s := range seeds {
			given[i] = sim.Seed(s)
		}

		// Plans cut to the shortened horizon; Inputs after it are not valid.
		fixedFile := demo
		fixedFile.Inputs = nil
		for _, in := range demo.Inputs {
			if int64(in.EffectiveTick) <= horizon {
				fixedFile.Inputs = append(fixedFile.Inputs, in)
			}
		}
		fixed := ensemble.Arm{Kind: ensemble.KindFixedReplay, Name: "demo", File: &fixedFile}
		rb := ensemble.Arm{Kind: ensemble.KindAdaptiveEvaluation, Name: "rulebased", StartTick: rapid.Int64Range(0, 20).Draw(t, "rbStart")}
		prefixFile := fixedFile
		prefixFile.Inputs = append([]inputsfile.Input(nil), fixedFile.Inputs...)
		ad := ensemble.Arm{Kind: ensemble.KindAdaptiveEvaluation, Name: "adaptive", File: &prefixFile, StartTick: rapid.Int64Range(0, 23).Draw(t, "adStart")}
		pool := []ensemble.Arm{fixed, rb, ad}
		armIdx := rapid.SliceOfNDistinct(rapid.IntRange(0, 2), 1, 2, func(v int) int { return v }).Draw(t, "arms")
		arms := make([]ensemble.Arm, len(armIdx))
		for i, k := range armIdx {
			arms[i] = pool[k]
		}

		mk := func(order []sim.Seed, conc int) ensemble.Config {
			return ensemble.Config{Scenario: sc, ScenarioHash: hash, Seeds: order, Arms: arms, Concurrency: conc}
		}
		refJSON, refCSV := edBytes(t, mk(given, 1))

		conc := rapid.IntRange(1, 8).Draw(t, "concurrency")
		shuffled := rapid.Permutation(given).Draw(t, "shuffled")
		for _, c := range []struct {
			name  string
			order []sim.Seed
			conc  int
		}{
			{"given order, drawn concurrency", given, conc},
			{"shuffled order, drawn concurrency", shuffled, conc},
			{"shuffled order, repeated run", shuffled, conc},
		} {
			j, cs := edBytes(t, mk(c.order, c.conc))
			if !bytes.Equal(j, refJSON) {
				t.Fatalf("%s: JSON differs", c.name)
			}
			if !bytes.Equal(cs, refCSV) {
				t.Fatalf("%s: CSV differs", c.name)
			}
		}
	})
}
