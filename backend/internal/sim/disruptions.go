package sim

import (
	"encoding/binary"
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
// effect for sim minutes [Onset, End). For supplier_outage, Role is the
// targeted supplier role code (SupplierPrimary or SupplierBackup), MagnitudeBps
// is 0 and Target is -1 (every SKU of that role). For demand_surge, Target is
// the SKU index or -1 when the entry has no target.
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
			d.Role = SupplierPrimary
		case "backup":
			d.Role = SupplierBackup
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
// and input sequence so the handler can find the disruption again. A script
// entry is re-resolved from the Scenario at activation; an injected one has no
// Scenario entry, so its parameters travel in the onset payload (see
// injectedPayload).
func scheduleDisruption(q *EventQueue, d ActiveDisruption) {
	payload := EventPayload{Ref: int64(d.InputSeq), Aux: int64(d.Origin)}
	onset := payload
	if d.Origin == OriginInjected {
		onset = injectedPayload(d)
	}
	q.Push(Event{At: d.Onset, Priority: PriorityDisruptionStart, Kind: EvDisruptionOnset, Payload: onset})
	q.Push(Event{At: d.End, Priority: PriorityDisruptionEnd, Kind: EvDisruptionEnd, Payload: payload})
}

// injectedPayload packs an injected Disruption into an onset payload:
// Ref = input sequence, SKU = target, Qty = magnitude, and Aux = origin (bits
// 0..7) | kind (8..15) | role (16..23) | duration in Ticks (24..).
func injectedPayload(d ActiveDisruption) EventPayload {
	dur := (int64(d.End) - int64(d.Onset)) / MinutesPerTick
	return EventPayload{
		Ref: int64(d.InputSeq), SKU: d.Target, Qty: int64(d.MagnitudeBps),
		Aux: int64(d.Origin) | int64(d.Kind)<<8 | int64(d.Role)<<16 | dur<<24,
	}
}

// unpackInjected is the inverse of injectedPayload; onset is the event minute.
func unpackInjected(onset SimMinute, p EventPayload) ActiveDisruption {
	return ActiveDisruption{
		Kind: uint8(p.Aux >> 8), Origin: uint8(p.Aux), Role: uint8(p.Aux >> 16),
		InputSeq: uint64(p.Ref), Onset: onset, End: onset + SimMinute((p.Aux>>24)*MinutesPerTick),
		MagnitudeBps: Bps(p.Qty), Target: p.SKU,
	}
}

// scheduleScriptDisruptions validates every script entry and schedules its
// events in script order. Injected Disruptions are scheduled by StepTick when
// their Input is applied (see scheduleInjected).
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

// activateDisruption handles EvDisruptionOnset. The Tick's severity draw
// (see stepper.severity) jitters the magnitude of surges and absences.
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
	case OriginInjected:
		d = unpackInjected(ev.At, ev.Payload)
	default:
		return fmt.Errorf("%w: unknown disruption origin %d", ErrBadDisruption, uint8(ev.Payload.Aux))
	}
	d.MagnitudeBps = jitterMagnitude(d.Kind, d.MagnitudeBps, e.severity)
	e.activate(d)
	return nil
}

// SeverityJitterBps is the half-width, in basis points of the scripted or
// injected magnitude, of the severity jitter drawn per Tick from the
// disruption stream: a demand_surge or workforce_absence takes effect at
// magnitude * (1 + j), j uniform in [-SeverityJitterBps, +SeverityJitterBps].
const SeverityJitterBps = 500

// DisruptionDrawsPerTick is the number of disruption-stream draws consumed per
// Tick. It is fixed: it never depends on commands or on whether a Disruption
// is active or starting (Req 16.5, INV-12).
const DisruptionDrawsPerTick = 1

// jitterMagnitude applies the severity draw to a magnitude. A supplier_outage
// has no magnitude and is returned unchanged. The result is rounded half to
// even; an absence stays within 0..10000 and a surge stays positive.
func jitterMagnitude(kind uint8, mag Bps, draw uint64) Bps {
	if kind != DisruptionDemandSurge && kind != DisruptionWorkforceAbsence {
		return mag
	}
	j := int64(draw%uint64(2*SeverityJitterBps+1)) - SeverityJitterBps
	m := MulDiv(int64(mag), BpsScale+j, BpsScale)
	if kind == DisruptionWorkforceAbsence && m > BpsScale {
		m = BpsScale
	}
	if m < 1 && mag > 0 {
		m = 1
	}
	return Bps(m)
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
	return d.Kind == DisruptionSupplierOutage && d.Role == role && (d.Target == -1 || d.Target == sku)
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

// DisruptionInput is the payload of an injected Disruption Input (Req 16.2).
// The start Tick is the Input's EffectiveTick: the Disruption takes effect in
// that Tick, exactly like a script entry with OnsetTick equal to it.
//
//	demand_surge:      Target = SKU index or -1 (all), MagnitudeBps multiplier
//	supplier_outage:   Role = supplier role, Target = SKU index or -1, MagnitudeBps 0
//	workforce_absence: Target -1, MagnitudeBps = share of regular workers absent
type DisruptionInput struct {
	Kind          uint8 // DisruptionDemandSurge, DisruptionSupplierOutage or DisruptionWorkforceAbsence
	Target        int64
	Role          uint8
	DurationTicks int64
	MagnitudeBps  Bps
}

// Bounds of an injected Disruption, enforced by ValidateDisruptionInput.
const (
	// MinDisruptionDurationTicks is the shortest injected duration. The
	// longest is the Scenario horizon.
	MinDisruptionDurationTicks = 1
	// MaxSurgeBps is the largest demand_surge multiplier (10x).
	MaxSurgeBps Bps = 100000
)

// ValidateDisruptionInput checks an injected Disruption against the engine's
// bounds for a Scenario and returns an error wrapping ErrBadDisruption that
// names the allowed range. startTick is the Input's effective Tick.
func ValidateDisruptionInput(sc *Scenario, in DisruptionInput, startTick Tick) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrBadDisruption, fmt.Sprintf(format, a...))
	}
	if startTick < 1 || int64(startTick) > sc.HorizonTicks {
		return bad("start tick %d outside 1..%d", startTick, sc.HorizonTicks)
	}
	if in.DurationTicks < MinDisruptionDurationTicks || in.DurationTicks > sc.HorizonTicks {
		return bad("duration %d ticks outside %d..%d", in.DurationTicks, MinDisruptionDurationTicks, sc.HorizonTicks)
	}
	if in.Target < -1 || in.Target >= int64(len(sc.SKUs)) {
		return bad("target %d outside -1..%d", in.Target, len(sc.SKUs)-1)
	}
	switch in.Kind {
	case DisruptionDemandSurge:
		if in.Role != 0 {
			return bad("demand_surge takes no supplier role")
		}
		if in.MagnitudeBps < 1 || in.MagnitudeBps > MaxSurgeBps {
			return bad("demand_surge magnitude %d bps outside 1..%d", in.MagnitudeBps, MaxSurgeBps)
		}
	case DisruptionSupplierOutage:
		if in.Role != SupplierPrimary && in.Role != SupplierBackup {
			return bad("supplier role %d is not primary (%d) or backup (%d)", in.Role, SupplierPrimary, SupplierBackup)
		}
		if in.MagnitudeBps != 0 {
			return bad("supplier_outage takes no magnitude")
		}
	case DisruptionWorkforceAbsence:
		if in.Role != 0 || in.Target != -1 {
			return bad("workforce_absence takes no supplier role or target")
		}
		if in.MagnitudeBps < 0 || in.MagnitudeBps > BpsScale {
			return bad("workforce_absence magnitude %d bps outside 0..%d", in.MagnitudeBps, BpsScale)
		}
	default:
		return bad("unknown kind code %d", in.Kind)
	}
	return nil
}

// scheduleInjected validates the Disruption Input inp, which takes effect at
// the Tick starting at minute start, and schedules its activation and expiry.
// Entries are pushed in input sequence order by StepTick, after every script
// entry (pushed at Init), so same-minute activations run script entries first
// and injected ones by ascending sequence (Req 5.8). It draws nothing.
func scheduleInjected(sc *Scenario, q *EventQueue, start SimMinute, inp Input) error {
	if err := ValidateDisruptionInput(sc, inp.Disruption, inp.EffectiveTick); err != nil {
		return fmt.Errorf("input %d: %w", inp.Seq, err)
	}
	p := inp.Disruption
	scheduleDisruption(q, ActiveDisruption{
		Kind: p.Kind, Origin: OriginInjected, InputSeq: inp.Seq,
		Onset: start, End: start + SimMinute(p.DurationTicks*MinutesPerTick),
		MagnitudeBps: p.MagnitudeBps, Target: p.Target, Role: p.Role,
	})
	return nil
}

const disruptionInputEncodedLen = 1 + 8 + 1 + 8 + 8

// EncodeDisruptionInput returns the canonical fixed-width big-endian encoding
// of d (the Input_Log payload of an injected Disruption): kind, target, role,
// duration, magnitude.
func EncodeDisruptionInput(d DisruptionInput) []byte {
	e := encoder{b: make([]byte, 0, disruptionInputEncodedLen)}
	e.u8(d.Kind)
	e.i64(d.Target)
	e.u8(d.Role)
	e.i64(d.DurationTicks)
	e.i64(int64(d.MagnitudeBps))
	return e.b
}

// DecodeDisruptionInput is the inverse of EncodeDisruptionInput.
func DecodeDisruptionInput(b []byte) (DisruptionInput, error) {
	if len(b) != disruptionInputEncodedLen {
		return DisruptionInput{}, fmt.Errorf("%w: encoded length %d, want %d", ErrBadDisruption, len(b), disruptionInputEncodedLen)
	}
	return DisruptionInput{
		Kind:          b[0],
		Target:        int64(binary.BigEndian.Uint64(b[1:9])),
		Role:          b[9],
		DurationTicks: int64(binary.BigEndian.Uint64(b[10:18])),
		MagnitudeBps:  Bps(binary.BigEndian.Uint64(b[18:26])),
	}, nil
}

// DecisionWindowOpenInjected is DecisionWindowOpen plus the onset Ticks of
// injected Disruptions (the start Ticks of the branch's Disruption Inputs).
// Every Disruption onset opens a Decision_Window (Req 16.4); a Tick that is
// both a script and an injected onset yields WindowDisruptionOnset once.
func DecisionWindowOpenInjected(sc Scenario, tick Tick, injectedOnsets []Tick) (open bool, reasons []WindowReason) {
	open, reasons = DecisionWindowOpen(sc, tick)
	if tick < 0 || int64(tick) >= sc.HorizonTicks {
		return open, reasons
	}
	for _, r := range reasons {
		if r == WindowDisruptionOnset {
			return open, reasons
		}
	}
	for _, t := range injectedOnsets {
		if t == tick {
			return true, append(reasons, WindowDisruptionOnset)
		}
	}
	return open, reasons
}

// DecisionWindowOpenState is DecisionWindowOpenInjected with the injected
// onsets read from the State: an injected Disruption whose onset Tick is the
// State's Tick is still listed in State.Disruptions at that boundary.
func DecisionWindowOpenState(sc Scenario, st State) (open bool, reasons []WindowReason) {
	var onsets []Tick
	for _, d := range st.Disruptions {
		if d.Origin == OriginInjected {
			onsets = append(onsets, TickOf(d.Onset))
		}
	}
	return DecisionWindowOpenInjected(sc, st.Tick(), onsets)
}
