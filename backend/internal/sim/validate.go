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
)

// Command bounds. A price must lie in [PriceMinBps, PriceMaxBps] of the SKU's
// reference price (basis points), and a purchase order quantity in
// [1, MaxOrderQuantity].
const (
	PriceMinBps      = 5000
	PriceMaxBps      = 20000
	MaxOrderQuantity = 1_000_000
)

// ValidationResult is the outcome for one proposed Command. Command is a copy
// of the input, never altered (Req 17.6). Code and Message are empty when OK.
type ValidationResult struct {
	Index   int
	Command Command
	OK      bool
	Code    RejectCode
	Message string
}

// PriceBounds returns the allowed unit price range of a SKU in cents.
func PriceBounds(ref Cents) (lo, hi Cents) {
	return Cents(MulDiv(int64(ref), PriceMinBps, BpsScale)), Cents(MulDiv(int64(ref), PriceMaxBps, BpsScale))
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
	for i, c := range cmds {
		code, msg, cost := validateOne(sc, obs, c, budgetRemaining, available)
		r := ValidationResult{Index: i, Command: c, OK: code == "", Code: code, Message: msg}
		if r.OK {
			budgetRemaining--
			available -= cost
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

// validateOne returns an empty code when c is acceptable, and then the cash it
// commits.
func validateOne(sc Scenario, obs Observation, c Command, budget, available int64) (RejectCode, string, int64) {
	if c.Kind >= numCommandKinds {
		return ReasonUnknownCommand, fmt.Sprintf("command kind %d is not in the command set", c.Kind), 0
	}
	switch c.Kind {
	case CmdSetPrice, CmdPlacePurchaseOrder:
	default:
		return ReasonNotImplemented, fmt.Sprintf("command %s is not implemented yet", c.Kind.Name()), 0
	}
	if budget <= 0 {
		return ReasonBudgetExhausted, "no command budget remains in this decision window", 0
	}
	sku, ok := obs.sku(c.SKU)
	if !ok {
		return ReasonUnknownSKU, fmt.Sprintf("sku %d does not exist", c.SKU), 0
	}
	if c.Kind == CmdSetPrice {
		lo, hi := PriceBounds(sku.ReferencePriceCents)
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
