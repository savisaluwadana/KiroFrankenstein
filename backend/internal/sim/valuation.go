package sim

import "errors"

// ErrShipExceedsOnHand is returned when a shipment asks for more units than
// are on hand (INV-8: on hand never goes negative).
var ErrShipExceedsOnHand = errors.New("sim: shipment exceeds on-hand units")

// ShipCost returns the Moving_Average_Cost of shipping q units from a SKU that
// holds u units with Inventory_Value v (Req 37.5 to 37.9):
//
//   - q == u: the full remaining value v, so no residual value is left;
//   - q < u: v*q/u rounded half to even through the shared helper.
//
// q must be positive and at most u, and v must not be negative.
func ShipCost(v Cents, u, q int64) (Cents, error) {
	if q <= 0 || q > u || v < 0 {
		return 0, ErrShipExceedsOnHand
	}
	if q == u {
		return v, nil
	}
	return Cents(MulDiv(int64(v), q, u)), nil
}

// ship removes q units from the SKU's stock, returning the cost of goods sold.
// It updates OnHand, Value and the cumulative Shipped counter together so
// INV-8 and INV-19 hold after every shipment.
func (k *SKUInventory) ship(q int64) (Cents, error) {
	cogs, err := ShipCost(k.Value, k.OnHand, q)
	if err != nil {
		return 0, err
	}
	k.Value -= cogs
	k.OnHand -= q
	k.Shipped += q
	return cogs, nil
}
