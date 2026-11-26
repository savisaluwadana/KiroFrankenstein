package sim_test

import (
	"os"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Early benchmarks (Req 29.1, 29.9). Every determinism, invariant and
// validation check the engine always performs stays enabled; nothing is
// disabled for speed.

func loadFlagship(tb testing.TB) *sim.Scenario {
	tb.Helper()
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		tb.Fatal(err)
	}
	sc, err := sim.ParseScenario(data)
	if err != nil {
		tb.Fatal(err)
	}
	if err := sim.ValidateScenario(sc); err != nil {
		tb.Fatal(err)
	}
	return sc
}

// replayFlagship runs one full replay from Tick 0 to the horizon with no
// Commands and returns the final State and the number of outcome journal
// entries produced.
func replayFlagship(tb testing.TB, sc *sim.Scenario, seed sim.Seed) (sim.State, int) {
	tb.Helper()
	st, err := sim.Init(*sc, seed)
	if err != nil {
		tb.Fatal(err)
	}
	outcomes := 0
	for int64(st.Tick()) < sc.HorizonTicks {
		step, err := sim.Advance(*sc, st, nil)
		if err != nil {
			tb.Fatal(err)
		}
		outcomes += len(step.Outcomes)
		st = step.State
	}
	return st, outcomes
}

// TestFlagshipReplayCounts logs the per-run counts quoted in docs/benchmarks.md.
func TestFlagshipReplayCounts(t *testing.T) {
	sc := loadFlagship(t)
	st, outcomes := replayFlagship(t, sc, 2026)
	t.Logf("seed=2026 ticks=%d outcomes=%d ledger_entries=%d event_seq=%d hash=%x",
		int64(st.Tick()), outcomes, len(st.Ledger.Entries), st.EventSeq, sim.Hash(st))
}

// BenchmarkFlagshipReplay is one full 120-Tick flagship replay, Seed 2026.
func BenchmarkFlagshipReplay(b *testing.B) {
	sc := loadFlagship(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st, _ := replayFlagship(b, sc, 2026)
		_ = sim.Hash(st)
	}
}

// BenchmarkHundredReplays is 100 sequential flagship replays: 2 runs for each
// of Seeds 1 to 50 (early proxy for the 50-Seed Paired_Experiment, Req 29.9).
func BenchmarkHundredReplays(b *testing.B) {
	sc := loadFlagship(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for seed := sim.Seed(1); seed <= 50; seed++ {
			for run := 0; run < 2; run++ {
				st, _ := replayFlagship(b, sc, seed)
				_ = sim.Hash(st)
			}
		}
	}
}
