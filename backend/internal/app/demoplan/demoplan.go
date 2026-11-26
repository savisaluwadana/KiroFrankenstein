// Package demoplan derives the Demo_Path plan (Req 34.6, 34.7) by running the
// flagship Scenario with no Commands. It is pure computation over the sim
// package: no store, no clock, no I/O. It asserts nothing about any financial
// outcome; it only says where to fork and what to buy.
package demoplan

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// DefaultSeed is the demo Seed.
const DefaultSeed sim.Seed = 2026

// Errors returned by Derive.
var (
	ErrNoCrisis   = errors.New("demoplan: the no-command run has no crisis tick")
	ErrNoForkTick = errors.New("demoplan: no scheduled decision window can receive a backup order before the horizon")
)

// Command is one Demo_Command in the fixture's wire form.
type Command struct {
	Kind     string `json:"kind"`
	SKUIndex int64  `json:"skuIndex"`
	SKUID    string `json:"skuId"`
	Role     string `json:"role"`
	Qty      int64  `json:"qty"`
}

// Plan is the committed demo plan.
type Plan struct {
	Scenario   string    `json:"scenario"`
	Seed       uint64    `json:"seed"`
	CrisisTick int64     `json:"crisisTick"`
	ForkTick   int64     `json:"forkTick"`
	Commands   []Command `json:"commands"`
	Reasons    []string  `json:"reasons"`
}

// SimCommands converts the plan's commands to sim Commands.
func (p Plan) SimCommands() []sim.Command {
	out := make([]sim.Command, 0, len(p.Commands))
	for _, c := range p.Commands {
		role := sim.SupplierPrimary
		if c.Role == "backup" {
			role = sim.SupplierBackup
		}
		out = append(out, sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: c.SKUIndex, Role: role, Qty: c.Qty})
	}
	return out
}

// MarshalJSON-compatible fixture encoding: indented, trailing newline.
func (p Plan) Fixture() ([]byte, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// run is the no-command run: states[t] and frames[t] for t = 0..horizon.
type run struct {
	states []sim.State
	frames []sim.TickFrame
}

func runNoCommands(sc sim.Scenario, seed sim.Seed) (run, error) {
	st, err := sim.Init(sc, seed)
	if err != nil {
		return run{}, fmt.Errorf("init: %w", err)
	}
	f0, _, err := sim.InitialFrame(sc, st)
	if err != nil {
		return run{}, fmt.Errorf("initial frame: %w", err)
	}
	r := run{states: []sim.State{st}, frames: []sim.TickFrame{f0}}
	for int64(st.Tick()) < sc.HorizonTicks {
		step, err := sim.Advance(sc, st, nil)
		if err != nil {
			return run{}, fmt.Errorf("advance to tick %d: %w", st.Tick()+1, err)
		}
		st = step.State
		r.states = append(r.states, st)
		r.frames = append(r.frames, step.Frame)
	}
	return r, nil
}

func hasStockBound(f sim.TickFrame) bool {
	for _, c := range f.Report.SKUs {
		if c.StockBoundOrders > 0 {
			return true
		}
	}
	return false
}

// Derive computes the plan for sc and seed.
//
// Crisis_Tick: the lowest Tick at which a Disruption is active or the
// Fulfilment_Constraint_Report lists a stock-bound order.
//
// Demo_Fork_Tick: the lowest scheduled Decision_Window Tick >= Crisis_Tick
// such that a backup-supplier order placed there is received before the
// horizon, that is forkTick + backup lead time < horizon for the SKU chosen
// below. The check uses the slowest backup lead time of the Scenario, so one
// fork Tick serves every SKU.
//
// Demo_Command (slice): one place_purchase_order to the backup supplier for
// the SKU with the earliest stock-bound report after the fork Tick (lowest SKU
// index on a tie). Quantity rule, from the fork-Tick State only:
//
//	qty = stockBoundUnits + ceil(unitsDemandedSoFar * leadTicks / forkTick)
//
// where stockBoundUnits are the units of that SKU's stock-bound orders in that
// report, unitsDemandedSoFar is the SKU's shipped units plus open order units
// at the fork Tick, and leadTicks is the backup lead time. The result is
// bounded by MaxOrderQuantity and by the cash still uncommitted at the fork
// Tick (Observation cash minus open purchase orders) divided by the backup
// unit cost. If the bounded quantity is below 1 no Command is produced.
func Derive(sc sim.Scenario, seed sim.Seed) (Plan, error) {
	r, err := runNoCommands(sc, seed)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Scenario: sc.Name, Seed: uint64(seed), Commands: []Command{}}

	crisis := int64(-1)
	for _, f := range r.frames {
		if len(f.Disruptions) > 0 || hasStockBound(f) {
			crisis = int64(f.Tick)
			break
		}
	}
	if crisis < 0 {
		return Plan{}, ErrNoCrisis
	}
	plan.CrisisTick = crisis
	plan.Reasons = append(plan.Reasons, fmt.Sprintf("crisis tick %d: first tick with an active disruption or a stock-bound order in the no-command run", crisis))

	// Slowest backup lead time over all SKUs.
	var maxLead int64
	for i := range sc.SKUs {
		sup, err := sim.LookupSupplier(&sc, int64(i), sim.SupplierBackup)
		if err != nil {
			return Plan{}, fmt.Errorf("backup supplier of sku %d: %w", i, err)
		}
		if sup.LeadTimeTicks > maxLead {
			maxLead = sup.LeadTimeTicks
		}
	}
	fork := int64(-1)
	for t := crisis; t < sc.HorizonTicks; t++ {
		if open, _ := sim.DecisionWindowOpen(sc, sim.Tick(t)); open && t+maxLead < sc.HorizonTicks {
			fork = t
			break
		}
	}
	if fork < 0 {
		return Plan{}, ErrNoForkTick
	}
	plan.ForkTick = fork
	plan.Reasons = append(plan.Reasons, fmt.Sprintf("fork tick %d: first scheduled decision window at or after the crisis tick whose backup order (lead time up to %d ticks) is received before the horizon at tick %d", fork, maxLead, sc.HorizonTicks))

	// Earliest stock-bound report after the fork tick.
	sku, boundUnits, boundTick := int64(-1), int64(0), int64(0)
	for _, f := range r.frames {
		if int64(f.Tick) <= fork {
			continue
		}
		for _, c := range f.Report.SKUs { // ascending SKU index
			if c.StockBoundOrders > 0 {
				sku, boundUnits, boundTick = c.SKU, c.StockBoundUnits, int64(f.Tick)
				break
			}
		}
		if sku >= 0 {
			break
		}
	}
	if sku < 0 {
		plan.Reasons = append(plan.Reasons, "no stock-bound order after the fork tick in the no-command run: no purchase order proposed")
		return plan, nil
	}

	st := r.states[fork]
	obs := sim.Observe(sc, st)
	var o sim.ObsSKU
	for _, s := range obs.SKUs {
		if s.SKU == sku {
			o = s
		}
	}
	var sup sim.ObsSupplier
	for _, s := range o.Suppliers {
		if s.Role == sim.SupplierBackup {
			sup = s
		}
	}
	demanded := st.Inventory[sku].Shipped
	for _, od := range obs.OpenOrders {
		if od.SKU == sku {
			demanded += od.Qty
		}
	}
	var expected int64
	if fork > 0 {
		expected = (demanded*sup.LeadTimeTicks + fork - 1) / fork
	}
	qty := boundUnits + expected
	reasons := []string{
		fmt.Sprintf("sku %s (index %d): earliest stock-bound report after the fork tick is at tick %d covering %d units", o.ID, sku, boundTick, boundUnits),
		fmt.Sprintf("quantity %d = %d stock-bound units + %d expected units (%d units demanded in %d ticks, over the %d-tick backup lead time, rounded up)", qty, boundUnits, expected, demanded, fork, sup.LeadTimeTicks),
	}
	if qty > sim.MaxOrderQuantity {
		qty = sim.MaxOrderQuantity
		reasons = append(reasons, fmt.Sprintf("bounded to the maximum order quantity %d", qty))
	}
	free := int64(obs.Cash)
	for _, po := range obs.PurchaseOrders {
		free -= int64(po.UnitCost) * po.Qty
	}
	if sup.UnitCostCents > 0 {
		if afford := free / int64(sup.UnitCostCents); afford < qty {
			qty = afford
			reasons = append(reasons, fmt.Sprintf("bounded to %d units by uncommitted cash %d cents at %d cents per unit", qty, free, sup.UnitCostCents))
		}
	}
	if qty < 1 {
		plan.Reasons = append(plan.Reasons, reasons...)
		plan.Reasons = append(plan.Reasons, "bounded quantity is below 1: no purchase order proposed")
		return plan, nil
	}
	plan.Commands = append(plan.Commands, Command{Kind: "place_purchase_order", SKUIndex: sku, SKUID: o.ID, Role: "backup", Qty: qty})
	plan.Reasons = append(plan.Reasons, reasons...)

	for _, v := range sim.Validate(sc, obs, plan.SimCommands(), sc.CommandBudget) {
		if !v.OK {
			return Plan{}, fmt.Errorf("demoplan: derived command rejected by the validator at tick %d: %s: %s", fork, v.Code, v.Message)
		}
	}
	return plan, nil
}
