package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// vcViolation returns a description of the first valuation violation in st,
// or "" when inventory valuation is consistent.
func vcViolation(st sim.State) string {
	var sum sim.Cents
	for _, k := range st.Inventory {
		if k.Value < 0 {
			return "negative Value"
		}
		if k.OnHand == 0 && k.Value != 0 {
			return "residual value on a SKU with zero units"
		}
		sum += k.Value
	}
	if bal := st.Ledger.Balance(sim.AccountInventory); sum != bal {
		return "sum of SKU values != Inventory ledger balance"
	}
	return ""
}

// Feature: parallel-simulation-mvp, Property 19: Valuation consistency and no residual value
// **Validates: Requirements 37.10, 37.11**
func TestPropValuation(t *testing.T) {
	t.Run("generated runs", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			c := simtest.Cases(simtest.MinHorizon).Draw(t, "case")
			run, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			for i, st := range run.States {
				if msg := vcViolation(st); msg != "" {
					t.Fatalf("tick %d: %s: %+v", i, msg, st.Inventory)
				}
			}
		})
	})

	t.Run("ShipCost value level", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			v := sim.Cents(rapid.Int64Range(0, 1_000_000_000_000).Draw(t, "v"))
			u := rapid.Int64Range(1, 1_000_000).Draw(t, "u")
			q := rapid.Int64Range(1, u).Draw(t, "q")
			cost, err := sim.ShipCost(v, u, q)
			if err != nil {
				t.Fatalf("ShipCost(%d,%d,%d): %v", v, u, q, err)
			}
			if cost < 0 || cost > v {
				t.Fatalf("ShipCost(%d,%d,%d)=%d outside [0,v]", v, u, q, cost)
			}
			if full, _ := sim.ShipCost(v, u, u); full != v {
				t.Fatalf("ShipCost(%d,%d,%d)=%d, want exactly v", v, u, u, full)
			}
		})
	})

	t.Run("ShipCost partitions remove exactly v", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			v := sim.Cents(rapid.Int64Range(0, 1_000_000_000_000).Draw(t, "v"))
			u := rapid.Int64Range(1, 1_000_000).Draw(t, "u")
			remV, remU := v, u
			var removed sim.Cents
			for remU > 0 {
				q := rapid.Int64Range(1, remU).Draw(t, "part")
				cost, err := sim.ShipCost(remV, remU, q)
				if err != nil {
					t.Fatalf("ShipCost(%d,%d,%d): %v", remV, remU, q, err)
				}
				if cost < 0 || cost > remV {
					t.Fatalf("cost %d outside [0,%d]", cost, remV)
				}
				removed += cost
				remV -= cost
				remU -= q
			}
			if removed != v || remV != 0 {
				t.Fatalf("removed %d of %d (residual %d)", removed, v, remV)
			}
		})
	})
}
