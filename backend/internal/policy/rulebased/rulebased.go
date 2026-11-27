package rulebased

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// ManagerName is the display label of this manager.
const ManagerName = "Rule-based AI"

// Policy parameters, in Ticks. They are fixed constants of the policy, so the
// same Observation always gives the same Proposal.
const (
	// safetyTicks of expected demand are held on top of lead-time demand.
	safetyTicks = 4
	// coverTicks of expected demand beyond the lead time are the order-up-to target.
	coverTicks = 12
)

// Priority classes used to truncate to the Command_Budget: lower runs first.
const (
	prioStockoutRisk = iota // stock will run out before an order could arrive
	prioCapacity            // overtime and temporary workers
	prioReplenish           // ordinary reorders
)

// Manager is the deterministic operations-research baseline. It reads only the
// Observation and the public Scenario Assumptions it was built with; it never
// reads the Disruption_Script, the Seed or any random stream.
//
// Demand estimate. The Observation carries no order history, so the demand per
// Tick of a SKU is the larger of (a) the Scenario baseline arrivals converted
// to orders, shared equally between SKUs and assuming one unit per order, and
// (b) the SKU's backlog spread over its promise window. An active
// demand_surge Disruption on the SKU multiplies the estimate by its magnitude.
//
//	reorder point = demand x (lead time + safetyTicks)
//	position      = on hand + inbound - backlog
//	order         = demand x (lead time + coverTicks) - position  when position <= reorder point
//
// Orders go to the primary supplier, or to the backup supplier while the
// primary is in a supplier_outage. Capacity gap: shippable backlog (per SKU,
// the smaller of backlog and on hand) beyond one Tick of fulfilment capacity
// is closed with overtime first, then temporary workers, within the Scenario
// limits.
type Manager struct {
	sc sim.Scenario
}

var _ policy.Manager = (*Manager)(nil)

// New builds a Manager from the Scenario Assumptions. The Disruption_Script
// is dropped here and never read.
func New(sc sim.Scenario) *Manager {
	sc.DisruptionScript = nil
	return &Manager{sc: sc}
}

// Name returns "Rule-based AI".
func (m *Manager) Name() string { return ManagerName }

type candidate struct {
	prio int
	key  int64
	cmd  sim.Command
	text string
}

// Propose returns at most budget Commands in priority order. It ignores ctx
// and never fails.
func (m *Manager) Propose(_ context.Context, obs sim.Observation, budget int) (policy.Proposal, error) {
	var cands []candidate
	var notes []string

	backlog := map[int64]int64{} // lookup only, never iterated
	for _, o := range obs.OpenOrders {
		backlog[o.SKU] += o.Qty
	}
	var committed int64
	for _, po := range obs.PurchaseOrders {
		committed += int64(po.UnitCost) * po.Qty
	}
	avail := int64(obs.Cash) - committed

	for _, s := range obs.SKUs {
		rate := m.demandRate(obs, s, backlog[s.SKU])
		c, note := m.reorder(s, rate, backlog[s.SKU])
		if c != nil {
			cands = append(cands, *c)
		}
		if note != "" {
			notes = append(notes, note)
		}
	}
	cands = append(cands, m.capacity(obs, backlog)...)

	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].prio != cands[b].prio {
			return cands[a].prio < cands[b].prio
		}
		if cands[a].key != cands[b].key {
			return cands[a].key < cands[b].key
		}
		return cands[a].cmd.SKU < cands[b].cmd.SKU
	})

	var cmds []sim.Command
	var lines []string
	var dropped int
	for _, c := range cands {
		if budget >= 0 && len(cmds) >= budget {
			dropped++
			continue
		}
		cmd, cost, ok := m.afford(obs, c.cmd, avail)
		if !ok {
			lines = append(lines, c.text+" Skipped: not enough cash.")
			continue
		}
		avail -= cost
		cmds = append(cmds, cmd)
		if cmd.Qty != c.cmd.Qty {
			lines = append(lines, fmt.Sprintf("%s Reduced to %d to fit cash.", c.text, cmd.Qty))
		} else {
			lines = append(lines, c.text)
		}
	}
	if dropped > 0 {
		lines = append(lines, fmt.Sprintf("%d lower-priority command(s) dropped: the budget is %d per window.", dropped, budget))
	}
	if len(cmds) == 0 && len(lines) == 0 {
		lines = append(lines, "No action: no SKU is at or below its reorder point and capacity covers the backlog.")
	}
	rationale := strings.Join(append(notes, lines...), " ")
	return policy.Proposal{Commands: cmds, Rationale: rationale}, nil
}

// demandRate estimates units per Tick for one SKU.
func (m *Manager) demandRate(obs sim.Observation, s sim.ObsSKU, backlog int64) int64 {
	d := m.sc.Demand
	n := int64(len(m.sc.SKUs))
	if n < 1 {
		n = 1
	}
	rate := ceilDiv(d.BaseArrivalsPerTick*int64(d.BaseConversionBps), sim.BpsScale*n)
	promise := int64(1)
	if s.SKU >= 0 && s.SKU < int64(len(m.sc.SKUs)) && m.sc.SKUs[s.SKU].PromiseTicks > 1 {
		promise = m.sc.SKUs[s.SKU].PromiseTicks
	}
	if b := ceilDiv(backlog, promise); b > rate {
		rate = b
	}
	var surge int64 = sim.BpsScale
	for _, dis := range obs.Disruptions {
		if dis.Kind == sim.DisruptionDemandSurge && (dis.Target == -1 || dis.Target == s.SKU) && int64(dis.MagnitudeBps) > surge {
			surge = int64(dis.MagnitudeBps)
		}
	}
	return ceilDiv(rate*surge, sim.BpsScale)
}

// reorder returns a purchase-order candidate for s, or nil, and a note.
func (m *Manager) reorder(s sim.ObsSKU, rate, backlog int64) (*candidate, string) {
	var primary, backup *sim.ObsSupplier
	for i := range s.Suppliers {
		switch s.Suppliers[i].Role {
		case sim.SupplierPrimary:
			primary = &s.Suppliers[i]
		case sim.SupplierBackup:
			backup = &s.Suppliers[i]
		}
	}
	sup, why := primary, "primary"
	if sup == nil || !sup.Available {
		if backup == nil || !backup.Available {
			if rate > 0 {
				return nil, fmt.Sprintf("%s: %d on hand, %d backlog; no supplier available, cannot reorder.", s.ID, s.OnHand, backlog)
			}
			return nil, ""
		}
		sup, why = backup, "backup (primary supplier is in an outage)"
		if primary == nil {
			why = "backup"
		}
	}
	position := s.OnHand + s.InboundUnits - backlog
	rop := rate * (sup.LeadTimeTicks + safetyTicks)
	if rate == 0 || position > rop {
		return nil, ""
	}
	target := rate * (sup.LeadTimeTicks + coverTicks)
	qty := target - position
	if qty < 1 {
		return nil, ""
	}
	if qty > sim.MaxOrderQuantity {
		qty = sim.MaxOrderQuantity
	}
	prio := prioReplenish
	if s.OnHand <= rate*sup.LeadTimeTicks {
		prio = prioStockoutRisk
	}
	text := fmt.Sprintf("%s: %d on hand, %d backlog, %d inbound; demand about %d/tick, reorder point %d, ordering %d from the %s supplier (lead %d ticks, %d cents/unit).",
		s.ID, s.OnHand, backlog, s.InboundUnits, rate, rop, qty, why, sup.LeadTimeTicks, int64(sup.UnitCostCents))
	return &candidate{
		prio: prio,
		key:  s.OnHand * 1000 / rate,
		cmd:  sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: s.SKU, Role: sup.Role, Qty: qty},
		text: text,
	}, ""
}

// capacity returns overtime and temporary-worker candidates for a capacity gap.
func (m *Manager) capacity(obs sim.Observation, backlog map[int64]int64) []candidate {
	upw := m.sc.Workforce.UnitsPerWorkerHour
	w := obs.Workforce
	if upw < 1 || w.WorkersOnShift < 1 {
		return nil
	}
	var need int64
	for _, s := range obs.SKUs {
		b := backlog[s.SKU]
		if s.OnHand < b {
			b = s.OnHand
		}
		need += b
	}
	temps := w.TempsOnShift + w.TempsOnboarding
	capAt := func(hours int64) int64 { return (w.WorkersOnShift*(1+hours) + temps) * upw }
	cur := capAt(w.OvertimeHours)
	if need <= cur {
		return nil
	}
	var out []candidate
	hours := w.OvertimeHours
	for hours < m.sc.Workforce.MaxOvertimePerWorker && capAt(hours) < need {
		hours++
	}
	text := fmt.Sprintf("Capacity gap: %d shippable units waiting vs %d units/tick of capacity.", need, cur)
	if hours > w.OvertimeHours {
		out = append(out, candidate{prio: prioCapacity, key: 0,
			cmd:  sim.Command{Kind: sim.CmdScheduleOvertime, Qty: hours},
			text: fmt.Sprintf("%s Scheduling %d overtime hours per worker (limit %d).", text, hours, m.sc.Workforce.MaxOvertimePerWorker)})
		text = "Overtime alone is not enough."
	}
	if short := need - capAt(hours); short > 0 {
		n := ceilDiv(short, upw)
		if room := m.sc.Workforce.MaxTempWorkers - temps; n > room {
			n = room
		}
		if n >= 1 {
			out = append(out, candidate{prio: prioCapacity, key: 1,
				cmd:  sim.Command{Kind: sim.CmdHireTempWorkers, Qty: n},
				text: fmt.Sprintf("%s Hiring %d temporary workers (limit %d, %d on shift or onboarding).", text, n, m.sc.Workforce.MaxTempWorkers, temps)})
		}
	}
	return out
}

// afford trims c to the available cash and returns the cash it commits.
func (m *Manager) afford(obs sim.Observation, c sim.Command, avail int64) (sim.Command, int64, bool) {
	switch c.Kind {
	case sim.CmdPlacePurchaseOrder:
		var unit int64
		for _, s := range obs.SKUs {
			if s.SKU != c.SKU {
				continue
			}
			for _, sup := range s.Suppliers {
				if sup.Role == c.Role {
					unit = int64(sup.UnitCostCents)
				}
			}
		}
		if unit > 0 {
			if max := avail / unit; c.Qty > max {
				c.Qty = max
			}
		}
		if c.Qty < 1 {
			return c, 0, false
		}
		return c, c.Qty * unit, true
	case sim.CmdScheduleOvertime:
		per := obs.Workforce.Regular * int64(m.sc.Wages.OvertimePerHourCents)
		if per > 0 {
			if max := avail / per; c.Qty > max {
				c.Qty = max
			}
		}
		if c.Qty <= obs.Workforce.OvertimeHours {
			return c, 0, false
		}
		return c, c.Qty * per, true
	case sim.CmdHireTempWorkers:
		wage := int64(m.sc.Wages.TempPerHourCents)
		if wage > 0 {
			if max := avail / wage; c.Qty > max {
				c.Qty = max
			}
		}
		if c.Qty < 1 {
			return c, 0, false
		}
		return c, c.Qty * wage, true
	}
	return c, 0, false
}

func ceilDiv(a, b int64) int64 {
	if b <= 0 || a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
