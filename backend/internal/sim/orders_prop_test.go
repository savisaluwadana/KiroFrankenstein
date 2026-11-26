package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// ocViolation returns a description of the first order-conservation violation
// in b, or "" when the book is consistent.
func ocViolation(b sim.OrderBook) string {
	if got := int64(len(b.Open)) + b.Shipped + b.Cancelled; b.Created != got {
		return "Created != len(Open)+Shipped+Cancelled"
	}
	var openUnits int64
	for _, o := range b.Open {
		openUnits += o.Qty
	}
	if b.UnitsCreated != openUnits+b.UnitsShipped+b.UnitsCancelled {
		return "UnitsCreated != openUnits+UnitsShipped+UnitsCancelled"
	}
	if int64(len(b.Settled)) != b.Shipped+b.Cancelled {
		return "len(Settled) != Shipped+Cancelled"
	}
	return ""
}

// Feature: parallel-simulation-mvp, Property 13: Order conservation
// **Validates: Requirements 7.6**
func TestPropOrderConservation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(simtest.MinHorizon).Draw(t, "case")
		run, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		for i, st := range run.States {
			if msg := ocViolation(st.Orders); msg != "" {
				t.Fatalf("tick %d: %s: %+v", i, msg, st.Orders)
			}
		}
	})
}
