package sim

import "sort"

// Observation is everything a Manager may know at a Tick_Boundary (Req 18.3,
// 18.6, 18.7). It is built only from the State at that Tick and the Scenario
// Assumptions. It never contains the Seed, random stream state, future draws
// or Disruption_Script entries whose onset Tick is after the current Tick; a
// Disruption appears only once its onset Tick is reached.
//
// Sort orders of every collection:
//
//	SKUs            by SKU identifier string, then index
//	SKU.Suppliers   by role (primary, backup)
//	OpenOrders      by promised ship-by Sim_Time, then order sequence
//	PurchaseOrders  by purchase-order identifier
//	Disruptions     by onset Tick, then origin (script before injected), then input sequence
type Observation struct {
	Tick Tick
	// Cash is the Cash account balance in cents.
	Cash           Cents
	SKUs           []ObsSKU
	OpenOrders     []ObsOrder
	PurchaseOrders []ObsPurchaseOrder
	Workforce      ObsWorkforce
	Disruptions    []ObsDisruption
	Metrics        Metrics
	Window         ObsWindow
}

// ObsSKU is the view of one SKU.
type ObsSKU struct {
	SKU                 int64 // index into Scenario.SKUs, used by Commands
	ID                  string
	OnHand              int64
	InventoryValue      Cents
	ReferencePriceCents Cents
	PriceCents          Cents
	MarketingCents      Cents
	InboundPOs          int64
	InboundUnits        int64
	Suppliers           []ObsSupplier
}

// ObsSupplier is one supplier role of a SKU. Available is false while a
// supplier_outage covers it; AvailableFromTick is then the Tick at whose
// boundary the outage has ended (0 when Available).
type ObsSupplier struct {
	Role              uint8
	UnitCostCents     Cents
	LeadTimeTicks     int64
	Available         bool
	AvailableFromTick Tick
}

// ObsOrder is an open customer order.
type ObsOrder struct {
	ID         uint64
	SKU        int64
	Qty        int64
	PromisedBy SimMinute
}

// ObsPurchaseOrder is an open purchase order.
type ObsPurchaseOrder struct {
	ID        uint64
	SKU       int64
	Supplier  uint8
	Qty       int64
	UnitCost  Cents
	DueAt     SimMinute
	Expedited bool
}

// ObsWorkforce is the labour state.
type ObsWorkforce struct {
	Regular         int64
	OvertimeHours   int64
	WorkersOnShift  int64
	TempsOnShift    int64
	TempsOnboarding int64
}

// ObsDisruption is an active Disruption. Target is a SKU index or -1; for a
// supplier_outage MagnitudeBps carries the supplier role code. OnsetTick is
// at most the Observation Tick, and EndTick is the Tick at whose boundary the
// Disruption is over.
type ObsDisruption struct {
	Kind         uint8
	Origin       uint8
	InputSeq     uint64
	Target       int64
	MagnitudeBps Bps
	OnsetTick    Tick
	EndTick      Tick
}

// ObsWindow is the Decision_Window information: whether a Scenario window is
// open at this Tick (cadence or script onset; fork windows are added by the app
// layer), the per-window Command_Budget and the horizon.
type ObsWindow struct {
	Open           bool
	Reasons        []WindowReason
	CadenceTicks   int64
	CommandBudget  int64
	HorizonTicks   int64
	TicksRemaining int64
}

// Observe builds the Observation of st, which must be at a Tick_Boundary. It
// does not modify st or sc and draws from no random stream.
func Observe(sc Scenario, st State) Observation {
	tick := st.Tick()
	obs := Observation{
		Tick:    tick,
		Cash:    st.Ledger.Balance(AccountCash),
		Metrics: ComputeMetrics(sc, st),
	}

	idx := make([]int, 0, len(sc.SKUs))
	for i := range sc.SKUs {
		idx = append(idx, i)
	}
	sort.SliceStable(idx, func(a, b int) bool { return sc.SKUs[idx[a]].ID < sc.SKUs[idx[b]].ID })
	for _, i := range idx {
		spec := sc.SKUs[i]
		o := ObsSKU{SKU: int64(i), ID: spec.ID, ReferencePriceCents: spec.ReferencePriceCents}
		for _, inv := range st.Inventory {
			if inv.SKU == int64(i) {
				o.OnHand, o.InventoryValue = inv.OnHand, inv.Value
			}
		}
		if p, ok := st.priceOf(int64(i)); ok {
			o.PriceCents, o.MarketingCents = p.PriceCents, p.MarketingCents
		}
		for _, po := range st.PurchaseOs {
			if po.SKU == int64(i) {
				o.InboundPOs++
				o.InboundUnits += po.Qty
			}
		}
		for _, role := range []uint8{SupplierPrimary, SupplierBackup} {
			sup, err := LookupSupplier(&sc, int64(i), role)
			if err != nil {
				continue
			}
			os := ObsSupplier{Role: role, UnitCostCents: sup.UnitCostCents, LeadTimeTicks: sup.LeadTimeTicks, Available: true}
			if end, down := outageEnd(st.Disruptions, int64(i), role, st.Clock); down {
				os.Available, os.AvailableFromTick = false, Tick(int64(end)/MinutesPerTick)
			}
			o.Suppliers = append(o.Suppliers, os)
		}
		obs.SKUs = append(obs.SKUs, o)
	}

	for _, o := range st.Orders.Open {
		obs.OpenOrders = append(obs.OpenOrders, ObsOrder{ID: o.ID, SKU: o.SKU, Qty: o.Qty, PromisedBy: o.PromisedBy})
	}
	sort.SliceStable(obs.OpenOrders, func(a, b int) bool {
		x, y := obs.OpenOrders[a], obs.OpenOrders[b]
		if x.PromisedBy != y.PromisedBy {
			return x.PromisedBy < y.PromisedBy
		}
		return x.ID < y.ID
	})
	for _, po := range st.PurchaseOs {
		obs.PurchaseOrders = append(obs.PurchaseOrders, ObsPurchaseOrder{
			ID: po.ID, SKU: po.SKU, Supplier: po.Supplier, Qty: po.Qty,
			UnitCost: po.UnitCost, DueAt: po.DueAt, Expedited: po.Expedited != 0,
		})
	}
	sort.SliceStable(obs.PurchaseOrders, func(a, b int) bool { return obs.PurchaseOrders[a].ID < obs.PurchaseOrders[b].ID })

	obs.Workforce = ObsWorkforce{
		Regular: st.Workforce.Regular, OvertimeHours: st.Workforce.OvertimeHours,
		WorkersOnShift: regularOnShift(&st, st.Clock),
	}
	for _, c := range st.Workforce.Temps {
		if c.ReadyAt <= st.Clock {
			obs.Workforce.TempsOnShift += c.Count
		} else {
			obs.Workforce.TempsOnboarding += c.Count
		}
	}

	for _, d := range st.Disruptions {
		if d.Onset > st.Clock || d.End <= st.Clock {
			continue
		}
		obs.Disruptions = append(obs.Disruptions, ObsDisruption{
			Kind: d.Kind, Origin: d.Origin, InputSeq: d.InputSeq, Target: d.Target,
			MagnitudeBps: d.MagnitudeBps, OnsetTick: TickOf(d.Onset), EndTick: Tick(int64(d.End) / MinutesPerTick),
		})
	}
	sort.SliceStable(obs.Disruptions, func(a, b int) bool {
		x, y := obs.Disruptions[a], obs.Disruptions[b]
		if x.OnsetTick != y.OnsetTick {
			return x.OnsetTick < y.OnsetTick
		}
		if x.Origin != y.Origin {
			return x.Origin < y.Origin
		}
		return x.InputSeq < y.InputSeq
	})

	open, reasons := DecisionWindowOpen(sc, tick)
	obs.Window = ObsWindow{
		Open: open, Reasons: reasons, CadenceTicks: sc.DecisionCadenceTicks,
		CommandBudget: sc.CommandBudget, HorizonTicks: sc.HorizonTicks,
		TicksRemaining: sc.HorizonTicks - int64(tick),
	}
	return obs
}
