package sim

// createOrders appends n single-unit orders for sku at unit price price,
// created at sim minute at, each promised by at + promiseTicks*60 (Req 7.4).
// Order IDs are the per-branch creation sequence, so they are increasing and
// Orders.Open stays sorted by ID.
func (e *stepper) createOrders(sku, n int64, price Cents, at SimMinute) {
	promised := at + SimMinute(e.sc.SKUs[sku].PromiseTicks*MinutesPerTick)
	for i := int64(0); i < n; i++ {
		e.st.Orders.Open = append(e.st.Orders.Open, Order{
			ID:         e.st.OrderSeq,
			SKU:        sku,
			Qty:        1,
			UnitPrice:  price,
			CreatedAt:  at,
			PromisedBy: promised,
		})
		e.st.OrderSeq++
	}
	e.st.Orders.Created += n
	e.st.Orders.UnitsCreated += n
}

// settle records a shipped (shippedAt >= 0) or cancelled (NotShipped) order in
// Orders.Settled, keeping it sorted by order ID.
func (b *OrderBook) settle(o Order, shippedAt SimMinute) {
	rec := SettledOrder{ID: o.ID, Qty: o.Qty, PromisedBy: o.PromisedBy, ShippedAt: shippedAt}
	i := len(b.Settled)
	for i > 0 && b.Settled[i-1].ID > rec.ID {
		i--
	}
	b.Settled = append(b.Settled, SettledOrder{})
	copy(b.Settled[i+1:], b.Settled[i:])
	b.Settled[i] = rec
}

// cancelAgedOrders handles EvOrderCancel: every open order whose age at sim
// minute at has reached the cancellation threshold is cancelled (Req 7.5).
// Revenue and payment are recognised at shipment, so a cancellation records
// an outcome-journal entry and no Ledger_Entry (Req 37.3).
func (e *stepper) cancelAgedOrders(at SimMinute) {
	limit := SimMinute(e.sc.Demand.CancelThresholdTicks * MinutesPerTick)
	kept := e.st.Orders.Open[:0]
	for _, o := range e.st.Orders.Open {
		age := at - o.CreatedAt
		if age < limit {
			kept = append(kept, o)
			continue
		}
		e.st.Orders.Cancelled++
		e.st.Orders.settle(o, NotShipped)
		e.st.Orders.UnitsCancelled += o.Qty
		e.journal = append(e.journal, Outcome{At: at, Kind: OutcomeOrderCancelled, SKU: o.SKU, Ref: int64(o.ID), Qty: o.Qty, Aux: int64(age)})
	}
	e.st.Orders.Open = kept
}
