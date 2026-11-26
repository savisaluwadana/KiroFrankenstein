package sim

import (
	"errors"
	"fmt"
	"sort"
)

// Disruption origins stored in ActiveDisruption.Origin.
const (
	OriginScript   uint8 = 0
	OriginInjected uint8 = 1
)

// Disruption kind names as written in a Disruption_Script.
const (
	kindNameDemandSurge      = "demand_surge"
	kindNameSupplierOutage   = "supplier_outage"
	kindNameWorkforceAbsence = "workforce_absence"
)

// ErrBadDisruption is returned when a Disruption_Script entry cannot be run.
var ErrBadDisruption = errors.New("sim: invalid disruption")

// resolveScriptDisruption converts Disruption_Script entry idx into the
// ActiveDisruption it produces. Onset is the start of the onset Tick and End
// the start of the Tick after the last affected Tick, so the disruption is in
// effect for sim minutes [Onset, End). For supplier_outage, MagnitudeBps
// carries the targeted supplier role code (SupplierPrimary or SupplierBackup)
// and Target is -1 (every SKU of that role). For demand_surge, Target is the
// SKU index or -1 when the entry has no target.
func resolveScriptDisruption(sc *Scenario, idx int) (ActiveDisruption, error) {
	spec := sc.DisruptionScript[idx]
	bad := func(format string, a ...any) (ActiveDisruption, error) {
		return ActiveDisruption{}, fmt.Errorf("%w: disruptionScript[%d]: %s", ErrBadDisruption, idx, fmt.Sprintf(format, a...))
	}
	if spec.OnsetTick < 1 || spec.DurationTicks < 1 {
		return bad("onsetTick and durationTicks must be at least 1")
	}
	d := ActiveDisruption{
		Origin:   OriginScript,
		InputSeq: uint64(idx),
		Onset:    Tick(spec.OnsetTick - 1).Boundary(),
		End:      Tick(spec.OnsetTick - 1 + spec.DurationTicks).Boundary(),
		Target:   -1,
	}
	switch spec.Kind {
	case kindNameDemandSurge:
		d.Kind = DisruptionDemandSurge
		if spec.MagnitudeBps == nil || *spec.MagnitudeBps <= 0 {
			return bad("demand_surge needs a positive magnitudeBps")
		}
		d.MagnitudeBps = *spec.MagnitudeBps
		if spec.Target != nil {
			found := false
			for i, k := range sc.SKUs {
				if k.ID == *spec.Target {
					d.Target, found = int64(i), true
					break
				}
			}
			if !found {
				return bad("unknown sku target %q", *spec.Target)
			}
		}
	case kindNameSupplierOutage:
		d.Kind = DisruptionSupplierOutage
		if spec.Target == nil {
			return bad("supplier_outage needs a target supplier role")
		}
		switch *spec.Target {
		case "primary":
			d.MagnitudeBps = Bps(SupplierPrimary)
		case "backup":
			d.MagnitudeBps = Bps(SupplierBackup)
		default:
			return bad("unknown supplier role %q", *spec.Target)
		}
	case kindNameWorkforceAbsence:
		d.Kind = DisruptionWorkforceAbsence
		if spec.MagnitudeBps == nil || *spec.MagnitudeBps < 0 || *spec.MagnitudeBps > BpsScale {
			return bad("workforce_absence needs magnitudeBps in 0..10000")
		}
		d.MagnitudeBps = *spec.MagnitudeBps
	default:
		return bad("unknown kind %q", spec.Kind)
	}
	return d, nil
}

// scheduleDisruption pushes the expiry and activation events of d. Events at
// one sim minute run expiries (PriorityDisruptionEnd) before activations
// (PriorityDisruptionStart), and within a priority in insertion order, so
// scripted entries (scheduled at Init in script order) precede injected ones
// (scheduled later, in input order) (Req 5.8). The payload carries the origin
// and input sequence so the handler can find the disruption again.
func scheduleDisruption(q *EventQueue, d ActiveDisruption) {
	payload := EventPayload{Ref: int64(d.InputSeq), Aux: int64(d.Origin)}
	q.Push(Event{At: d.Onset, Priority: PriorityDisruptionStart, Kind: EvDisruptionOnset, Payload: payload})
	q.Push(Event{At: d.End, Priority: PriorityDisruptionEnd, Kind: EvDisruptionEnd, Payload: payload})
}

// scheduleScriptDisruptions validates every script entry and schedules its
// events in script order. Injected Disruptions (M2) call scheduleDisruption
// and activate directly with their own ActiveDisruption.
func scheduleScriptDisruptions(sc *Scenario, q *EventQueue) error {
	for i := range sc.DisruptionScript {
		d, err := resolveScriptDisruption(sc, i)
		if err != nil {
			return err
		}
		scheduleDisruption(q, d)
	}
	return nil
}

// activateDisruption handles EvDisruptionOnset.
func (e *stepper) activateDisruption(ev Event) error {
	var d ActiveDisruption
	switch uint8(ev.Payload.Aux) {
	case OriginScript:
		if ev.Payload.Ref < 0 || ev.Payload.Ref >= int64(len(e.sc.DisruptionScript)) {
			return fmt.Errorf("%w: no script entry %d", ErrBadDisruption, ev.Payload.Ref)
		}
		var err error
		if d, err = resolveScriptDisruption(e.sc, int(ev.Payload.Ref)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: injected disruptions are not supported yet", ErrBadDisruption)
	}
	e.activate(d)
	return nil
}

// activate appends d to State.Disruptions (kept sorted by Onset, Origin,
// InputSeq) and, for a supplier_outage, moves every open purchase order of the
// affected supplier whose due time falls inside the outage to the outage end
// plus the supplier lead time (Req 8.3).
func (e *stepper) activate(d ActiveDisruption) {
	e.st.Disruptions = append(e.st.Disruptions, d)
	sort.SliceStable(e.st.Disruptions, func(i, j int) bool {
		a, b := e.st.Disruptions[i], e.st.Disruptions[j]
		if a.Onset != b.Onset {
			return a.Onset < b.Onset
		}
		if a.Origin != b.Origin {
			return a.Origin < b.Origin
		}
		return a.InputSeq < b.InputSeq
	})
	if d.Kind != DisruptionSupplierOutage {
		return
	}
	for i := range e.st.PurchaseOs {
		po := &e.st.PurchaseOs[i]
		if !outageCovers(d, po.SKU, po.Supplier) || po.DueAt < d.Onset || po.DueAt >= d.End {
			continue
		}
		sup, err := LookupSupplier(e.sc, po.SKU, po.Supplier)
		if err != nil {
			continue // cannot happen for a PO placed by PlacePurchaseOrder
		}
		po.DueAt = d.End + SimMinute(sup.LeadTimeTicks*MinutesPerTick)
		e.q.Push(Event{
			At:       po.DueAt,
			Priority: PriorityPurchaseReceipt,
			Kind:     EvPurchaseReceipt,
			Payload:  EventPayload{Ref: int64(po.ID), SKU: po.SKU, Qty: po.Qty},
		})
	}
}

// expireDisruption handles EvDisruptionEnd: it removes the disruption with the
// event's origin and input sequence from State.Disruptions.
func (e *stepper) expireDisruption(ev Event) {
	for i, d := range e.st.Disruptions {
		if d.Origin == uint8(ev.Payload.Aux) && int64(d.InputSeq) == ev.Payload.Ref {
			e.st.Disruptions = append(e.st.Disruptions[:i], e.st.Disruptions[i+1:]...)
			return
		}
	}
}

// outageCovers reports whether outage d applies to purchase orders of the
// given SKU and supplier role.
func outageCovers(d ActiveDisruption, sku int64, role uint8) bool {
	return d.Kind == DisruptionSupplierOutage && uint8(d.MagnitudeBps) == role && (d.Target == -1 || d.Target == sku)
}

// outageEnd returns the latest End among outages active at minute at for the
// SKU and supplier role, and whether any is active.
func outageEnd(ds []ActiveDisruption, sku int64, role uint8, at SimMinute) (SimMinute, bool) {
	var end SimMinute
	found := false
	for _, d := range ds {
		if outageCovers(d, sku, role) && d.Onset <= at && at < d.End && (!found || d.End > end) {
			end, found = d.End, true
		}
	}
	return end, found
}
