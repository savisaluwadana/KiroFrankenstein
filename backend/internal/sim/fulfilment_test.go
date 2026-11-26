package sim

import (
	"reflect"
	"testing"
)

// fulfilScenario returns the generic valid scenario with demand switched off,
// so the only events that change state in Tick 1 are the fulfilment event
// (minute 30) and the order cancellation check (minute 55, threshold 48 ticks).
func fulfilScenario() Scenario {
	sc := *validScenario()
	sc.Demand.BaseArrivalsPerTick = 0
	return sc
}

// stepOne advances one Tick and fails the test on error.
func stepOne(t *testing.T, sc Scenario, st State) (State, []Outcome, FulfilmentConstraintReport) {
	t.Helper()
	next, j, rep, err := StepTick(sc, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	return next, j, rep
}

func TestFulfilmentLateShipmentPostsServiceCredit(t *testing.T) {
	sc := fulfilScenario()
	st := mustInit(t, sc, 1)
	// Opening: 50 units at 400 = 20000. Order 1 was promised at minute 10 and
	// ships at minute 30 (20 minutes late); order 2 is promised at minute 60.
	st.Orders.Open = []Order{
		{ID: 1, SKU: 0, Qty: 1, UnitPrice: 1000, CreatedAt: 0, PromisedBy: 10},
		{ID: 2, SKU: 0, Qty: 1, UnitPrice: 1000, CreatedAt: 0, PromisedBy: 60},
	}
	st.Orders.Created, st.Orders.UnitsCreated, st.OrderSeq = 2, 2, 3
	before := len(st.Ledger.Entries)

	next, journal, rep := stepOne(t, sc, st)

	// Shipments (minute 30) post before the accruals (minute 58), which are
	// not counted here.
	for _, e := range next.Ledger.Entries[before:] {
		if e.Cause != CauseShipment && e.Cause != CauseLabor && e.Cause != CauseMarketing && e.Cause != CauseOverhead {
			t.Fatalf("unexpected entry cause %d", e.Cause)
		}
	}
	if next.Ledger.Entries[before+1].Cause != CauseShipment {
		t.Fatalf("want one entry per shipment, got %+v", next.Ledger.Entries[before:])
	}
	late := next.Ledger.Entries[before]
	if late.Cause != CauseShipment || late.CauseRef != 1 || late.At != 30 || late.Tick != 1 {
		t.Errorf("late entry header = %+v", late)
	}
	wantLate := []LedgerLine{
		{AccountCash, 1000, 0}, {AccountRevenue, 0, 1000},
		{AccountCostOfGoodsSold, 400, 0}, {AccountInventory, 0, 400},
		{AccountFreightExpense, 300, 0}, {AccountCash, 0, 300},
		{AccountServiceCreditExpense, 200, 0}, {AccountCash, 0, 200},
	}
	if !reflect.DeepEqual(late.Lines, wantLate) {
		t.Errorf("late lines = %+v\nwant %+v", late.Lines, wantLate)
	}
	if err := late.Validate(); err != nil {
		t.Error(err)
	}
	onTime := next.Ledger.Entries[before+1]
	for _, l := range onTime.Lines {
		if l.Account == AccountServiceCreditExpense {
			t.Errorf("on-time order was charged a service credit: %+v", onTime.Lines)
		}
	}
	// COGS of order 2 is 19600*1/49 = 400 exactly.
	inv := next.Inventory[0]
	if inv.OnHand != 48 || inv.Value != 19200 || inv.Shipped != 2 {
		t.Errorf("inventory = %+v", inv)
	}
	ob := next.Orders
	if ob.Shipped != 2 || ob.ShippedLate != 1 || ob.UnitsShipped != 2 || len(ob.Open) != 0 ||
		ob.Created != int64(len(ob.Open))+ob.Shipped+ob.Cancelled {
		t.Errorf("order book = %+v", ob)
	}
	if d, c := next.Ledger.Totals(); d != c {
		t.Errorf("ledger unbalanced: %d vs %d", d, c)
	}
	// Cash: opening + 2*1000 - 2*300 - 200, less the Tick's accruals
	// (wages 10*2000 and overhead 100).
	if got, want := next.Ledger.Balance(AccountCash), sc.OpeningCashCents+2000-600-200-20000-100; got != want {
		t.Errorf("cash = %d, want %d", got, want)
	}
	var ships []Outcome
	for _, o := range journal {
		if o.Kind == OutcomeShipment {
			ships = append(ships, o)
		}
	}
	if len(ships) != 2 || ships[0].Ref != 1 || ships[0].Aux != 20 || ships[1].Ref != 2 || ships[1].Aux != 0 {
		t.Errorf("shipment outcomes = %+v", ships)
	}
	if rep.ShippedOrders != 2 || len(rep.SKUs) != 0 {
		t.Errorf("report = %+v", rep)
	}
}

func TestFulfilmentShipsInPromiseOrderAndHalfEvenCost(t *testing.T) {
	sc := fulfilScenario()
	sc.Workforce.UnitsPerWorkerHour = 1
	st := mustInit(t, sc, 1)
	st.Workforce.Regular = 1 // capacity 1
	// 2 units worth 5 cents: shipping 1 costs 5*1/2 = 2.5, rounded half to even = 2.
	st.Inventory[0].OnHand, st.Inventory[0].Value = 2, 5
	st.Orders.Open = []Order{
		{ID: 1, SKU: 0, Qty: 1, UnitPrice: 1000, PromisedBy: 200},
		{ID: 2, SKU: 0, Qty: 1, UnitPrice: 1000, PromisedBy: 100},
	}
	next, _, rep := stepOne(t, sc, st)
	if len(next.Orders.Open) != 1 || next.Orders.Open[0].ID != 1 {
		t.Fatalf("open = %+v, want only order 1 left", next.Orders.Open)
	}
	if c := rep.For(0); c.CapacityBoundOrders != 1 || !reflect.DeepEqual(c.CapacityBoundIDs, []uint64{1}) {
		t.Errorf("report = %+v", rep)
	}
	if inv := next.Inventory[0]; inv.OnHand != 1 || inv.Value != 3 {
		t.Errorf("inventory = %+v, want 1 unit worth 3", inv)
	}
	// The last unit takes the full remaining value even with more capacity.
	st2 := next
	st2.Workforce.Regular = 5
	end, _, _ := stepOne(t, sc, st2)
	if inv := end.Inventory[0]; inv.OnHand != 0 || inv.Value != 0 {
		t.Errorf("after last unit inventory = %+v", inv)
	}
}

func TestFulfilmentReportListsStockAndCapacityBound(t *testing.T) {
	sc := fulfilScenario()
	sc.Workforce.UnitsPerWorkerHour = 2
	st := mustInit(t, sc, 1)
	// StepTick does not validate; add the second SKU after Init.
	sc.SKUs = append(sc.SKUs, SKUSpec{ID: "b", ReferencePriceCents: 1000, PromiseTicks: 24})
	st.Workforce.Regular = 1 // capacity 2
	st.Inventory = append(st.Inventory, SKUInventory{SKU: 1, OnHand: 1, Value: 400, OpeningUnits: 1})
	st.Prices = append(st.Prices, SKUPrice{SKU: 1, PriceCents: 1000})
	st.Orders.Open = []Order{
		{ID: 1, SKU: 0, Qty: 1, UnitPrice: 1000, PromisedBy: 100},
		{ID: 2, SKU: 1, Qty: 1, UnitPrice: 1000, PromisedBy: 110},
		{ID: 3, SKU: 1, Qty: 1, UnitPrice: 1000, PromisedBy: 120}, // stock-bound: SKU 1 has one unit
		{ID: 4, SKU: 0, Qty: 1, UnitPrice: 1000, PromisedBy: 130}, // capacity-bound: stock left, capacity used up
		{ID: 5, SKU: 0, Qty: 1, UnitPrice: 1000, PromisedBy: 140}, // capacity-bound
	}
	next, _, rep := stepOne(t, sc, st)

	want := FulfilmentConstraintReport{
		Tick: 1, Capacity: 2, CapacityUsed: 2, ShippedOrders: 2,
		SKUs: []SKUConstraint{
			{SKU: 0, CapacityBoundOrders: 2, CapacityBoundUnits: 2, CapacityBoundIDs: []uint64{4, 5}},
			{SKU: 1, StockBoundOrders: 1, StockBoundUnits: 1, StockBoundIDs: []uint64{3}},
		},
	}
	if !reflect.DeepEqual(rep, want) {
		t.Errorf("report = %+v\nwant %+v", rep, want)
	}
	if ids := []uint64{next.Orders.Open[0].ID, next.Orders.Open[1].ID, next.Orders.Open[2].ID}; len(next.Orders.Open) != 3 ||
		!reflect.DeepEqual(ids, []uint64{3, 4, 5}) {
		t.Errorf("open = %+v", next.Orders.Open)
	}
	// Inventory conservation (INV-8) per SKU.
	for _, k := range next.Inventory {
		if k.OpeningUnits+k.Received-k.Shipped-k.WrittenOff != k.OnHand || k.OnHand < 0 {
			t.Errorf("sku %d not conserved: %+v", k.SKU, k)
		}
	}
	if next.Inventory[1].OnHand != 0 || next.Inventory[1].Value != 0 {
		t.Errorf("sku 1 = %+v", next.Inventory[1])
	}
}

func TestFulfilmentCapacity(t *testing.T) {
	sc := fulfilScenario() // 20 units per worker hour
	st := mustInit(t, sc, 1)
	st.Workforce = Workforce{Regular: 10, OvertimeHours: 1, Temps: []TempCohort{{Count: 2, HiredAt: 0, ReadyAt: 100}}}
	if got := fulfilmentCapacity(&sc, &st, 30); got != 400 {
		t.Errorf("before temp ready = %d, want 400", got)
	}
	if got := fulfilmentCapacity(&sc, &st, 100); got != 440 {
		t.Errorf("when temp ready = %d, want 440", got)
	}
	// 25 % absence: 10*2500/10000 = 2.5 rounds half to even to 2 workers, so 8 on shift.
	st.Disruptions = []ActiveDisruption{{Kind: DisruptionWorkforceAbsence, Onset: 0, End: 60, MagnitudeBps: 2500, Target: -1}}
	if got := fulfilmentCapacity(&sc, &st, 30); got != 320 {
		t.Errorf("during absence = %d, want 320", got)
	}
	if got := fulfilmentCapacity(&sc, &st, 60); got != 400 {
		t.Errorf("after absence = %d, want 400", got)
	}
}
