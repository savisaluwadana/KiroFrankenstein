package ensemble

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// The committed golden 20-seed Ensemble_Report of the flagship (Req 25.14):
// the demo human plan as the fixed arm versus the rule-based adaptive arm,
// Seeds 1..20. It changes only with an engine version bump or a deliberate
// scenario change recorded in docs/calibration.md. Regenerate with
// UPDATE_GOLDEN=1 go test ./internal/app/ensemble -run TestGoldenEnsembleReport
const (
	goldenJSONPath = "../../../scenarios/golden/ensemble-flagship-20.json"
	goldenCSVPath  = "../../../scenarios/golden/ensemble-flagship-20.csv"
)

func TestGoldenEnsembleReport(t *testing.T) {
	sc, h, f := load(t)
	res, err := Run(context.Background(), Config{
		Scenario: sc, ScenarioHash: h, Seeds: seeds(t, 20), Concurrency: 4,
		Arms: []Arm{
			// Names match the CLI invocation run from backend/ (see ci.yml).
			{Kind: KindFixedReplay, Name: "fixed:scenarios/demo-duel-human.json", File: &f},
			{Kind: KindAdaptiveEvaluation, Name: "rulebased"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rep := BuildReport(res, sc.TieToleranceHundredths)
	js, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	cs, err := rep.CSV()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		for p, b := range map[string][]byte{goldenJSONPath: js, goldenCSVPath: cs} {
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	for p, got := range map[string][]byte{goldenJSONPath: js, goldenCSVPath: cs} {
		want, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s differs from the regenerated report (engine or scenario changed? record it in docs/calibration.md, then UPDATE_GOLDEN=1)", p)
		}
	}
}
