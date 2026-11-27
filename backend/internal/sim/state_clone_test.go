package sim

import (
	"bytes"
	"testing"
)

// TestStateCloneIsolation advances a state many ticks and checks that every
// earlier returned State still encodes byte-identically, so no later step
// writes through memory shared with a previous State (INV-2, INV-4).
func TestStateCloneIsolation(t *testing.T) {
	sc := *validScenario()
	st := mustInit(t, sc, 85741351723057822)

	var states []State
	var encoded [][]byte
	for i := 0; i < 400 && int64(st.Tick()) < sc.HorizonTicks; i++ {
		st, _ = mustStep(t, sc, st)
		states = append(states, st)
		encoded = append(encoded, Encode(st))
	}
	if len(states) < 2 {
		t.Fatalf("advanced only %d ticks", len(states))
	}
	for i, s := range states {
		if !bytes.Equal(Encode(s), encoded[i]) {
			t.Fatalf("state after tick %d changed after later ticks were computed", s.Tick())
		}
	}
}

// TestStateCloneIsolationSettledOutOfOrder forces the in-place insertion that
// OrderBook.settle performs when a lower order ID settles after a higher one
// (a late cancellation after a later shipment) on a clone whose Settled slice
// has spare capacity, and checks the source State is untouched.
func TestStateCloneIsolationSettledOutOfOrder(t *testing.T) {
	var src State
	src.Orders.Settled = make([]SettledOrder, 0, 8)
	src.Orders.Open = make([]Order, 0, 8)
	src.Orders.settle(Order{ID: 5, Qty: 1}, 10)
	src.Orders.settle(Order{ID: 7, Qty: 1}, 20)
	before := Encode(src)

	c := src.clone()
	c.Orders.settle(Order{ID: 6, Qty: 1}, NotShipped)
	c.Orders.settle(Order{ID: 1, Qty: 1}, NotShipped)

	if !bytes.Equal(Encode(src), before) {
		t.Fatal("settling on a clone mutated the source State")
	}
	if len(c.Orders.Settled) != 4 || c.Orders.Settled[0].ID != 1 || c.Orders.Settled[2].ID != 6 {
		t.Fatalf("clone settled list wrong: %+v", c.Orders.Settled)
	}
}
