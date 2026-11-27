package sim

import (
	"fmt"
	"math"
)

// RejectCode is a machine-readable rejection reason (Req 17.3).
type RejectCode string

// Rejection reason codes.
const (
	ReasonUnknownCommand      RejectCode = "unknown_command"
	ReasonNotImplemented      RejectCode = "not_implemented"
	ReasonBudgetExhausted     RejectCode = "budget_exhausted"
	ReasonUnknownSKU          RejectCode = "unknown_sku"
	ReasonPriceOutOfBounds    RejectCode = "price_out_of_bounds"
	ReasonInvalidQuantity     RejectCode = "invalid_quantity"
	ReasonUnknownSupplier     RejectCode = "unknown_supplier"
	ReasonSupplierUnavailable RejectCode = "supplier_unavailable"
	ReasonInsufficientCash    RejectCode = "insufficient_cash"
	ReasonUnknownPO           RejectCode = "unknown_purchase_order"
	ReasonAlreadyExpedited    RejectCode = "already_expedited"

	ReasonOvertimeExceedsLimit  RejectCode = "overtime_exceeds_limit"
	ReasonTooManyTempWorkers    RejectCode = "too_many_temp_workers"
	ReasonMarketingExceedsLimit RejectCode = "marketing_exceeds_limit"
)

// MaxOrderQuantity bounds a purchase order quantity to [1, MaxOrderQuantity].
// Price bounds are Scenario assumptions (pricing.minPriceBps, pricing.maxPriceBps).
const MaxOrderQuantity = 1_000_000

// ValidationResult is the outcome for one proposed Command. Command is a copy
// of the input, never altered (Req 17.6). Code and Message are empty when OK.
type ValidationResult struct {
	Index   int
	Command Command
	OK      bool
	Code    RejectCode
	Message string
}

// PriceBounds returns the allowed unit price range in cents of a SKU with the
// given reference price, from the Scenario's pricing assumptions.
func PriceBounds(sc Scenario, ref Cents) (lo, hi Cents) {
	return Cents(MulDiv(int64(ref), int64(sc.Pricing.MinPriceBps), BpsScale)), Cents(MulDiv(int64(ref), int64(sc.Pricing.MaxPriceBps), BpsScale))
}

// Validate checks cmds one at a time in order against obs, the Command_Budget
// still available in this Decision_Window (budgetRemaining) and the Cash
// committed by the Commands already accepted (Req 17.2, 17.7). It is a pure
// function: identical arguments give identical results, and no Command is
// modified. A rejected Command consumes no budget and commits no cash.
//
// Available cash for a purchase order is the Observation's Cash, minus the
// cost of every open purchase order in the Observation (paid on receipt),
// minus the cost of purchase orders accepted earlier in cmds. A supplier under
// an active supplier_outage is unavailable.
//
// Check order per Command: kind in the Command_Set, kind implemented, budget,
// then parameters.
func Validate(sc Scenario, obs Observation, cmds []Command, budgetRemaining int64) []ValidationResult {
	available := int64(obs.Cash)
	for _, po := range obs.PurchaseOrders {
		available -= int64(po.UnitCost) * po.Qty
	}
	results := make([]ValidationResult, 0, len(cmds))
	expedited := map[uint64]bool{} // lookup only, never iterated
	var pendingTemps int64
	for i, c := range cmds {
		code, msg, cost := validateOne(sc, obs, c, budgetRemaining, available, expedited, pendingTemps)
		r := ValidationResult{Index: i, Command: c, OK: code == "", Code: code, Message: msg}
		if r.OK {
			budgetRemaining--
			available -= cost
			if c.Kind == CmdHireTempWorkers {
				pendingTemps += c.Qty
			}
			if c.Kind == CmdExpeditePurchaseOrder {
				expedited[c.PORef] = true
			}
		}
		results = append(results, r)
	}
	return results
}

func (o Observation) sku(idx int64) (ObsSKU, bool) {
	for _, s := range o.SKUs {
		if s.SKU == idx {
			return s, true
		}
	}
	return ObsSKU{}, false
}

// validateExpedite checks an expedite_purchase_order Command: the order must be
// open in the Observation and not yet expedited (by the Observation or an
// earlier Command of this batch), its supplier must not be in an outage, and
// the expedite fee must be affordable. It returns the fee as the committed cash.
func validateExpedite(sc Scenario, obs Observation, c Command, available int64, expedited map[uint64]bool) (RejectCode, string, int64) {
	var po *ObsPurchaseOrder
	for i := range obs.PurchaseOrders {
		if obs.PurchaseOrders[i].ID == c.PORef {
			po = &obs.PurchaseOrders[i]
			break
		}
	}
	if po == nil {
		return ReasonUnknownPO, fmt.Sprintf("purchase order %d is not open", c.PORef), 0
	}
	if po.Expedited || expedited[c.PORef] {
		return ReasonAlreadyExpedited, fmt.Sprintf("purchase order %d is already expedited", c.PORef), 0
	}
	if sku, ok := obs.sku(po.SKU); ok {
		for _, s := range sku.Suppliers {
			if s.Role == po.Supplier && !s.Available {
				return ReasonSupplierUnavailable, fmt.Sprintf("supplier role %d for %s is in an outage until tick %d", s.Role, sku.ID, s.AvailableFromTick), 0
			}
		}
	}
	fee := int64(sc.Costs.ExpediteFeeCents)
	if fee > available {
		return ReasonInsufficientCash, fmt.Sprintf("expedite fee is %d cents but %d cents are available", fee, available), 0
	}
	return "", "", fee
}

// validateOne returns an empty code when c is acceptable, and then the cash it
// commits.
func validateOne(sc Scenario, obs Observation, c Command, budget, available int64, expedited map[uint64]bool, pendingTemps int64) (RejectCode, string, int64) {
	if c.Kind >= numCommandKinds {
		return ReasonUnknownCommand, fmt.Sprintf("command kind %d is not in the command set", c.Kind), 0
	}
	switch c.Kind {
	case CmdSetPrice, CmdPlacePurchaseOrder, CmdExpeditePurchaseOrder, CmdScheduleOvertime, CmdHireTempWorkers, CmdSetMarketingSpend:
	default:
		return ReasonNotImplemented, fmt.Sprintf("command %s is not implemented yet", c.Kind.Name()), 0
	}
	if budget <= 0 {
		return ReasonBudgetExhausted, "no command budget remains in this decision window", 0
	}
	switch c.Kind {
	case CmdExpeditePurchaseOrder:
		return validateExpedite(sc, obs, c, available, expedited)
	case CmdScheduleOvertime:
		return validateOvertime(sc, obs, c, available)
	case CmdHireTempWorkers:
		return validateHireTemps(sc, obs, c, available, pendingTemps)
	case CmdSetMarketingSpend:
		return validateMarketing(sc, obs, c, available)
	}
	sku, ok := obs.sku(c.SKU)
	if !ok {
		return ReasonUnknownSKU, fmt.Sprintf("sku %d does not exist", c.SKU), 0
	}
	if c.Kind == CmdSetPrice {
		lo, hi := PriceBounds(sc, sku.ReferencePriceCents)
		if c.AmountCents < lo || c.AmountCents > hi {
			return ReasonPriceOutOfBounds, fmt.Sprintf("price %d cents is outside %d..%d cents for %s", c.AmountCents, lo, hi, sku.ID), 0
		}
		return "", "", 0
	}

	if c.Qty < 1 || c.Qty > MaxOrderQuantity {
		return ReasonInvalidQuantity, fmt.Sprintf("quantity %d is outside 1..%d", c.Qty, MaxOrderQuantity), 0
	}
	var sup *ObsSupplier
	for i := range sku.Suppliers {
		if sku.Suppliers[i].Role == c.Role {
			sup = &sku.Suppliers[i]
		}
	}
	if sup == nil {
		return ReasonUnknownSupplier, fmt.Sprintf("sku %s has no supplier with role %d", sku.ID, c.Role), 0
	}
	if !sup.Available {
		return ReasonSupplierUnavailable, fmt.Sprintf("supplier role %d for %s is in an outage until tick %d", c.Role, sku.ID, sup.AvailableFromTick), 0
	}
	if sup.UnitCostCents > 0 && c.Qty > math.MaxInt64/int64(sup.UnitCostCents) {
		return ReasonInvalidQuantity, "order cost overflows", 0
	}
	cost := c.Qty * int64(sup.UnitCostCents)
	if cost > available {
		return ReasonInsufficientCash, fmt.Sprintf("order costs %d cents but %d cents are available", cost, available), 0
	}
	return "", "", cost
}
