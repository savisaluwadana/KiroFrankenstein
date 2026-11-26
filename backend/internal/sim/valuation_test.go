package sim

import (
	"errors"
	"testing"
)

func TestValuationShipCost(t *testing.T) {
	tests := []struct {
		name    string
		v       Cents
		u, q    int64
		want    Cents
		wantErr bool
	}{
		{"q<u exact", 1000, 10, 3, 300, false},
		{"q<u rounds down (0.5 to even 0)", 5, 10, 1, 0, false},     // 0.5 -> 0
		{"q<u rounds up (1.5 to even 2)", 15, 10, 1, 2, false},      // 1.5 -> 2
		{"q<u rounds down (2.5 to even 2)", 25, 10, 1, 2, false},    // 2.5 -> 2
		{"q<u above half rounds up", 1001, 3, 1, 334, false},        // 333.67
		{"q=u takes full remaining value", 1001, 3, 3, 1001, false}, // no residual
		{"q=u single unit", 7, 1, 1, 7, false},
		{"q>u rejected", 100, 2, 3, 0, true},
		{"q=0 rejected", 100, 2, 0, 0, true},
	}
	for _, tc := range tests {
		got, err := ShipCost(tc.v, tc.u, tc.q)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s: ShipCost(%d,%d,%d) = %d, %v; want %d, err=%v", tc.name, tc.v, tc.u, tc.q, got, err, tc.want, tc.wantErr)
		}
	}
	if _, err := ShipCost(100, 2, 3); !errors.Is(err, ErrShipExceedsOnHand) {
		t.Errorf("err = %v", err)
	}
}

func TestValuationShipUpdatesInventoryAndLeavesNoResidual(t *testing.T) {
	k := SKUInventory{OnHand: 3, Value: 1001, OpeningUnits: 3}
	var total Cents
	for i := 0; i < 3; i++ {
		c, err := k.ship(1)
		if err != nil {
			t.Fatal(err)
		}
		total += c
	}
	// 1001*1/3 = 334 (333.67), then 667*1/2 = 334 (333.5 -> even 334), then the last takes 333.
	if total != 1001 || k.Value != 0 || k.OnHand != 0 || k.Shipped != 3 {
		t.Errorf("total=%d inv=%+v", total, k)
	}
}
