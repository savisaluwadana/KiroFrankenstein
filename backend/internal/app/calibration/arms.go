package calibration

import (
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Fixed human-style plans. They are illustrative lever isolations, not
// optimal plans and not predictions of what a person would do. Every quantity
// below is a plain number published in docs/calibration.md; none of them was
// searched for to make any manager win.

// restockQty is the per-SKU backup order quantity placed at each restock
// window, in order, 12 Ticks apart starting at the fork window: about one
// normal 12-hour cover (25 units per SKU per hour x 12 = 300), raised while the
// next 12 hours of arrivals overlap the demand surge.
var restockQty = []int64{400, 750, 750, 450, 300, 300}

// restockEvery is the spacing in Ticks of the restock windows. It is a
// multiple of the Decision_Window cadence (4).
const restockEvery = 12

func poCmd(tick, sku, qty int64, role uint8) PlannedCommand {
	return PlannedCommand{Tick: tick, Cmd: sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: sku, Role: role, Qty: qty}}
}

// Restock returns backup-supplier orders for every SKU at fork, fork+12, ...
// while the window is before the horizon.
func Restock(sc sim.Scenario, fork int64) []PlannedCommand {
	var out []PlannedCommand
	for i, q := range restockQty {
		t := fork + int64(i)*restockEvery
		if t >= sc.HorizonTicks-8 { // a backup order must arrive before the horizon
			break
		}
		for sku := range sc.SKUs {
			out = append(out, poCmd(t, int64(sku), q, sim.SupplierBackup))
		}
	}
	return out
}

// outageOnset returns the onset Tick of the scripted primary-supplier outage,
// or -1.
func outageOnset(sc sim.Scenario) int64 {
	for _, d := range sc.DisruptionScript {
		if d.Kind == "supplier_outage" {
			return d.OnsetTick
		}
	}
	return -1
}

// RestockCheap is Restock with one difference: an order goes to the cheaper
// primary supplier when it would be received before the primary outage begins,
// otherwise to the backup supplier. It shows the cost side of the inventory
// lever (primary unit cost is about 17% to 25% below the backup cost).
func RestockCheap(sc sim.Scenario, fork int64) []PlannedCommand {
	out := Restock(sc, fork)
	onset := outageOnset(sc)
	for i := range out {
		sku := out[i].Cmd.SKU
		for _, sup := range sc.Suppliers {
			if sup.SKUID == sc.SKUs[sku].ID && sup.Role == "primary" && onset >= 0 && out[i].Tick+sup.LeadTimeTicks <= onset {
				out[i].Cmd.Role = sim.SupplierPrimary
			}
		}
	}
	return out
}

// Hire returns one hire_temp_workers Command at tick.
func Hire(tick, n int64) []PlannedCommand {
	return []PlannedCommand{{Tick: tick, Cmd: sim.Command{Kind: sim.CmdHireTempWorkers, Qty: n}}}
}

// Overtime returns an overtime schedule of hours per worker from tick on, and a
// reset to 0 at endTick.
func Overtime(tick, hours, endTick int64) []PlannedCommand {
	return []PlannedCommand{
		{Tick: tick, Cmd: sim.Command{Kind: sim.CmdScheduleOvertime, Qty: hours}},
		{Tick: endTick, Cmd: sim.Command{Kind: sim.CmdScheduleOvertime, Qty: 0}},
	}
}

func cat(ps ...[]PlannedCommand) []PlannedCommand {
	var out []PlannedCommand
	for _, p := range ps {
		out = append(out, p...)
	}
	return out
}

// surgeWindow returns the onset and end Tick of the scripted demand surge, or
// -1, -1. It is used only to place the illustrative plans.
func surgeWindow(sc sim.Scenario) (onset, end int64) {
	for _, d := range sc.DisruptionScript {
		if d.Kind == "demand_surge" {
			return d.OnsetTick, d.OnsetTick + d.DurationTicks
		}
	}
	return -1, -1
}

// StandardArms returns the arms of the calibration table for the demo plan.
//
//	staffTick: where staffing is applied, one Decision_Window after the fork
//	           window, so that temporary workers are ready at the surge onset
//	           (onboarding 6 Ticks) and overtime starts with it.
func StandardArms(sc sim.Scenario, plan demoplan.Plan) []Arm {
	fork := plan.ForkTick
	onset, end := surgeWindow(sc)
	hireTick := onset - sc.Workforce.TempOnboardingTicks - 2 // 40 for onset 48, onboarding 6: ready at 46
	if hireTick < fork {
		hireTick = fork
	}
	inv := Restock(sc, fork)
	var demo []PlannedCommand
	for _, c := range plan.SimCommands() {
		demo = append(demo, PlannedCommand{Tick: fork, Cmd: c})
	}
	otPlan := Overtime(onset, 3, end)
	return []Arm{
		{Name: "no commands"},
		{Name: "rule-based AI", RuleBased: true},
		{Name: "recorded demo human plan", Plan: demo},
		{Name: "inventory-only (restock 4 SKUs)", Plan: inv},
		{Name: "staffing-only (8 temps)", Plan: Hire(hireTick, 8)},
		{Name: "staffing-only (overtime 3 h in the surge)", Plan: otPlan},
		{Name: "both (restock + 8 temps)", Plan: cat(inv, Hire(hireTick, 8))},
		{Name: "both (restock + overtime 3 h in the surge)", Plan: cat(inv, otPlan)},
		{Name: "wrong lever (30 temps + 4 h overtime, no restock)", Plan: cat(Hire(fork, 30), Overtime(fork, 4, sc.HorizonTicks))},
	}
}

// SweepArms returns the lever sweeps over a restocked warehouse: the number of
// temporary workers hired at the standard Tick, and the Tick at which 8
// temporary workers are hired.
func SweepArms(sc sim.Scenario, plan demoplan.Plan) []Arm {
	fork := plan.ForkTick
	onset, _ := surgeWindow(sc)
	hireTick := onset - sc.Workforce.TempOnboardingTicks - 2
	if hireTick < fork {
		hireTick = fork
	}
	inv := Restock(sc, fork)
	var arms []Arm
	for _, n := range []int64{0, 3, 8, 16, 24, 30} {
		name := fmt.Sprintf("restock + %d temps at tick %d", n, hireTick)
		if n == 0 {
			arms = append(arms, Arm{Name: "restock + 0 temps", Plan: inv})
			continue
		}
		arms = append(arms, Arm{Name: name, Plan: cat(inv, Hire(hireTick, n))})
	}
	for _, t := range []int64{28, 40, 48, 56, 64, 72} {
		arms = append(arms, Arm{Name: fmt.Sprintf("restock + 8 temps at tick %d", t), Plan: cat(inv, Hire(t, 8))})
	}
	// Cost side of the inventory lever: primary supplier where it arrives in time.
	for _, f := range []int64{24, 28} {
		arms = append(arms, Arm{Name: fmt.Sprintf("cheap restock from tick %d (primary when it arrives before the outage) + 8 temps at tick %d", f, hireTick), Plan: cat(RestockCheap(sc, f), Hire(hireTick, 8))})
	}
	// Restock timing: the same restock schedule shifted later.
	for _, shift := range []int64{-12, 12, 24} {
		f := fork + shift
		if f < 4 {
			continue
		}
		arms = append(arms, Arm{Name: fmt.Sprintf("restock from tick %d + 8 temps at tick %d", f, hireTick), Plan: cat(Restock(sc, f), Hire(hireTick, 8))})
	}
	return arms
}
