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

// Expedite errors.
var (
	ErrAlreadyExpedited = errors.New("sim: purchase order is already expedited")
	ErrSupplierDown     = errors.New("sim: supplier is in an outage")
)

// ExpeditePurchaseOrder moves the due time of open purchase order id earlier by
// the Scenario's expedite reduction (Ticks converted to minutes, never earlier
// than at), marks it expedited, posts one balanced entry (Dr Freight_Expense,
// Cr Cash) for the expedite fee and schedules a receipt at the new due time;
// the receipt queued for the old due time is then stale and ignored (Req 8.4,
// 10.7). An order can be expedited once. While its supplier is in an outage the
// order cannot be expedited. On error st and q are unchanged.
func ExpeditePurchaseOrder(sc *Scenario, st *State, q *EventQueue, id uint64, at SimMinute) error {
	idx := -1
	for i, p := range st.PurchaseOs {
		if p.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: %d", ErrUnknownPO, id)
	}
	po := &st.PurchaseOs[idx]
	if po.Expedited != 0 {
		return fmt.Errorf("%w: %d", ErrAlreadyExpedited, id)
	}
	if _, down := outageEnd(st.Disruptions, po.SKU, po.Supplier, at); down {
		return fmt.Errorf("%w: purchase order %d", ErrSupplierDown, id)
	}
	if fee := sc.Costs.ExpediteFeeCents; fee > 0 {
		entry := LedgerEntry{
			Tick: TickOf(at), At: at, Cause: CauseFreight, CauseRef: int64(id),
			Lines: []LedgerLine{
				{Account: AccountFreightExpense, Debit: fee},
				{Account: AccountCash, Credit: fee},
			},
		}
		if err := st.Ledger.Post(entry); err != nil {
			return fmt.Errorf("purchase order %d expedite fee posting: %w", id, err)
		}
	}
	due := po.DueAt - SimMinute(sc.Costs.ExpediteReductionTicks*MinutesPerTick)
	if due < at {
		due = at
	}
	po.DueAt = due
	po.Expedited = 1
	q.Push(Event{
		At:       due,
		Priority: PriorityPurchaseReceipt,
		Kind:     EvPurchaseReceipt,
		Payload:  EventPayload{Ref: int64(po.ID), SKU: po.SKU, Qty: po.Qty},
	})
	return nil
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
		if ev.Payload.Ref >= 0 && uint64(ev.Payload.Ref) < e.st.POSeq {
			// Stale receipt of an order already received at an earlier,
			// expedited due time.
			return nil
		}
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
