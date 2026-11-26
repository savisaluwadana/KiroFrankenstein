package simtest

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func TestLoadFlagship(t *testing.T) {
	sc, err := LoadFlagship()
	if err != nil {
		t.Fatal(err)
	}
	if sc.HorizonTicks != 120 {
		t.Fatalf("horizon = %d, want 120", sc.HorizonTicks)
	}
	sc.HorizonTicks = 24
	if again := MustFlagship(); again.HorizonTicks != 120 {
		t.Fatal("LoadFlagship returned shared state")
	}
}

func TestRunToHorizonShort(t *testing.T) {
	sc, err := WithHorizon(MustFlagship(), MinHorizon)
	if err != nil {
		t.Fatal(err)
	}
	run, err := RunToHorizon(sc, 2026, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Hashes) != MinHorizon+1 || len(run.States) != MinHorizon+1 {
		t.Fatalf("got %d hashes, want %d", len(run.Hashes), MinHorizon+1)
	}
	again, _ := RunToHorizon(sc, 2026, nil)
	if run.Hashes[MinHorizon] != again.Hashes[MinHorizon] {
		t.Fatal("run not deterministic")
	}
}

// TestPropGeneratedInputLogsValid checks that every generated Input log is
// within budget, sits at Decision_Window Ticks, and replays without error with
// every command accepted by the validator at its window.
func TestPropGeneratedInputLogsValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := Cases(ShortHorizon).Draw(t, "case")
		sc := c.Scenario
		perTick := map[sim.Tick]int64{}
		var last uint64
		for _, in := range c.Inputs {
			if in.Seq <= last {
				t.Fatalf("seq not increasing: %d after %d", in.Seq, last)
			}
			last = in.Seq
			win := in.EffectiveTick - 1
			if open, _ := sim.DecisionWindowOpen(*sc, win); !open {
				t.Fatalf("input at tick %d, not a decision window", win)
			}
			perTick[win]++
			if perTick[win] > sc.CommandBudget {
				t.Fatalf("budget exceeded at window %d", win)
			}
		}
		run, err := RunToHorizon(sc, c.Seed, c.Inputs)
		if err != nil {
			t.Fatal(err)
		}
		// Re-validate each window's inputs against the replayed Observation.
		byWin := map[sim.Tick][]sim.Command{}
		for _, in := range c.Inputs {
			byWin[in.EffectiveTick-1] = append(byWin[in.EffectiveTick-1], in.Command)
		}
		for win, cmds := range byWin {
			obs := sim.Observe(*sc, run.States[win])
			for _, r := range sim.Validate(*sc, obs, cmds, sc.CommandBudget) {
				if !r.OK {
					t.Fatalf("window %d command %d rejected: %s %s", win, r.Index, r.Code, r.Message)
				}
			}
		}
	})
}
