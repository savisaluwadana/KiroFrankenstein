package sim

import (
	"fmt"
	"sort"
)

// SKUConstraint lists, for one SKU in one Tick, the open orders that were not
// shipped and why (Req 9.7). Order IDs are in fulfilment order, that is
// (PromisedBy, creation sequence).
type SKUConstraint struct {
	SKU                 int64
	StockBoundOrders    int64    // orders left open because on-hand stock was short
	StockBoundUnits     int64    // units of those orders
	StockBoundIDs       []uint64 // their order IDs
	CapacityBoundOrders int64    // orders left open because capacity ran out
	CapacityBoundUnits  int64
	CapacityBoundIDs    []uint64
}

// FulfilmentConstraintReport is the per-Tick record of unshipped open orders
// split into stock-bound and capacity-bound (Req 9.7). SKUs holds only SKUs
// with at least one unshipped order, ascending by SKU index. It is derived
// output and not part of State.
type FulfilmentConstraintReport struct {
	Tick          Tick
	Capacity      int64 // units of fulfilment capacity available this Tick
	CapacityUsed  int64 // units shipped this Tick
	SKUs          []SKUConstraint
	ShippedOrders int64 // orders shipped this Tick
}

// For returns the entry of sku, or a zero entry when sku has none.
func (r FulfilmentConstraintReport) For(sku int64) SKUConstraint {
	for _, c := range r.SKUs {
		if c.SKU == sku {
			return c
		}
	}
	return SKUConstraint{SKU: sku}
}

// regularOnShift returns the regular workers on shift at sim minute at after
// every active workforce_absence disruption has removed its share.
func regularOnShift(st *State, at SimMinute) int64 {
	onShift := st.Workforce.Regular
	for _, d := range st.Disruptions {
		if d.Kind != DisruptionWorkforceAbsence || d.Onset > at || at >= d.End {
			continue
		}
		onShift -= RoundHalfEven(onShift*int64(d.MagnitudeBps), BpsScale)
	}
	if onShift < 0 {
		onShift = 0
	}
	return onShift
}

// fulfilmentCapacity returns the units that can be shipped at sim minute at
// (Req 9.1, 9.2, 9.4): on-shift regular workers times units per worker hour,
// plus overtime units, plus temporary workers whose onboarding has finished
// (ReadyAt <= at) times units per worker hour. Active workforce_absence
// disruptions reduce the regular workers on shift by their magnitude.
func fulfilmentCapacity(sc *Scenario, st *State, at SimMinute) int64 {
	perWorker := sc.Workforce.UnitsPerWorkerHour
	onShift := regularOnShift(st, at)
	capacity := onShift * perWorker
	capacity += onShift * st.Workforce.OvertimeHours * perWorker
	for _, c := range st.Workforce.Temps {
		if c.ReadyAt <= at {
			capacity += c.Count * perWorker
		}
	}
	return capacity
}

// fulfil handles EvFulfilment at sim minute at. Open orders are taken in
// (PromisedBy, ID) order and each is shipped in full while both the remaining
// capacity and the SKU's on-hand stock allow it (Req 9.3). An order that
// cannot ship is stock-bound when the SKU's stock is short, otherwise
// capacity-bound; stock is tested first so a stock-out is never hidden behind
// exhausted capacity. A skipped order does not stop later, smaller or
// different-SKU orders from shipping.
func (e *stepper) fulfil(at SimMinute) error {
	open := e.st.Orders.Open
	order := make([]int, len(open))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		oa, ob := open[order[a]], open[order[b]]
		if oa.PromisedBy != ob.PromisedBy {
			return oa.PromisedBy < ob.PromisedBy
		}
		return oa.ID < ob.ID
	})

	capacity := fulfilmentCapacity(e.sc, &e.st, at)
	rep := FulfilmentConstraintReport{Tick: TickOf(at), Capacity: capacity}
	remaining := capacity
	shipped := make([]bool, len(open))
	bound := map[int64]*SKUConstraint{} // lookup only; output is sorted below

	for _, i := range order {
		o := open[i]
		if o.SKU < 0 || o.SKU >= int64(len(e.st.Inventory)) {
			return fmt.Errorf("sim: order %d references unknown SKU %d", o.ID, o.SKU)
		}
		inv := &e.st.Inventory[o.SKU]
		stockShort := inv.OnHand < o.Qty
		if stockShort || remaining < o.Qty {
			c := bound[o.SKU]
			if c == nil {
				c = &SKUConstraint{SKU: o.SKU}
				bound[o.SKU] = c
			}
			if stockShort {
				c.StockBoundOrders++
				c.StockBoundUnits += o.Qty
				c.StockBoundIDs = append(c.StockBoundIDs, o.ID)
			} else {
				c.CapacityBoundOrders++
				c.CapacityBoundUnits += o.Qty
				c.CapacityBoundIDs = append(c.CapacityBoundIDs, o.ID)
			}
			continue
		}
		if err := e.shipOrder(o, inv, at); err != nil {
			return err
		}
		remaining -= o.Qty
		shipped[i] = true
		rep.ShippedOrders++
	}

	if rep.ShippedOrders > 0 {
		kept := open[:0]
		for i, o := range open {
			if !shipped[i] {
				kept = append(kept, o)
			}
		}
		e.st.Orders.Open = kept
	}
	rep.CapacityUsed = capacity - remaining
	for sku := int64(0); sku < int64(len(e.st.Inventory)); sku++ {
		if c := bound[sku]; c != nil {
			rep.SKUs = append(rep.SKUs, *c)
		}
	}
	e.fcr = rep
	return nil
}

// shipOrder ships o in full from inv and posts one balanced Ledger_Entry that
// holds every line caused by the shipment (Req 10.3, 10.4, 10.5, 10.11, 10.12):
//
//	Dr Cash                 Cr Revenue            unit price * qty
//	Dr Cost_Of_Goods_Sold   Cr Inventory          moving-average cost
//	Dr Freight_Expense      Cr Cash               outbound freight * qty
//	Dr Service_Credit_Exp.  Cr Cash               service credit, when late
//
// Zero-amount lines are omitted. Revenue stays unchanged for a late order
// (Req 37.4).
func (e *stepper) shipOrder(o Order, inv *SKUInventory, at SimMinute) error {
	cogs, err := inv.ship(o.Qty)
	if err != nil {
		return fmt.Errorf("order %d: %w", o.ID, err)
	}
	revenue := o.UnitPrice * Cents(o.Qty)
	freight := e.sc.Costs.OutboundShipCentsPerUnit * Cents(o.Qty)
	late := at > o.PromisedBy
	var credit Cents
	if late {
		credit = e.sc.Costs.ServiceCreditPerLateOrderCents
	}

	var lines []LedgerLine
	add := func(acct AccountID, debit, credit Cents) {
		if debit != 0 || credit != 0 {
			lines = append(lines, LedgerLine{Account: acct, Debit: debit, Credit: credit})
		}
	}
	add(AccountCash, revenue, 0)
	add(AccountRevenue, 0, revenue)
	add(AccountCostOfGoodsSold, cogs, 0)
	add(AccountInventory, 0, cogs)
	add(AccountFreightExpense, freight, 0)
	add(AccountCash, 0, freight)
	add(AccountServiceCreditExpense, credit, 0)
	add(AccountCash, 0, credit)
	entry := LedgerEntry{Tick: TickOf(at), At: at, Cause: CauseShipment, CauseRef: int64(o.ID), Lines: lines}
	if err := e.st.Ledger.Post(entry); err != nil {
		return fmt.Errorf("order %d shipment posting: %w", o.ID, err)
	}

	e.st.Orders.Shipped++
	e.st.Orders.settle(o, at)
	e.st.Orders.UnitsShipped += o.Qty
	var lateBy int64
	if late {
		e.st.Orders.ShippedLate++
		lateBy = int64(at - o.PromisedBy)
	}
	e.journal = append(e.journal, Outcome{At: at, Kind: OutcomeShipment, SKU: o.SKU, Ref: int64(o.ID), Qty: o.Qty, Aux: lateBy})
	return nil
}
