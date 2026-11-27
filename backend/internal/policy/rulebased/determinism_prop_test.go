package rulebased_test

import (
	"context"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/rulebased"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// Feature: parallel-simulation-mvp, Property 23: Rule-based manager determinism
//
// **Validates: Requirements 19.2**
func TestPropRuleBasedDeterminism(t *testing.T) {
	nonEmpty := 0
	total := 0
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(48).Draw(t, "case")
		sc := c.Scenario
		tick := sim.Tick(rapid.Int64Range(1, sc.HorizonTicks).Draw(t, "tick"))
		run, err := simtest.RunToTick(sc, c.Seed, c.Inputs, tick)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		obs := sim.Observe(*sc, run.Final())

		// Observations with randomly varied cash and stock.
		varied := obs
		varied.Cash = sim.Cents(rapid.Int64Range(0, 50_000_000).Draw(t, "cash"))
		varied.SKUs = append([]sim.ObsSKU(nil), obs.SKUs...)
		for i := range varied.SKUs {
			varied.SKUs[i].OnHand = rapid.Int64Range(0, 2000).Draw(t, "onHand")
		}

		budget := int(sc.CommandBudget)
		other := *sc
		other.DisruptionScript = nil
		if len(sc.DisruptionScript) > 0 {
			other.DisruptionScript = sc.DisruptionScript[:len(sc.DisruptionScript)-1]
		} else {
			other.DisruptionScript = []sim.DisruptionSpec{{}}
		}

		for _, o := range []sim.Observation{obs, varied} {
			m := rulebased.New(*sc)
			first, err := m.Propose(context.Background(), o, budget)
			if err != nil {
				t.Fatalf("propose: %v", err)
			}
			again, _ := m.Propose(context.Background(), o, budget)
			fresh, _ := rulebased.New(*sc).Propose(context.Background(), o, budget)
			diff, _ := rulebased.New(other).Propose(context.Background(), o, budget)
			for name, p := range map[string]any{"repeat": again, "fresh": fresh, "other-script": diff} {
				if !reflect.DeepEqual(first, p) {
					t.Fatalf("%s proposal differs:\n%+v\n%+v", name, first, p)
				}
			}
			if len(first.Commands) > budget {
				t.Fatalf("%d commands exceed budget %d", len(first.Commands), budget)
			}
			for _, r := range sim.Validate(*sc, o, first.Commands, int64(budget)) {
				if !r.OK {
					t.Fatalf("command %+v rejected: %s %s", r.Command, r.Code, r.Message)
				}
			}
			total++
			if len(first.Commands) > 0 {
				nonEmpty++
			}
		}
	})
	if total > 0 && nonEmpty == 0 {
		t.Fatalf("vacuous: all %d proposals were empty", total)
	}
}
