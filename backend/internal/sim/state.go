package sim

// State is the snapshot-complete simulation state at a Tick_Boundary
// (design.md, "State"). Together with the Scenario and the Inputs whose
// effective Tick is later than the State's Tick it is sufficient to continue
// deterministic execution (Req 14.6, 28.6).
//
// The State holds simulation data only. It deliberately has no world or branch
// identifier, label, manager kind, provenance, timestamp, idempotency key,
// rationale, model name or engine version string (Req 12.4, INV-15). SKUs are
// referred to by their index in Scenario.SKUs.
//
// Collections are slices that Encode writes in canonical order (see encode.go);
// maps are never used so no output depends on map iteration order.
type State struct {
	Clock       SimMinute          // always a Tick_Boundary between steps
	Inventory   []SKUInventory     // sorted by SKU; Value == 0 iff OnHand == 0
	Orders      OrderBook          // open orders and conservation counters
	OrderSeq    uint64             // next order creation sequence counter
	PurchaseOs  []PurchaseOrder    // open purchase orders, sorted by ID
	POSeq       uint64             // next purchase-order identifier counter
	Workforce   Workforce          // incl. temp cohorts still in onboarding
	Ledger      Ledger             // opening balances and posted entries
	Disruptions []ActiveDisruption // active disruptions, sorted (Onset, Origin, InputSeq)
	Prices      []SKUPrice         // current price and marketing spend, sorted by SKU
	Streams     []NamedStream      // full stream state, sorted by name
	Pending     []Event            // pending Event_Queue entries in (At, Priority, Seq) order
	EventSeq    uint64             // Event_Queue insertion sequence counter
}

// Tick returns the Tick whose boundary is the State's Clock.
func (s State) Tick() Tick { return Tick(int64(s.Clock) / MinutesPerTick) }

// SKUInventory is the stock of one SKU with its moving-average value and the
// counters that make inventory conservation checkable (INV-8, INV-19):
// OpeningUnits + Received - Shipped - WrittenOff == OnHand.
type SKUInventory struct {
	SKU          int64 // index into Scenario.SKUs
	OnHand       int64 // units
	Value        Cents // Inventory_Value: total value of OnHand at moving-average cost
	OpeningUnits int64
	Received     int64 // cumulative units received
	Shipped      int64 // cumulative units shipped
	WrittenOff   int64 // cumulative units written off
}

// Order is an open (neither shipped nor cancelled) customer order. ID is the
// order creation sequence number, the simulation-internal tie-breaker for
// fulfilment order.
type Order struct {
	ID         uint64
	SKU        int64
	Qty        int64
	UnitPrice  Cents // price at creation
	CreatedAt  SimMinute
	PromisedBy SimMinute
}

// SettledOrder is the record kept of an order that left the Open list, so the
// Metrics_Calculator can decide at any later Tick whether it was due and
// whether it shipped on time (Req 11.3, 11.5). ShippedAt is the sim minute of
// shipment, or NotShipped for a cancelled order.
type SettledOrder struct {
	ID         uint64
	Qty        int64
	PromisedBy SimMinute
	ShippedAt  SimMinute
}

// NotShipped is the SettledOrder.ShippedAt value of a cancelled order.
const NotShipped SimMinute = -1

// OrderBook holds open orders and the counters for order conservation (INV-9):
// Created == len(Open) + Shipped + Cancelled. Settled holds one record per
// shipped or cancelled order, so len(Settled) == Shipped + Cancelled.
type OrderBook struct {
	Open           []Order        // sorted by ID
	Settled        []SettledOrder // shipped and cancelled orders, sorted by ID
	Created        int64
	Shipped        int64
	ShippedLate    int64 // subset of Shipped shipped after PromisedBy
	Cancelled      int64
	UnitsCreated   int64
	UnitsShipped   int64
	UnitsCancelled int64
}

// PurchaseOrder is an open supplier order.
type PurchaseOrder struct {
	ID        uint64
	SKU       int64
	Supplier  uint8 // supplier role: 0 primary, 1 backup
	Qty       int64
	UnitCost  Cents // Supplier_Unit_Cost at placement
	PlacedAt  SimMinute
	DueAt     SimMinute
	Expedited uint8 // 0 or 1
}

// TempCohort is a group of temporary workers hired together. They contribute
// capacity from ReadyAt on and are paid from ReadyAt (onboarding is unpaid
// capacity-wise; wage accrual is defined by the engine tasks).
type TempCohort struct {
	Count   int64
	HiredAt SimMinute
	ReadyAt SimMinute
}

// Workforce is the warehouse labour state.
type Workforce struct {
	Regular       int64 // regular workers on shift capacity (before absence disruptions)
	OvertimeHours int64 // overtime hours scheduled per worker for the current Tick
	Temps         []TempCohort
}

// ActiveDisruption is a Disruption that is currently in effect. Origin is 0 for
// a Disruption_Script entry and 1 for an injected Disruption; InputSeq is the
// script index or the input sequence number respectively.
type ActiveDisruption struct {
	Kind         uint8 // matches the disruption kind code used by the engine
	Origin       uint8
	InputSeq     uint64
	Onset        SimMinute
	End          SimMinute
	MagnitudeBps Bps
	Target       int64 // SKU index, or -1 when the Disruption has no target
}

// SKUPrice is the current selling price and hourly marketing spend of a SKU.
type SKUPrice struct {
	SKU            int64
	PriceCents     Cents
	MarketingCents Cents // marketing spend accrued per hour
}

// NamedStream carries the complete internal state of one random stream so a
// resumed snapshot reproduces every later draw (Req 14.6, 28.6).
type NamedStream struct {
	Name  StreamName
	State StreamState
}

// AccountID identifies a ledger account. The fixed chart of accounts and its
// constants are defined by the ledger task (2.12).
type AccountID uint8

// LedgerLine is one debit or credit line of an entry; exactly one of Debit and
// Credit is non-zero for a normal line.
type LedgerLine struct {
	Account AccountID
	Debit   Cents
	Credit  Cents
}

// LedgerEntry is one balanced posting. Cause identifies why it was posted
// (cause kind plus reference such as an order or purchase-order ID). Human
// readable descriptions are derived from the cause and are not state.
type LedgerEntry struct {
	Tick     Tick
	At       SimMinute
	Cause    uint8
	CauseRef int64
	Lines    []LedgerLine
}

// Ledger is the minimal ledger state: opening balances and the posted entries
// in posting order. Posting rules, accounts and running balances belong to
// ledger.go (task 2.12), which extends this type.
type Ledger struct {
	OpeningCash   Cents
	OpeningEquity Cents
	Entries       []LedgerEntry
}

// Queue rebuilds the Event_Queue from the State's pending events and counter.
func (s State) Queue() (*EventQueue, error) {
	return RestoreEventQueue(s.Pending, s.EventSeq)
}

// SetQueue copies the queue's pending events and sequence counter into s.
func (s *State) SetQueue(q *EventQueue) {
	s.Pending = q.Pending()
	s.EventSeq = q.SeqCounter()
}
