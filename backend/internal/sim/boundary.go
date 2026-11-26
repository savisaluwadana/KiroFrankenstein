package sim

import "fmt"

// WindowReason says why a Decision_Window is open at a Tick (Req 18.1).
type WindowReason uint8

// Window reasons. Append only.
const (
	// WindowCadence: the Tick is a multiple of Scenario.DecisionCadenceTicks
	// (Tick 0 included, so the first Commands can take effect at Tick 1).
	WindowCadence WindowReason = iota + 1
	// WindowDisruptionOnset: a Disruption_Script entry has this onset Tick,
	// that is the Tick in which the Disruption first takes effect.
	WindowDisruptionOnset
	// WindowFork: the fork Tick of a child Branch (Req 15.7). The engine cannot
	// know it; the app layer adds it when it opens the child's window.
	WindowFork
)

// DecisionWindowOpen reports whether a Decision_Window opens at the
// Tick_Boundary of tick and why, in ascending reason order (Req 18.1). It
// looks only at the Scenario: cadence multiples and Disruption_Script onset
// Ticks. Injected Disruption onsets and fork Ticks are known to the app layer,
// which adds WindowDisruptionOnset or WindowFork itself. No window opens at or
// after the horizon, because a Command there would take effect after it.
func DecisionWindowOpen(sc Scenario, tick Tick) (open bool, reasons []WindowReason) {
	if tick < 0 || int64(tick) >= sc.HorizonTicks {
		return false, nil
	}
	if c := sc.DecisionCadenceTicks; c > 0 && int64(tick)%c == 0 {
		reasons = append(reasons, WindowCadence)
	}
	for _, d := range sc.DisruptionScript {
		if d.OnsetTick == int64(tick) {
			reasons = append(reasons, WindowDisruptionOnset)
			break
		}
	}
	return len(reasons) > 0, reasons
}

// MarkerKind classifies a Tick_Frame marker.
type MarkerKind uint8

// Marker kinds. Append only.
const (
	MarkerDecisionWindow  MarkerKind = iota + 1 // Ref = WindowReason
	MarkerDisruptionOnset                       // Ref = Disruption kind code
)

// FrameMarker is a timeline marker derived from the State and Scenario.
// Markers for Inputs taking effect are added by the app layer, which owns the
// Input log.
type FrameMarker struct {
	Kind MarkerKind
	Ref  int64
}

// SKUFrame is the per-SKU part of a Tick_Frame.
type SKUFrame struct {
	SKU          int64
	OnHand       int64
	InboundPOs   int64 // open purchase orders for the SKU
	InboundUnits int64 // units on those orders
}

// DisruptionFrame is an active Disruption as shown in a Tick_Frame.
type DisruptionFrame struct {
	Kind            uint8
	Origin          uint8
	InputSeq        uint64
	Target          int64
	MagnitudeBps    Bps
	Onset           SimMinute
	End             SimMinute
	TicksSinceOnset int64 // 0 in the onset Tick
}

// FrameMetrics (defined in metrics.go) is the Metrics_Calculator output of
// the frame's State.

// TickFrame is the read model of one Tick_Boundary used by the World_View. It
// holds only values derived from the State, the Scenario and the tick's
// Fulfilment_Constraint_Report, so equal inputs give equal frames.
type TickFrame struct {
	Tick            Tick
	Hash            [32]byte // State_Hash of the State at Tick
	SKUs            []SKUFrame
	WorkersOnShift  int64 // regular workers on shift after absences
	TempsOnShift    int64 // temporary workers past onboarding
	TempsOnboarding int64 // temporary workers hired but not yet ready
	OrderQueueLen   int64 // open orders
	Disruptions     []DisruptionFrame
	Report          FulfilmentConstraintReport
	Markers         []FrameMarker
	Metrics         FrameMetrics
}

// BuildTickFrame derives the Tick_Frame of st, which must be at a
// Tick_Boundary. report is the Fulfilment_Constraint_Report of the Tick that
// just ended (the zero report at Tick 0).
func BuildTickFrame(sc Scenario, st State, report FulfilmentConstraintReport) (TickFrame, error) {
	if !st.Clock.IsBoundary() {
		return TickFrame{}, ErrBadClock
	}
	tick := st.Tick()
	f := TickFrame{
		Tick:           tick,
		Hash:           Hash(st),
		WorkersOnShift: regularOnShift(&st, st.Clock),
		OrderQueueLen:  int64(len(st.Orders.Open)),
		Report:         report,
		Metrics:        ComputeMetrics(sc, st),
	}
	for _, inv := range st.Inventory {
		sf := SKUFrame{SKU: inv.SKU, OnHand: inv.OnHand}
		for _, po := range st.PurchaseOs {
			if po.SKU == inv.SKU {
				sf.InboundPOs++
				sf.InboundUnits += po.Qty
			}
		}
		f.SKUs = append(f.SKUs, sf)
	}
	for _, c := range st.Workforce.Temps {
		if c.ReadyAt <= st.Clock {
			f.TempsOnShift += c.Count
		} else {
			f.TempsOnboarding += c.Count
		}
	}
	// A Disruption whose End is this boundary is still listed (its expiry
	// event has At == Clock and has not run) but is no longer in effect.
	for _, d := range st.Disruptions {
		if d.End <= st.Clock {
			continue
		}
		f.Disruptions = append(f.Disruptions, DisruptionFrame{
			Kind: d.Kind, Origin: d.Origin, InputSeq: d.InputSeq, Target: d.Target,
			MagnitudeBps: d.MagnitudeBps, Onset: d.Onset, End: d.End,
			TicksSinceOnset: int64(tick) - int64(TickOf(d.Onset)),
		})
		if TickOf(d.Onset) == tick {
			f.Markers = append(f.Markers, FrameMarker{Kind: MarkerDisruptionOnset, Ref: int64(d.Kind)})
		}
	}
	if _, reasons := DecisionWindowOpen(sc, tick); len(reasons) > 0 {
		for _, r := range reasons {
			f.Markers = append(f.Markers, FrameMarker{Kind: MarkerDecisionWindow, Ref: int64(r)})
		}
	}
	return f, nil
}

// Step is the result of Advance: everything produced at one Tick_Boundary.
type Step struct {
	State    State
	Outcomes []Outcome
	Report   FulfilmentConstraintReport
	Frame    TickFrame
	// Window is true when the Scenario opens a Decision_Window at State's Tick
	// (see DecisionWindowOpen); the caller must not call Advance again until
	// that window has ended (Req 5.9, 18.2).
	Window        bool
	WindowReasons []WindowReason
}

// Advance runs StepTick for one Tick and then performs the Tick_Boundary
// sequence of Req 5.9 that belongs to the engine: the State is fixed (no event
// at or after the boundary has run), the State_Hash is computed, and the
// Tick_Frame and window flag are derived from that State. Snapshots, the
// Observation and metrics are the caller's (tasks 2.18, 2.19 and the app
// layer). It returns ErrHorizonReached when st is at the horizon. It does not
// modify st.
func Advance(sc Scenario, st State, inputs []Input) (Step, error) {
	next, outcomes, report, err := StepTick(sc, st, inputs)
	if err != nil {
		return Step{}, err
	}
	frame, err := BuildTickFrame(sc, next, report)
	if err != nil {
		return Step{}, fmt.Errorf("sim: build tick frame: %w", err)
	}
	open, reasons := DecisionWindowOpen(sc, next.Tick())
	return Step{
		State: next, Outcomes: outcomes, Report: report, Frame: frame,
		Window: open, WindowReasons: reasons,
	}, nil
}

// InitialFrame returns the Tick_Frame of the Tick 0 State and its window
// status, for a caller that starts a World.
func InitialFrame(sc Scenario, st State) (TickFrame, bool, error) {
	f, err := BuildTickFrame(sc, st, FulfilmentConstraintReport{Tick: st.Tick()})
	if err != nil {
		return TickFrame{}, false, err
	}
	open, _ := DecisionWindowOpen(sc, st.Tick())
	return f, open, nil
}
