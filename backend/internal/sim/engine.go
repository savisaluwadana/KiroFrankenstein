package sim

import (
	"errors"
	"fmt"
	"sort"
)

// EngineVersion identifies the within-tick offset table and event semantics
// below. Changing any offset, priority or draw count requires a new version.
const EngineVersion = 1

// Disruption kind codes stored in ActiveDisruption.Kind. Append only.
const (
	DisruptionDemandSurge      uint8 = 1
	DisruptionSupplierOutage   uint8 = 2
	DisruptionWorkforceAbsence uint8 = 3
)

// Named random streams (design.md, "Randomness Strategy"). The slice is in
// sorted name order, which is the canonical order of State.Streams.
const (
	StreamDemand     StreamName = "demand"
	StreamDisruption StreamName = "disruption"
	StreamSupplier   StreamName = "supplier"
	StreamWorkforce  StreamName = "workforce"
)

var streamNames = []StreamName{StreamDemand, StreamDisruption, StreamSupplier, StreamWorkforce}

// Event priorities for engine version 1 (lower runs first at equal At).
// PriorityCommandApply (0) is defined with the event queue.
const (
	PriorityDisruptionEnd   uint8 = 1
	PriorityDisruptionStart uint8 = 2
	PriorityPurchaseReceipt uint8 = 3
	PriorityDemandArrivals  uint8 = 4
	PriorityFulfilment      uint8 = 5
	PriorityOrderCancel     uint8 = 6
	PriorityAccrual         uint8 = 7
)

// Minute offsets into a Tick for the recurring hourly processes, engine
// version 1. Tick N covers minutes [(N-1)*60, N*60); offsets are measured
// from (N-1)*60 (design.md, "Within-tick minute offsets").
const (
	OffsetDemandArrivals int64 = 5
	OffsetFulfilment     int64 = 30
	OffsetOrderCancel    int64 = 55
	OffsetAccrual        int64 = 58
)

// recurring lists the per-tick events in the order they are first scheduled.
// Events that share an offset and priority (the three accruals) run in this
// order because the queue breaks the tie by insertion sequence.
var recurring = []struct {
	kind     EventKind
	offset   int64
	priority uint8
}{
	{EvDemandArrivals, OffsetDemandArrivals, PriorityDemandArrivals},
	{EvFulfilment, OffsetFulfilment, PriorityFulfilment},
	{EvOrderCancel, OffsetOrderCancel, PriorityOrderCancel},
	{EvLaborAccrual, OffsetAccrual, PriorityAccrual},
	{EvMarketingAccrual, OffsetAccrual, PriorityAccrual},
	{EvOverheadAccrual, OffsetAccrual, PriorityAccrual},
}

// Engine errors.
var (
	ErrInvalidScenario   = errors.New("sim: scenario cannot start a world")
	ErrHorizonReached    = errors.New("sim: horizon reached")
	ErrInputsUnsupported = errors.New("sim: input kind is not supported yet")
	ErrBadClock          = errors.New("sim: state clock is not a tick boundary")
	ErrBadInputTick      = errors.New("sim: input effective tick is not the tick being computed")
)

// InputKind says what an Input carries.
type InputKind uint8

// Input kinds. Append only.
const (
	InputCommand InputKind = iota
	InputDisruption
)

// Input is the engine-visible projection of a recorded input: effective Tick,
// sequence, kind and payload, with no metadata (so the hash cannot depend on
// it). A Command Input submitted in the Decision_Window at Tick N has
// EffectiveTick N+1: it is applied at minute N*60, the first event of the step
// that computes Tick N+1. Only the payload selected by Kind is read.
type Input struct {
	EffectiveTick Tick
	Seq           uint64
	Kind          InputKind
	Command       Command         // Kind == InputCommand
	Disruption    DisruptionInput // Kind == InputDisruption (placeholder until M2)
}

// DisruptionInput is the payload placeholder of an injected Disruption. The
// engine rejects InputDisruption with ErrInputsUnsupported until M2.
type DisruptionInput struct {
	Kind          uint8
	Target        int64
	DurationTicks int64
	MagnitudeBps  Bps
}

// OutcomeKind identifies an entry of the per-tick outcome journal.
type OutcomeKind uint8

// Outcome kinds. Append only.
const (
	// OutcomeDemand: one per SKU per Tick. Qty = Potential_Arrivals,
	// Ref = conversion in bps, Aux = orders created.
	OutcomeDemand OutcomeKind = iota
	// OutcomeOrderCancelled: Ref = order ID, Qty = units, Aux = age in minutes.
	// A cancellation creates no Ledger_Entry (Req 37.3).
	OutcomeOrderCancelled
	// OutcomeShipment: Ref = order ID, SKU = SKU index, Qty = units shipped,
	// Aux = minutes shipped after PromisedBy (0 when on time).
	OutcomeShipment
)

// Outcome is one entry of the outcome journal returned by StepTick. The
// journal is derived output; it is not part of State and is not hashed.
type Outcome struct {
	At   SimMinute
	Kind OutcomeKind
	SKU  int64
	Ref  int64
	Qty  int64
	Aux  int64
}

// Init builds the Tick 0 State for sc and seed: opening ledger entry,
// inventory, prices, the four named random streams, and the recurring events
// of Tick 1. It validates sc first.
func Init(sc Scenario, seed Seed) (State, error) {
	if err := ValidateScenario(&sc); err != nil {
		return State{}, fmt.Errorf("%w: %w", ErrInvalidScenario, err)
	}
	for i, k := range sc.SKUs {
		if k.ReferencePriceCents <= 0 {
			return State{}, fmt.Errorf("%w: skus[%d].referencePriceCents must be positive", ErrInvalidScenario, i)
		}
	}
	ledger, err := NewOpeningLedger(sc.OpeningCashCents, sc.SKUs)
	if err != nil {
		return State{}, fmt.Errorf("%w: %w", ErrInvalidScenario, err)
	}

	st := State{
		OrderSeq:  1,
		POSeq:     1,
		Workforce: Workforce{Regular: sc.Workforce.RegularWorkers},
		Ledger:    ledger,
	}
	for i, k := range sc.SKUs {
		idx := int64(i)
		st.Inventory = append(st.Inventory, SKUInventory{
			SKU:          idx,
			OnHand:       k.OpeningUnits,
			Value:        Cents(k.OpeningUnits) * k.OpeningUnitCostCents,
			OpeningUnits: k.OpeningUnits,
		})
		st.Prices = append(st.Prices, SKUPrice{SKU: idx, PriceCents: k.ReferencePriceCents})
	}
	for _, n := range streamNames {
		st.Streams = append(st.Streams, NamedStream{Name: n, State: NewStream(seed, n).State()})
	}

	var q EventQueue
	if err := scheduleScriptDisruptions(&sc, &q); err != nil {
		return State{}, fmt.Errorf("%w: %w", ErrInvalidScenario, err)
	}
	if sc.HorizonTicks >= 1 {
		for _, r := range recurring {
			scheduleRecurring(&q, r.kind, 1)
		}
	}
	st.SetQueue(&q)
	return st, nil
}

// scheduleRecurring pushes the event of the given recurring kind for tick.
func scheduleRecurring(q *EventQueue, kind EventKind, tick Tick) {
	for _, r := range recurring {
		if r.kind != kind {
			continue
		}
		q.Push(Event{
			At:       SimMinute((int64(tick)-1)*MinutesPerTick + r.offset),
			Priority: r.priority,
			Kind:     kind,
			Payload:  EventPayload{Ref: int64(tick)},
		})
		return
	}
}

// stepper holds the working copies used while advancing one Tick. All
// mutable simulation data lives in st, q and streams; nothing else survives
// the step, so the returned State is snapshot-complete.
type stepper struct {
	sc      *Scenario
	st      State
	q       *EventQueue
	streams []Stream // parallel to st.Streams
	journal []Outcome
	fcr     FulfilmentConstraintReport // set by the fulfilment event of the Tick
}

// StepTick advances in by exactly one Tick and returns the new State, the
// outcome journal and the fulfilment constraint report. It does not modify in.
// Every event with Sim_Time below the destination boundary is processed in
// (At, Priority, Seq) order (Req 5.3, 5.4). Every input must have EffectiveTick
// equal to the Tick being computed; its Command is applied at the start
// boundary at priority 0, in ascending input Seq order (Req 5.7). Inputs are
// assumed to have passed Validate; an input that cannot be applied returns an
// error. Applying inputs never draws from a random stream.
func StepTick(sc Scenario, in State, inputs []Input) (State, []Outcome, FulfilmentConstraintReport, error) {
	var fcr FulfilmentConstraintReport
	if !in.Clock.IsBoundary() {
		return State{}, nil, fcr, ErrBadClock
	}
	dest := in.Tick() + 1
	if int64(dest) > sc.HorizonTicks {
		return State{}, nil, fcr, ErrHorizonReached
	}

	q, err := in.Queue()
	if err != nil {
		return State{}, nil, fcr, err
	}
	e := &stepper{sc: &sc, st: in.clone(), q: q}
	for _, ns := range e.st.Streams {
		e.streams = append(e.streams, RestoreStream(ns.State))
	}

	ordered := append([]Input(nil), inputs...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })
	for _, inp := range ordered {
		if inp.EffectiveTick != dest {
			return State{}, nil, fcr, fmt.Errorf("%w: input %d has tick %d, computing %d", ErrBadInputTick, inp.Seq, inp.EffectiveTick, dest)
		}
		if inp.Kind != InputCommand {
			return State{}, nil, fcr, fmt.Errorf("%w: input %d kind %d", ErrInputsUnsupported, inp.Seq, inp.Kind)
		}
		q.Push(commandEvent(in.Clock, inp))
	}

	limit := dest.Boundary()
	for {
		ev, ok := q.Peek()
		if !ok || ev.At >= limit {
			break
		}
		q.Pop()
		if err := e.handle(ev); err != nil {
			return State{}, nil, fcr, err
		}
	}

	e.st.Clock = limit
	for i := range e.streams {
		e.st.Streams[i].State = e.streams[i].State()
	}
	e.st.SetQueue(q)
	return e.st, e.journal, e.fcr, nil
}

// handle dispatches one event and reschedules recurring events for the next
// Tick while it is still inside the horizon (no post-horizon simulation).
func (e *stepper) handle(ev Event) error {
	switch ev.Kind {
	case EvDemandArrivals:
		if err := e.demandArrivals(ev); err != nil {
			return err
		}
	case EvOrderCancel:
		e.cancelAgedOrders(ev.At)
	case EvFulfilment:
		if err := e.fulfil(ev.At); err != nil {
			return err
		}
	case EvLaborAccrual:
		if err := e.laborAccrual(ev.At); err != nil {
			return err
		}
	case EvMarketingAccrual:
		if err := e.marketingAccrual(ev.At); err != nil {
			return err
		}
	case EvOverheadAccrual:
		if err := e.overheadAccrual(ev.At); err != nil {
			return err
		}
	case EvCommandApply:
		return e.applyCommand(ev)
	case EvPurchaseReceipt:
		// One-shot event: its payload Ref is a PO ID, so never reschedule.
		return e.receive(ev)
	case EvDisruptionOnset:
		// One-shot event: Ref is the script index or input sequence.
		return e.activateDisruption(ev)
	case EvDisruptionEnd:
		e.expireDisruption(ev)
		return nil
	default:
		return fmt.Errorf("sim: event kind %d at minute %d has no handler", ev.Kind, ev.At)
	}
	if next := Tick(ev.Payload.Ref) + 1; int64(next) <= e.sc.HorizonTicks {
		scheduleRecurring(e.q, ev.Kind, next)
	}
	return nil
}

// stream returns the working stream with the given name.
func (e *stepper) stream(name StreamName) (*Stream, error) {
	for i, ns := range e.st.Streams {
		if ns.Name == name {
			return &e.streams[i], nil
		}
	}
	return nil, fmt.Errorf("sim: state has no %q stream", name)
}

// clone returns a deep copy of s that shares no mutable memory with it.
// Posted ledger lines are immutable and shared.
func (s State) clone() State {
	c := s
	c.Inventory = append([]SKUInventory(nil), s.Inventory...)
	c.Orders.Open = append([]Order(nil), s.Orders.Open...)
	c.PurchaseOs = append([]PurchaseOrder(nil), s.PurchaseOs...)
	c.Workforce.Temps = append([]TempCohort(nil), s.Workforce.Temps...)
	c.Ledger.Entries = append([]LedgerEntry(nil), s.Ledger.Entries...)
	c.Disruptions = append([]ActiveDisruption(nil), s.Disruptions...)
	c.Prices = append([]SKUPrice(nil), s.Prices...)
	c.Streams = append([]NamedStream(nil), s.Streams...)
	c.Pending = append([]Event(nil), s.Pending...)
	return c
}
