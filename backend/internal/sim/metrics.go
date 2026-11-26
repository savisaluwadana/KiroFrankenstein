package sim

// Disclaimer is the text every results surface carries (Req 31.5, 31.6,
// Glossary "Disclaimer"). It must not be reworded.
const Disclaimer = "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results."

// Metrics is the output of the Metrics_Calculator at one Tick (Req 11). Every
// figure is an integer and is either a sum of ledger entries or a documented
// formula over them or over the order book (INV-10). The doc comment of each
// field names its source; the API exposes the same text.
//
// All sums run over Ledger entries from Tick 0 through Tick, which is the
// State's Tick, unless a field says otherwise.
type Metrics struct {
	Tick Tick

	// NetRevenue is the Revenue account balance: the sum of Revenue credits.
	// Revenue is recognised at shipment and never reduced (Req 37.2, 37.4).
	NetRevenue Cents
	// CostOfGoodsSold is the Cost_Of_Goods_Sold account balance.
	CostOfGoodsSold Cents
	// FreightExpense is the Freight_Expense account balance.
	FreightExpense Cents
	// VariableCost = Cost_Of_Goods_Sold + Freight_Expense.
	VariableCost Cents
	// Contribution = NetRevenue - VariableCost.
	Contribution Cents
	// LaborExpense is the Labor_Expense account balance.
	LaborExpense Cents
	// MarketingExpense is the Marketing_Expense account balance.
	MarketingExpense Cents
	// OverheadExpense is the Overhead_Expense account balance.
	OverheadExpense Cents
	// ServiceCreditExpense is the Service_Credit_Expense account balance.
	ServiceCreditExpense Cents
	// NetProfit = Contribution - (Labor + Marketing + Overhead + Service_Credit
	// expenses) = Revenue balance - the sum of all expense balances (Req 11.7).
	NetProfit Cents

	// NetCashFlow is the sum of Cash debits minus Cash credits over every
	// entry from Tick 0 through Tick, excluding the opening entry (Req 11.2).
	// Use NetCashFlowRange for another range.
	NetCashFlow Cents
	// Cash is the Cash account balance = opening cash + all Cash lines.
	Cash Cents
	// InventoryValue is the Inventory account balance, which equals the sum
	// of per-SKU Inventory_Values (INV-19).
	InventoryValue Cents

	// OrdersDue is the number of orders (open, shipped or cancelled) whose
	// promised ship-by minute is at most Tick*60 (Glossary "Orders_Due").
	OrdersDue int64
	// OrdersDueOnTime is the number of Orders_Due that shipped at or before
	// their promised ship-by minute.
	OrdersDueOnTime int64
	// UnitsDue is the number of units in Orders_Due.
	UnitsDue int64
	// UnitsDueShipped is the number of units in Orders_Due that have shipped.
	UnitsDueShipped int64
	// ServiceLevel = RoundHalfEven(OrdersDueOnTime*10000, OrdersDue) bps;
	// 10000 when OrdersDue is zero (Req 11.3, 11.4).
	ServiceLevel Bps
	// FillRate = RoundHalfEven(UnitsDueShipped*10000, UnitsDue) bps; 10000
	// when UnitsDue is zero (Req 11.4, 11.5).
	FillRate Bps

	// BacklogOrders and BacklogUnits count orders that are neither shipped nor
	// cancelled (Req 11.6).
	BacklogOrders int64
	BacklogUnits  int64
	// LateBacklogOrders and LateBacklogUnits are the part of the backlog whose
	// promised ship-by minute is before Tick*60, that is already passed.
	LateBacklogOrders int64
	LateBacklogUnits  int64

	// Reconciliation holds the identities that tie profit, cash and inventory
	// together (Req 11.8, 37.21).
	Reconciliation Reconciliation
}

// Reconciliation lists the accrual-to-cash and balance-identity figures.
type Reconciliation struct {
	// OpeningInventory is the Inventory debit of the opening entry.
	OpeningInventory Cents
	// InventoryChange = Inventory balance - OpeningInventory.
	InventoryChange Cents
	// ProfitLessCashFlow = NetProfit - NetCashFlow. It equals InventoryChange
	// (Req 11.8): COGS leaves inventory without cash, and purchases are cash
	// that becomes inventory.
	ProfitLessCashFlow Cents
	// AccrualToCashHolds reports ProfitLessCashFlow == InventoryChange.
	AccrualToCashHolds bool
	// OpeningEquity is the Opening_Equity account balance.
	OpeningEquity Cents
	// BalanceIdentityHolds reports Cash + Inventory == OpeningEquity + NetProfit
	// (Req 37.21).
	BalanceIdentityHolds bool
}

// FrameMetrics is the Metrics_Calculator output carried by a Tick_Frame.
type FrameMetrics = Metrics

// bpsRatio returns RoundHalfEven(num*10000, den), or 10000 when den is zero.
func bpsRatio(num, den int64) Bps {
	if den == 0 {
		return Bps(BpsScale)
	}
	return Bps(MulDiv(num, BpsScale, den))
}

// ComputeMetrics derives every metric of st at st.Tick() from the Ledger and
// the order book. It never reads floating point and does not modify st.
// Order book figures need OrderBook.Settled, which the engine maintains.
func ComputeMetrics(sc Scenario, st State) Metrics {
	_ = sc // Scenario-independent today; kept so the API need not change when a metric needs an Assumption.
	bal := st.Ledger.Balances()
	m := Metrics{Tick: st.Tick()}
	m.NetRevenue = bal[AccountRevenue]
	m.CostOfGoodsSold = bal[AccountCostOfGoodsSold]
	m.FreightExpense = bal[AccountFreightExpense]
	m.VariableCost = m.CostOfGoodsSold + m.FreightExpense
	m.Contribution = m.NetRevenue - m.VariableCost
	m.LaborExpense = bal[AccountLaborExpense]
	m.MarketingExpense = bal[AccountMarketingExpense]
	m.OverheadExpense = bal[AccountOverheadExpense]
	m.ServiceCreditExpense = bal[AccountServiceCreditExpense]
	m.NetProfit = m.Contribution - (m.LaborExpense + m.MarketingExpense + m.OverheadExpense + m.ServiceCreditExpense)
	m.Cash = bal[AccountCash]
	m.InventoryValue = bal[AccountInventory]
	m.NetCashFlow = NetCashFlowRange(st.Ledger, 0, m.Tick)

	now := st.Clock
	for _, o := range st.Orders.Open {
		m.BacklogOrders++
		m.BacklogUnits += o.Qty
		if o.PromisedBy < now {
			m.LateBacklogOrders++
			m.LateBacklogUnits += o.Qty
		}
		if o.PromisedBy <= now {
			m.OrdersDue++
			m.UnitsDue += o.Qty
		}
	}
	for _, o := range st.Orders.Settled {
		if o.PromisedBy > now {
			continue
		}
		m.OrdersDue++
		m.UnitsDue += o.Qty
		if o.ShippedAt != NotShipped {
			m.UnitsDueShipped += o.Qty
			if o.ShippedAt <= o.PromisedBy {
				m.OrdersDueOnTime++
			}
		}
	}
	m.ServiceLevel = bpsRatio(m.OrdersDueOnTime, m.OrdersDue)
	m.FillRate = bpsRatio(m.UnitsDueShipped, m.UnitsDue)

	r := &m.Reconciliation
	for _, e := range st.Ledger.Entries {
		if e.Cause != CauseOpening {
			continue
		}
		for _, l := range e.Lines {
			if l.Account == AccountInventory {
				r.OpeningInventory += l.Debit - l.Credit
			}
		}
	}
	r.InventoryChange = m.InventoryValue - r.OpeningInventory
	r.ProfitLessCashFlow = m.NetProfit - m.NetCashFlow
	r.AccrualToCashHolds = r.ProfitLessCashFlow == r.InventoryChange
	r.OpeningEquity = bal[AccountOpeningEquity]
	r.BalanceIdentityHolds = m.Cash+m.InventoryValue == r.OpeningEquity+m.NetProfit
	return m
}

// NetCashFlowRange is the sum of Cash debits minus Cash credits over entries
// whose Tick lies in [from, to], excluding the opening entry (Req 11.2). It
// reads the Cash account only.
func NetCashFlowRange(l Ledger, from, to Tick) Cents {
	var sum Cents
	for _, e := range l.Entries {
		if e.Cause == CauseOpening || e.Tick < from || e.Tick > to {
			continue
		}
		for _, ln := range e.Lines {
			if ln.Account == AccountCash {
				sum += ln.Debit - ln.Credit
			}
		}
	}
	return sum
}

// Commitment is one open purchase order reported at the end of the horizon.
// It is a commitment, not a liability: no Ledger_Entry exists for it (Req 37.18).
type Commitment struct {
	PurchaseOrderID uint64
	SKU             int64
	Supplier        uint8
	Qty             int64
	UnitCost        Cents
	CommittedCost   Cents // Qty * UnitCost, the Supplier_Unit_Cost at placement
	DueAt           SimMinute
}

// StockLine is the remaining stock of one SKU at cost (Req 37.19).
type StockLine struct {
	SKU    int64
	OnHand int64
	Value  Cents // Inventory_Value at moving-average cost
}

// EndOfHorizon is the End_Of_Horizon_Statement (Req 37.16 to 37.20). It
// reports positions as they stand; nothing is written down, liquidated,
// refunded or accrued.
type EndOfHorizon struct {
	Tick      Tick
	AtHorizon bool // Tick == Scenario.HorizonTicks
	Metrics   Metrics

	// Open orders are reported as Backlog and Late_Backlog in Metrics. Neither
	// of the two figures below is ever non-zero: open orders have no revenue,
	// cancellation or service credit recorded (Req 37.17).
	BacklogRevenueRecognised Cents
	BacklogServiceCredit     Cents

	// OpenPurchaseOrders lists commitments sorted by purchase-order ID.
	OpenPurchaseOrders []Commitment
	CommittedUnits     int64
	CommittedCost      Cents

	// Stock is the remaining inventory per SKU, ascending, at Inventory_Value.
	Stock          []StockLine
	StockUnits     int64
	StockValue     Cents // sum of per-SKU values
	InventoryMatch bool  // StockValue equals the Inventory account balance (INV-19)

	// AccruedUnpaidWages is always zero: wages are paid in the Tick earned (Req 37.20).
	AccruedUnpaidWages Cents

	// BalanceIdentityHolds repeats Metrics.Reconciliation.BalanceIdentityHolds.
	BalanceIdentityHolds bool
}

// EndOfHorizonStatement builds the statement for st. It can be called at any
// Tick; AtHorizon says whether st is at the Scenario horizon.
func EndOfHorizonStatement(sc Scenario, st State) EndOfHorizon {
	m := ComputeMetrics(sc, st)
	s := EndOfHorizon{
		Tick:                 st.Tick(),
		AtHorizon:            int64(st.Tick()) == sc.HorizonTicks,
		Metrics:              m,
		BalanceIdentityHolds: m.Reconciliation.BalanceIdentityHolds,
	}
	for _, p := range st.PurchaseOs {
		c := Commitment{
			PurchaseOrderID: p.ID, SKU: p.SKU, Supplier: p.Supplier, Qty: p.Qty,
			UnitCost: p.UnitCost, CommittedCost: Cents(p.Qty) * p.UnitCost, DueAt: p.DueAt,
		}
		s.OpenPurchaseOrders = append(s.OpenPurchaseOrders, c)
		s.CommittedUnits += p.Qty
		s.CommittedCost += c.CommittedCost
	}
	sortCommitments(s.OpenPurchaseOrders)
	for _, k := range st.Inventory {
		s.Stock = append(s.Stock, StockLine{SKU: k.SKU, OnHand: k.OnHand, Value: k.Value})
		s.StockUnits += k.OnHand
		s.StockValue += k.Value
	}
	sortStock(s.Stock)
	s.InventoryMatch = s.StockValue == m.InventoryValue
	return s
}

func sortCommitments(c []Commitment) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j-1].PurchaseOrderID > c[j].PurchaseOrderID; j-- {
			c[j-1], c[j] = c[j], c[j-1]
		}
	}
}

func sortStock(s []StockLine) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].SKU > s[j].SKU; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
