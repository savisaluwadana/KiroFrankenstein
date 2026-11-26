package sim

import (
	"reflect"
	"strings"
	"testing"
)

func TestMetricsZeroOrdersDueIs10000(t *testing.T) {
	sc := fulfilScenario()
	st := mustInit(t, sc, 1)
	m := ComputeMetrics(sc, st)
	if m.OrdersDue != 0 || m.ServiceLevel != 10000 || m.FillRate != 10000 {
		t.Errorf("zero due: %+v", m)
	}
	if m.NetProfit != 0 || m.NetCashFlow != 0 || m.Cash != sc.OpeningCashCents {
		t.Errorf("opening metrics = %+v", m)
	}
	if !m.Reconciliation.AccrualToCashHolds || !m.Reconciliation.BalanceIdentityHolds {
		t.Errorf("reconciliation = %+v", m.Reconciliation)
	}
}

func TestMetricsWorkedExample(t *testing.T) {
	sc := fulfilScenario()
	st := mustInit(t, sc, 1)
	// Order 1 (promised minute 10) ships late at minute 30; order 2
	// (promised minute 60) ships on time. Both are due at minute 60.
	st.Orders.Open = []Order{
		{ID: 1, SKU: 0, Qty: 1, UnitPrice: 1000, CreatedAt: 0, PromisedBy: 10},
		{ID: 2, SKU: 0, Qty: 1, UnitPrice: 1000, CreatedAt: 0, PromisedBy: 60},
	}
	st.Orders.Created, st.Orders.UnitsCreated, st.OrderSeq = 2, 2, 3
	next, _, _ := stepOne(t, sc, st)

	m := ComputeMetrics(sc, next)
	// Hand computed: revenue 2*1000; COGS 400 + 400 (opening 50 units worth
	// 20000: 20000/50, then 19600/49); freight 2*300; service credit 200 for
	// order 1; wages 10 workers * 2000; overhead 100; marketing 0.
	want := Metrics{
		Tick: 1, NetRevenue: 2000, CostOfGoodsSold: 800, FreightExpense: 600, VariableCost: 1400,
		Contribution: 600, LaborExpense: 20000, MarketingExpense: 0, OverheadExpense: 100,
		ServiceCreditExpense: 200, NetProfit: -19700,
		NetCashFlow: -18900, Cash: sc.OpeningCashCents - 18900, InventoryValue: 19200,
		OrdersDue: 2, OrdersDueOnTime: 1, UnitsDue: 2, UnitsDueShipped: 2,
		ServiceLevel: 5000, FillRate: 10000,
		Reconciliation: Reconciliation{
			OpeningInventory: 20000, InventoryChange: -800, ProfitLessCashFlow: -800,
			AccrualToCashHolds: true, OpeningEquity: next.Ledger.OpeningEquity, BalanceIdentityHolds: true,
		},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("metrics =\n%+v\nwant\n%+v", m, want)
	}
	if got := NetCashFlowRange(next.Ledger, 1, 1); got != m.NetCashFlow {
		t.Errorf("range cash flow = %d", got)
	}
	if got := NetCashFlowRange(next.Ledger, 2, 5); got != 0 {
		t.Errorf("empty range cash flow = %d", got)
	}
}

func TestMetricsDueCancelledBacklogAndRounding(t *testing.T) {
	sc := fulfilScenario()
	st := mustInit(t, sc, 1)
	st.Clock = Tick(2).Boundary() // 120
	st.Orders.Settled = []SettledOrder{
		{ID: 1, Qty: 1, PromisedBy: 50, ShippedAt: 40},          // due, on time
		{ID: 2, Qty: 2, PromisedBy: 60, ShippedAt: 100},         // due, late
		{ID: 3, Qty: 3, PromisedBy: 70, ShippedAt: NotShipped},  // due, cancelled
		{ID: 4, Qty: 1, PromisedBy: 500, ShippedAt: 110},        // shipped early, not due
		{ID: 5, Qty: 1, PromisedBy: 900, ShippedAt: NotShipped}, // cancelled, not due
	}
	st.Orders.Open = []Order{
		{ID: 6, Qty: 2, PromisedBy: 119}, // late backlog and due
		{ID: 7, Qty: 4, PromisedBy: 120}, // due, promise not yet passed
		{ID: 8, Qty: 5, PromisedBy: 121}, // not due
	}
	m := ComputeMetrics(sc, st)
	if m.OrdersDue != 5 || m.OrdersDueOnTime != 1 || m.UnitsDue != 12 || m.UnitsDueShipped != 3 {
		t.Errorf("due counts = %+v", m)
	}
	// 1/5 = 2000; 3/12 = 2500.
	if m.ServiceLevel != 2000 || m.FillRate != 2500 {
		t.Errorf("service %d fill %d", m.ServiceLevel, m.FillRate)
	}
	if m.BacklogOrders != 3 || m.BacklogUnits != 11 || m.LateBacklogOrders != 1 || m.LateBacklogUnits != 2 {
		t.Errorf("backlog = %+v", m)
	}
	// Half-to-even: 1/3 -> 3333, 2/3 -> 6667, and 1/8 = 1250 exact.
	if bpsRatio(1, 3) != 3333 || bpsRatio(2, 3) != 6667 || bpsRatio(0, 5) != 0 || bpsRatio(7, 0) != 10000 {
		t.Error("bpsRatio rounding")
	}
	if bpsRatio(1, 20000) != 0 || bpsRatio(3, 40000) != 1 {
		t.Errorf("bpsRatio ties: %d %d", bpsRatio(1, 20000), bpsRatio(3, 40000))
	}
}

func TestMetricsEngineRunKeepsIdentitiesAndFrame(t *testing.T) {
	sc := *validScenario()
	st := mustInit(t, sc, 7)
	for i := 0; i < 30; i++ {
		step, err := Advance(sc, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		st = step.State
		m := ComputeMetrics(sc, st)
		if !reflect.DeepEqual(step.Frame.Metrics, m) {
			t.Fatalf("tick %d: frame metrics differ from ComputeMetrics", i+1)
		}
		r := m.Reconciliation
		if !r.AccrualToCashHolds || !r.BalanceIdentityHolds {
			t.Fatalf("tick %d: reconciliation %+v", i+1, r)
		}
		ob := st.Orders
		if int64(len(ob.Settled)) != ob.Shipped+ob.Cancelled {
			t.Fatalf("tick %d: settled %d != shipped %d + cancelled %d", i+1, len(ob.Settled), ob.Shipped, ob.Cancelled)
		}
		if m.OrdersDue > ob.Created || m.ServiceLevel < 0 || m.ServiceLevel > 10000 || m.FillRate > 10000 {
			t.Fatalf("tick %d: metrics out of range %+v", i+1, m)
		}
		if m.NetProfit != m.NetRevenue-m.LaborExpense-m.MarketingExpense-m.OverheadExpense-m.ServiceCreditExpense-m.CostOfGoodsSold-m.FreightExpense {
			t.Fatalf("tick %d: profit consistency", i+1)
		}
	}
	if st.Orders.Shipped == 0 {
		t.Error("run shipped nothing; example is vacuous")
	}
}

func TestEndOfHorizonStatement(t *testing.T) {
	sc := fulfilScenario()
	st := mustInit(t, sc, 1)
	st.Orders.Open = []Order{
		{ID: 1, SKU: 0, Qty: 1, UnitPrice: 1000, CreatedAt: 0, PromisedBy: 10},
		{ID: 2, SKU: 0, Qty: 1, UnitPrice: 1000, CreatedAt: 0, PromisedBy: 60},
	}
	st.Orders.Created, st.Orders.UnitsCreated, st.OrderSeq = 2, 2, 3
	st.Workforce.Regular = 0 // nothing ships: both orders stay in the backlog
	st.PurchaseOs = []PurchaseOrder{
		{ID: 2, SKU: 0, Supplier: 1, Qty: 5, UnitCost: 450, DueAt: 5000},
		{ID: 1, SKU: 0, Supplier: 0, Qty: 10, UnitCost: 400, DueAt: 4000},
	}
	next, _, _ := stepOne(t, sc, st)
	entries := len(next.Ledger.Entries)

	s := EndOfHorizonStatement(sc, next)
	if s.AtHorizon || s.Tick != 1 {
		t.Errorf("tick/horizon = %d %v", s.Tick, s.AtHorizon)
	}
	// Backlog without revenue.
	m := s.Metrics
	if m.BacklogOrders != 2 || m.BacklogUnits != 2 || m.LateBacklogOrders != 1 || m.LateBacklogUnits != 1 || m.NetRevenue != 0 ||
		m.ServiceCreditExpense != 0 || s.BacklogRevenueRecognised != 0 || s.BacklogServiceCredit != 0 {
		t.Errorf("backlog = %+v", s)
	}
	// Open POs are commitments at Supplier_Unit_Cost, sorted by ID, no ledger entry.
	wantPOs := []Commitment{
		{PurchaseOrderID: 1, SKU: 0, Supplier: 0, Qty: 10, UnitCost: 400, CommittedCost: 4000, DueAt: 4000},
		{PurchaseOrderID: 2, SKU: 0, Supplier: 1, Qty: 5, UnitCost: 450, CommittedCost: 2250, DueAt: 5000},
	}
	if !reflect.DeepEqual(s.OpenPurchaseOrders, wantPOs) || s.CommittedUnits != 15 || s.CommittedCost != 6250 {
		t.Errorf("commitments = %+v", s)
	}
	for _, e := range next.Ledger.Entries {
		if e.Cause == CauseReceipt || e.Cause == CauseFreight {
			t.Errorf("open PO produced a ledger entry: %+v", e)
		}
	}
	if len(next.Ledger.Entries) != entries {
		t.Error("EndOfHorizonStatement changed the ledger")
	}
	// Remaining stock at cost, no write-down.
	if !reflect.DeepEqual(s.Stock, []StockLine{{SKU: 0, OnHand: 50, Value: 20000}}) || s.StockUnits != 50 || s.StockValue != 20000 || !s.InventoryMatch {
		t.Errorf("stock = %+v", s)
	}
	// Wages are paid in the Tick earned; nothing is accrued.
	if s.AccruedUnpaidWages != 0 {
		t.Errorf("accrued wages = %d", s.AccruedUnpaidWages)
	}
	if !s.BalanceIdentityHolds {
		t.Error("balance identity broken")
	}

	// At the horizon the flag is set.
	sc.HorizonTicks = 1
	if !EndOfHorizonStatement(sc, next).AtHorizon {
		t.Error("AtHorizon not set at the horizon")
	}
}

func TestDisclaimerText(t *testing.T) {
	const want = "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results."
	if Disclaimer != want || strings.TrimSpace(Disclaimer) != Disclaimer {
		t.Errorf("Disclaimer = %q", Disclaimer)
	}
}
