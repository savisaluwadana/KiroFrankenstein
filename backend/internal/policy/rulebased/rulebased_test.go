package rulebased

import (
	"context"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func testScenario() sim.Scenario {
	return sim.Scenario{
		SKUs: []sim.SKUSpec{{ID: "Chargers", PromiseTicks: 4}, {ID: "Cables", PromiseTicks: 4}},
		Workforce: sim.WorkforceSpec{
			RegularWorkers: 10, UnitsPerWorkerHour: 5, MaxOvertimePerWorker: 2, MaxTempWorkers: 6,
		},
		Wages: sim.WagesSpec{OvertimePerHourCents: 2000, TempPerHourCents: 1500},
		Demand: sim.DemandSpec{
			BaseArrivalsPerTick: 20, BaseConversionBps: 5000, // 10 orders per tick over 2 SKUs: 5 each
		},
		DisruptionScript: []sim.DisruptionSpec{{}},
	}
}

func sku(idx int64, id string, onHand int64, primaryOK bool) sim.ObsSKU {
	return sim.ObsSKU{
		SKU: idx, ID: id, OnHand: onHand,
		Suppliers: []sim.ObsSupplier{
			{Role: sim.SupplierPrimary, UnitCostCents: 1000, LeadTimeTicks: 10, Available: primaryOK},
			{Role: sim.SupplierBackup, UnitCostCents: 1500, LeadTimeTicks: 4, Available: true},
		},
	}
}

// stocked returns an Observation with ample stock, cash and capacity.
func stocked() sim.Observation {
	return sim.Observation{
		Cash:      10_000_000,
		SKUs:      []sim.ObsSKU{sku(0, "Chargers", 1000, true), sku(1, "Cables", 1000, true)},
		Workforce: sim.ObsWorkforce{Regular: 10, WorkersOnShift: 10},
	}
}

func propose(t *testing.T, obs sim.Observation, budget int) ([]sim.Command, string) {
	t.Helper()
	m := New(testScenario())
	p, err := m.Propose(context.Background(), obs, budget)
	if err != nil {
		t.Fatal(err)
	}
	return p.Commands, p.Rationale
}

func orders(n int, skuIdx int64, qty int64) []sim.ObsOrder {
	out := make([]sim.ObsOrder, n)
	for i := range out {
		out[i] = sim.ObsOrder{ID: uint64(i + 1), SKU: skuIdx, Qty: qty}
	}
	return out
}

func TestManagerName(t *testing.T) {
	if got := New(testScenario()).Name(); got != "Rule-based AI" {
		t.Fatalf("Name = %q", got)
	}
}

func TestNothingWhenStocked(t *testing.T) {
	cmds, why := propose(t, stocked(), 5)
	if len(cmds) != 0 {
		t.Fatalf("commands = %+v", cmds)
	}
	if !strings.Contains(why, "No action") {
		t.Fatalf("rationale = %q", why)
	}
}

func TestReorderBelowReorderPoint(t *testing.T) {
	obs := stocked()
	obs.SKUs[0].OnHand = 12 // rate 5, lead 10: reorder point 70
	cmds, why := propose(t, obs, 5)
	if len(cmds) != 1 {
		t.Fatalf("commands = %+v", cmds)
	}
	c := cmds[0]
	// target = 5*(10+12) = 110, position 12, order 98
	if c.Kind != sim.CmdPlacePurchaseOrder || c.SKU != 0 || c.Role != sim.SupplierPrimary || c.Qty != 98 {
		t.Fatalf("command = %+v", c)
	}
	for _, want := range []string{"Chargers: 12 on hand", "0 backlog", "primary"} {
		if !strings.Contains(why, want) {
			t.Errorf("rationale %q lacks %q", why, want)
		}
	}
}

func TestInboundStockSuppressesReorder(t *testing.T) {
	obs := stocked()
	obs.SKUs[0].OnHand = 12
	obs.SKUs[0].InboundUnits = 200
	if cmds, _ := propose(t, obs, 5); len(cmds) != 0 {
		t.Fatalf("commands = %+v", cmds)
	}
}

func TestBackupSupplierDuringOutage(t *testing.T) {
	obs := stocked()
	obs.SKUs[0] = sku(0, "Chargers", 12, false)
	cmds, why := propose(t, obs, 5)
	if len(cmds) != 1 || cmds[0].Role != sim.SupplierBackup {
		t.Fatalf("commands = %+v", cmds)
	}
	if !strings.Contains(why, "backup") || !strings.Contains(why, "outage") {
		t.Fatalf("rationale = %q", why)
	}
	// backup lead 4: reorder point 5*8=40, target 5*16=80, position 12.
	if cmds[0].Qty != 68 {
		t.Fatalf("qty = %d", cmds[0].Qty)
	}
}

func TestCashBoundsOrder(t *testing.T) {
	obs := stocked()
	obs.SKUs[0].OnHand = 12
	obs.Cash = 30_000                                                                     // 30 units at 1000
	obs.PurchaseOrders = []sim.ObsPurchaseOrder{{ID: 1, SKU: 1, Qty: 10, UnitCost: 1000}} // commits 10_000
	cmds, _ := propose(t, obs, 5)
	if len(cmds) != 1 || cmds[0].Qty != 20 {
		t.Fatalf("commands = %+v", cmds)
	}
}

func TestCapacityGapOvertimeThenTemps(t *testing.T) {
	obs := stocked()
	// capacity 10*5 = 50/tick; 200 shippable units waiting.
	obs.OpenOrders = orders(20, 0, 10)
	cmds, why := propose(t, obs, 5)
	if len(cmds) != 2 {
		t.Fatalf("commands = %+v", cmds)
	}
	if cmds[0].Kind != sim.CmdScheduleOvertime || cmds[0].Qty != 2 {
		t.Fatalf("first = %+v", cmds[0])
	}
	// overtime max: 10*3*5 = 150; short 50 -> 10 temps, capped to 6.
	if cmds[1].Kind != sim.CmdHireTempWorkers || cmds[1].Qty != 6 {
		t.Fatalf("second = %+v", cmds[1])
	}
	if !strings.Contains(why, "200 shippable") {
		t.Fatalf("rationale = %q", why)
	}
}

func TestCapacityGapOvertimeOnly(t *testing.T) {
	obs := stocked()
	obs.OpenOrders = orders(7, 0, 10) // 70 units vs 50
	cmds, _ := propose(t, obs, 5)
	if len(cmds) != 1 || cmds[0].Kind != sim.CmdScheduleOvertime || cmds[0].Qty != 1 {
		t.Fatalf("commands = %+v", cmds)
	}
}

func TestBudgetTruncationPrioritisesStockoutRisk(t *testing.T) {
	obs := stocked()
	obs.SKUs[1].OnHand = 5 // stockout risk; SKU 0 stays stocked
	obs.OpenOrders = orders(20, 0, 10)
	cmds, why := propose(t, obs, 2)
	if len(cmds) != 2 {
		t.Fatalf("commands = %+v", cmds)
	}
	if cmds[0].Kind != sim.CmdPlacePurchaseOrder || cmds[0].SKU != 1 {
		t.Fatalf("first = %+v", cmds[0])
	}
	if cmds[1].Kind != sim.CmdScheduleOvertime {
		t.Fatalf("second = %+v", cmds[1])
	}
	if !strings.Contains(why, "dropped") {
		t.Fatalf("rationale = %q", why)
	}
	if got, _ := propose(t, obs, 0); len(got) != 0 {
		t.Fatalf("budget 0 gave %+v", got)
	}
}

func TestProposalPassesValidator(t *testing.T) {
	sc := testScenario()
	obs := stocked()
	obs.SKUs[0] = sku(0, "Chargers", 12, false)
	obs.OpenOrders = orders(20, 1, 10)
	p, err := New(sc).Propose(context.Background(), obs, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range sim.Validate(sc, obs, p.Commands, 5) {
		if !r.OK {
			t.Errorf("%+v rejected: %s %s", r.Command, r.Code, r.Message)
		}
	}
}

func TestDeterministic(t *testing.T) {
	obs := stocked()
	obs.SKUs[0].OnHand = 3
	obs.SKUs[1].OnHand = 3
	a, ra := propose(t, obs, 5)
	b, rb := propose(t, obs, 5)
	if ra != rb || len(a) != len(b) {
		t.Fatal("not deterministic")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("command %d differs", i)
		}
	}
}
