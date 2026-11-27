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
	waiting := map[int64]int64{} // open orders per product, lookup only
	for _, o := range obs.OpenOrders {
		backlog[o.SKU] += o.Qty
		waiting[o.SKU]++
	}
	var committed int64
	for _, po := range obs.PurchaseOrders {
		committed += int64(po.UnitCost) * po.Qty
	}
	avail := int64(obs.Cash) - committed

	for _, s := range obs.SKUs {
		rate := m.demandRate(obs, s, backlog[s.SKU])
		c, note := m.reorder(s, rate, backlog[s.SKU], waiting[s.SKU])
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
			lines = append(lines, c.text+" I skipped that because there is not enough cash.")
			continue
		}
		avail -= cost
		cmds = append(cmds, cmd)
		if cmd.Qty != c.cmd.Qty {
			lines = append(lines, fmt.Sprintf("%s I scaled that down to %d to stay within cash.", c.text, cmd.Qty))
		} else {
			lines = append(lines, c.text)
		}
	}
	if dropped > 0 {
		if dropped == 1 {
			lines = append(lines, fmt.Sprintf("One other change will have to wait: I can make at most %d per decision window.", budget))
		} else {
			lines = append(lines, fmt.Sprintf("%d other changes will have to wait: I can make at most %d per decision window.", dropped, budget))
		}
	}
	if len(cmds) == 0 && len(lines) == 0 {
		lines = append(lines, "Nothing needed changing this hour: every product has enough stock for the next few hours and packing is keeping up.")
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
func (m *Manager) reorder(s sim.ObsSKU, rate, backlog, waiting int64) (*candidate, string) {
	name := productName(s.ID)
	var primary, backup *sim.ObsSupplier
	for i := range s.Suppliers {
		switch s.Suppliers[i].Role {
		case sim.SupplierPrimary:
			primary = &s.Suppliers[i]
		case sim.SupplierBackup:
			backup = &s.Suppliers[i]
		}
	}
	sup, backupNote := primary, ""
	if sup == nil || !sup.Available {
		if backup == nil || !backup.Available {
			if rate > 0 {
				return nil, fmt.Sprintf("%s: %s, but no supplier is available right now, so I could not reorder.", name, stockLine(s.OnHand, waiting, s.InboundUnits))
			}
			return nil, ""
		}
		sup = backup
		if primary != nil {
			backupNote = "and the normal supplier is down, "
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
	supplier := "normal"
	if sup.Role == sim.SupplierBackup {
		supplier = "backup"
	}
	text := fmt.Sprintf("%s: %s, %sso I ordered %d more from the %s supplier (about %d hours away, %s each).",
		name, stockLine(s.OnHand, waiting, s.InboundUnits), backupNote, qty, supplier, sup.LeadTimeTicks, dollars(int64(sup.UnitCostCents)))
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
	text := fmt.Sprintf("Orders are piling up faster than the team can pack (%d items ready to ship, room to pack %d an hour)", need, cur)
	if hours > w.OvertimeHours {
		out = append(out, candidate{prio: prioCapacity, key: 0,
			cmd:  sim.Command{Kind: sim.CmdScheduleOvertime, Qty: hours},
			text: fmt.Sprintf("%s, so I scheduled %d %s of overtime per worker.", text, hours, plural(hours, "hour", "hours"))})
		text = "Overtime alone is not enough"
	}
	if short := need - capAt(hours); short > 0 {
		n := ceilDiv(short, upw)
		if room := m.sc.Workforce.MaxTempWorkers - temps; n > room {
			n = room
		}
		if n >= 1 {
			out = append(out, candidate{prio: prioCapacity, key: 1,
				cmd:  sim.Command{Kind: sim.CmdHireTempWorkers, Qty: n},
				text: fmt.Sprintf("%s, so I hired %d temporary %s.", text, n, plural(n, "worker", "workers"))})
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

// productNames mirrors the web glossary's known display names; other ids are
// title-cased.
var productNames = map[string]string{
	"headphones": "Headphones",
	"charger":    "Chargers",
	"speaker":    "Speakers",
	"watchband":  "Watch bands",
}

// productName turns a product id into a display name with the same rules as
// the web glossary productName: known names first, else "_" and "-" become
// spaces and each word is capitalised. Deterministic.
func productName(id string) string {
	if n, ok := productNames[id]; ok {
		return n
	}
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(id))
	if len(words) == 0 {
		return "Product"
	}
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// dollars formats cents as "$8.00" with integer math only.
func dollars(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// stockLine reads "12 in stock and 40 orders waiting (30 already on the way)".
func stockLine(onHand, waiting, inbound int64) string {
	line := fmt.Sprintf("%d in stock", onHand)
	if waiting > 0 {
		line += fmt.Sprintf(" and %d %s waiting", waiting, plural(waiting, "order", "orders"))
	}
	if inbound > 0 {
		line += fmt.Sprintf(" (%d already on the way)", inbound)
	}
	return line
}
