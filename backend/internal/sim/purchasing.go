package sim

import (
	"errors"
	"fmt"
	"math"
)

// Supplier roles stored in PurchaseOrder.Supplier.
const (
	SupplierPrimary uint8 = 0
	SupplierBackup  uint8 = 1
)

// Purchasing errors.
var (
	ErrUnknownSupplier = errors.New("sim: no supplier for sku and role")
	ErrBadPOQuantity   = errors.New("sim: purchase order quantity must be positive")
	ErrUnknownSKU      = errors.New("sim: unknown sku index")
	ErrUnknownPO       = errors.New("sim: receipt for unknown purchase order")
)

// supplierRoleName returns the scenario role string for a role code.
func supplierRoleName(role uint8) (string, bool) {
	switch role {
	case SupplierPrimary:
		return "primary", true
	case SupplierBackup:
		return "backup", true
	}
	return "", false
}

// LookupSupplier returns the scenario supplier spec for a SKU index and role.
func LookupSupplier(sc *Scenario, sku int64, role uint8) (SupplierSpec, error) {
	name, ok := supplierRoleName(role)
	if !ok || sku < 0 || sku >= int64(len(sc.SKUs)) {
		return SupplierSpec{}, fmt.Errorf("%w: sku %d role %d", ErrUnknownSupplier, sku, role)
	}
	id := sc.SKUs[sku].ID
	for _, s := range sc.Suppliers {
		if s.SKUID == id && s.Role == name {
			return s, nil
		}
	}
	return SupplierSpec{}, fmt.Errorf("%w: sku %q role %q", ErrUnknownSupplier, id, name)
}

// PlacePurchaseOrder opens a purchase order for qty units of sku from the
// supplier role at sim minute at (the effective placement time). The ID comes
// from st.POSeq, the cost is the Supplier_Unit_Cost at placement, the due time
// is at plus the supplier lead time (Ticks converted to minutes), and an
// EvPurchaseReceipt event is pushed on q for the due time (Req 8.1, 8.5).
// No ledger entry is posted: payment happens on receipt (Req 10.6, 37.6). On
// error st and q are unchanged.
func PlacePurchaseOrder(sc *Scenario, st *State, q *EventQueue, sku int64, role uint8, qty int64, at SimMinute) (PurchaseOrder, error) {
	if qty <= 0 {
		return PurchaseOrder{}, ErrBadPOQuantity
	}
	if sku < 0 || sku >= int64(len(st.Inventory)) {
		return PurchaseOrder{}, fmt.Errorf("%w: %d", ErrUnknownSKU, sku)
	}
	sup, err := LookupSupplier(sc, sku, role)
	if err != nil {
		return PurchaseOrder{}, err
	}
	if sup.UnitCostCents > 0 && qty > math.MaxInt64/int64(sup.UnitCostCents) {
		return PurchaseOrder{}, ErrAmountOverflow
	}
	start := at
	if end, down := outageEnd(st.Disruptions, sku, role, at); down {
		start = end // placed during a supplier_outage: due = outage end + lead time (Req 8.3)
	}
	po := PurchaseOrder{
		ID:       st.POSeq,
		SKU:      sku,
		Supplier: role,
		Qty:      qty,
		UnitCost: sup.UnitCostCents,
		PlacedAt: at,
		DueAt:    start + SimMinute(sup.LeadTimeTicks*MinutesPerTick),
	}
	st.POSeq++
	st.PurchaseOs = append(st.PurchaseOs, po)
	q.Push(Event{
		At:       po.DueAt,
		Priority: PriorityPurchaseReceipt,
		Kind:     EvPurchaseReceipt,
		Payload:  EventPayload{Ref: int64(po.ID), SKU: sku, Qty: qty},
	})
	return po, nil
}

// receive handles EvPurchaseReceipt for the purchase order in ev.Payload.Ref:
// on-hand and Received rise by the quantity, Inventory_Value by Supplier_Unit_Cost
// times quantity, one balanced entry (Dr Inventory, Cr Cash) is posted, and the
// order leaves State.PurchaseOs (Req 8.2, 10.6, 37.6). Written-off units are
// never touched (Req 8.8).
func (e *stepper) receive(ev Event) error {
	idx := -1
	for i, p := range e.st.PurchaseOs {
		if int64(p.ID) == ev.Payload.Ref {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: %d", ErrUnknownPO, ev.Payload.Ref)
	}
	po := e.st.PurchaseOs[idx]
	if po.DueAt != ev.At {
		// Stale event: the order was moved (supplier_outage delay) and a
		// receipt for its current DueAt is queued separately.
		return nil
	}
	if po.SKU < 0 || po.SKU >= int64(len(e.st.Inventory)) {
		return fmt.Errorf("sim: purchase order %d references unknown SKU %d", po.ID, po.SKU)
	}
	if po.UnitCost > 0 && po.Qty > math.MaxInt64/int64(po.UnitCost) {
		return ErrAmountOverflow
	}
	cost := po.UnitCost * Cents(po.Qty)
	if cost > 0 {
		entry := LedgerEntry{
			Tick: TickOf(ev.At), At: ev.At, Cause: CauseReceipt, CauseRef: int64(po.ID),
			Lines: []LedgerLine{
				{Account: AccountInventory, Debit: cost},
				{Account: AccountCash, Credit: cost},
			},
		}
		if err := e.st.Ledger.Post(entry); err != nil {
			return fmt.Errorf("purchase order %d receipt posting: %w", po.ID, err)
		}
	}
	inv := &e.st.Inventory[po.SKU]
	inv.OnHand += po.Qty
	inv.Received += po.Qty
	inv.Value += cost
	e.st.PurchaseOs = append(e.st.PurchaseOs[:idx], e.st.PurchaseOs[idx+1:]...)
	return nil
}
