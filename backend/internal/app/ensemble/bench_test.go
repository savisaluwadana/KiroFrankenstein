package ensemble

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strconv"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
)

// BenchmarkPairedEnsemble50 measures the 50-Seed Paired_Experiment of the
// flagship (Req 29.2): the fixed demo plan versus the rule-based adaptive
// arm, Seeds 1..50, 100 full runs, with every validation, invariant check and
// state hash enabled. "default" uses the documented default concurrency
// (Concurrency 0 = GOMAXPROCS); "concurrency-1" is the sequential figure.
// Method and results: docs/benchmarks.md.
func BenchmarkPairedEnsemble50(b *testing.B) {
	sc, h, err := replayfile.LoadScenario(scenarioPath)
	if err != nil {
		b.Fatal(err)
	}
	f, err := replayfile.LoadInputsAnySeed(demoHumanPath, h)
	if err != nil {
		b.Fatal(err)
	}
	sd, err := SeedRange(1, 50)
	if err != nil {
		b.Fatal(err)
	}
	cases := []struct {
		name string
		conc int
	}{
		{"default-gomaxprocs" + strconv.Itoa(runtime.GOMAXPROCS(0)), 0},
		{"concurrency-1", 1},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cfg := Config{
				Scenario: sc, ScenarioHash: h, Seeds: sd, Concurrency: c.conc,
				Arms: []Arm{
					{Kind: KindFixedReplay, Name: "fixed:scenarios/demo-duel-human.json", File: &f},
					{Kind: KindAdaptiveEvaluation, Name: "rulebased"},
				},
			}
			var first string
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				res, err := Run(context.Background(), cfg)
				if err != nil {
					b.Fatal(err)
				}
				if i == 0 && first == "" {
					b.StopTimer() // one-off result check, not measured
					js, err := BuildReport(res, sc.TieToleranceHundredths).JSON()
					if err != nil {
						b.Fatal(err)
					}
					if len(res.Runs) != 100 || len(js) == 0 {
						b.Fatalf("runs=%d reportBytes=%d", len(res.Runs), len(js))
					}
					sum := sha256.Sum256(js)
					first = hex.EncodeToString(sum[:])
					b.Logf("report sha256=%s bytes=%d runs=%d", first, len(js), len(res.Runs))
					b.StartTimer()
				}
			}
		})
	}
}
