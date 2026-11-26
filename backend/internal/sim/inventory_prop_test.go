package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// icViolation returns a description of the first inventory-conservation
// violation in inv, or "" when every SKU is consistent.
func icViolation(inv []sim.SKUInventory) string {
	for _, s := range inv {
		if s.OpeningUnits+s.Received-s.Shipped-s.WrittenOff != s.OnHand {
			return "OpeningUnits+Received-Shipped-WrittenOff != OnHand"
		}
		if s.OnHand < 0 {
			return "OnHand < 0"
		}
		if s.WrittenOff != 0 {
			return "WrittenOff != 0"
		}
	}
	return ""
}

// Feature: parallel-simulation-mvp, Property 14: Inventory conservation and non-negative stock
// **Validates: Requirements 8.6, 8.7**
func TestPropInventoryConservation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(simtest.MinHorizon).Draw(t, "case")
		run, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		for i, st := range run.States {
			if msg := icViolation(st.Inventory); msg != "" {
				t.Fatalf("tick %d: %s: %+v", i, msg, st.Inventory)
			}
		}
	})
}
