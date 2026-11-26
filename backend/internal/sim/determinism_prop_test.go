package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// detCopyScenario returns an independent copy of sc made by printing it to the
// canonical document form, parsing it back and validating it.
func detCopyScenario(t *rapid.T, sc *sim.Scenario) *sim.Scenario {
	parsed, err := sim.ParseScenario(sim.PrintScenario(sc))
	if err != nil {
		t.Fatalf("re-parse scenario: %v", err)
	}
	cp, err := sim.ApplyOverridesAndValidate(parsed, nil)
	if err != nil {
		t.Fatalf("re-validate scenario: %v", err)
	}
	return cp
}

// detReencodeInputs returns a copy of in with every Command encoded and decoded.
func detReencodeInputs(t *rapid.T, in []sim.Input) []sim.Input {
	out := make([]sim.Input, len(in))
	for i, x := range in {
		if x.Kind == sim.InputCommand {
			c, err := sim.DecodeCommand(sim.EncodeCommand(x.Command))
			if err != nil {
				t.Fatalf("decode command: %v", err)
			}
			x.Command = c
		}
		out[i] = x
	}
	return out
}

// Feature: parallel-simulation-mvp, Property 5: Determinism at every tick
//
// **Validates: Requirements 6.3**
func TestPropDeterminism(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(48).Draw(t, "case")

		a, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
		if err != nil {
			t.Fatalf("run A: %v", err)
		}
		b, err := simtest.RunToHorizon(detCopyScenario(t, c.Scenario), c.Seed, detReencodeInputs(t, c.Inputs))
		if err != nil {
			t.Fatalf("run B: %v", err)
		}

		if len(a.Hashes) != len(b.Hashes) || int64(len(a.Hashes)) != c.Scenario.HorizonTicks+1 {
			t.Fatalf("hash counts differ: %d vs %d (horizon %d)", len(a.Hashes), len(b.Hashes), c.Scenario.HorizonTicks)
		}
		for i := range a.Hashes {
			if a.Hashes[i] != b.Hashes[i] {
				t.Fatalf("state hash differs at tick %d", i)
			}
		}
	})
}
