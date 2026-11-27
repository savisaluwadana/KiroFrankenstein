// Package calibration is the reproducible harness behind docs/calibration.md
// (Req 36). It runs the flagship Scenario under a named set of Arms and Seeds
// and reports operational and financial results next to the Fulfilment
// constraint evidence (stock-bound and capacity-bound orders per Tick).
//
// The harness reports observed results. It has no notion of a winner and no
// target outcome: the only gates (Req 36.1, 36.2) live in the tests and apply
// to the no-Command run and the Rule_Based_Manager run.
//
// It is pure computation over the sim package and the rule-based policy: no
// store, no clock, no network, no LLM.
package calibration

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/rulebased"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// PlannedCommand is one Command proposed at a Decision_Window Tick (it takes
// effect at Tick+1).
type PlannedCommand struct {
	Tick int64
	Cmd  sim.Command
}

// Arm is one evaluated strategy: a fixed list of timed Commands, or the
// Rule_Based_Manager deciding at every Decision_Window.
type Arm struct {
	Name      string
	RuleBased bool
	Plan      []PlannedCommand
}

// Evidence summarises the Fulfilment_Constraint_Reports of one run.
type Evidence struct {
	StockBoundTicks      int   // Ticks whose report lists a stock-bound order
	FirstStockBoundTick  int64 // -1 when none
	StockBoundOrderTicks int64 // sum over Ticks of stock-bound orders (order-ticks)
	CapBoundTicks        int
	FirstCapBoundTick    int64 // -1 when none
	CapBoundOrderTicks   int64
	PeakCapBoundUnits    int64 // largest per-Tick capacity-bound units
	PeakCapBoundTick     int64
	MaxQueue             int64 // largest open-order queue at a Tick boundary
}

// Result is one Arm under one Seed.
type Result struct {
	Seed      sim.Seed
	Arm       string
	Hash      [32]byte
	Metrics   sim.Metrics
	Score     sim.Score
	Cancelled int64
	Skipped   int // Commands rejected by the validator (never altered)
	Commands  int // Commands accepted
	Evidence  Evidence
	// CapBoundTicks lists the Ticks with a capacity-bound order, ascending.
	CapBoundTickList []int64
	// StockBoundTickList lists the Ticks with a stock-bound order, ascending.
	StockBoundTickList []int64
}

// Run simulates sc under seed to the horizon with arm. Every Command passes
// sim.Validate against the Observation of the preceding Decision_Window; an
// invalid Command is skipped, never altered (Req 24.6 semantics).
func Run(sc sim.Scenario, seed sim.Seed, arm Arm) (Result, error) {
	st, err := sim.Init(sc, seed)
	if err != nil {
		return Result{}, fmt.Errorf("init: %w", err)
	}
	var mgr *rulebased.Manager
	if arm.RuleBased {
		mgr = rulebased.New(sc)
	}
	res := Result{Seed: seed, Arm: arm.Name, Evidence: Evidence{FirstStockBoundTick: -1, FirstCapBoundTick: -1}}
	var seq uint64
	for int64(st.Tick()) < sc.HorizonTicks {
		eff := st.Tick() + 1
		var cmds []sim.Command
		open, _ := sim.DecisionWindowOpenState(sc, st)
		if open {
			for _, p := range arm.Plan {
				if p.Tick == int64(st.Tick()) {
					cmds = append(cmds, p.Cmd)
				}
			}
			if mgr != nil {
				obs := sim.Observe(sc, st)
				p, err := mgr.Propose(context.Background(), obs, int(sc.CommandBudget))
				if err != nil {
					return Result{}, fmt.Errorf("rule-based manager: %w", err)
				}
				cmds = append(cmds, p.Commands...)
			}
		}
		var batch []sim.Input
		if len(cmds) > 0 {
			obs := sim.Observe(sc, st)
			for _, r := range sim.Validate(sc, obs, cmds, sc.CommandBudget) {
				if !r.OK {
					res.Skipped++
					continue
				}
				seq++
				batch = append(batch, sim.Input{EffectiveTick: eff, Seq: seq, Kind: sim.InputCommand, Command: r.Command})
				res.Commands++
			}
		}
		step, err := sim.Advance(sc, st, batch)
		if err != nil {
			return Result{}, fmt.Errorf("advance to tick %d: %w", int64(eff), err)
		}
		st = step.State
		res.note(step.Frame)
	}
	res.Hash = sim.Hash(st)
	res.Metrics = sim.ComputeMetrics(sc, st)
	res.Score = sim.ComputeScore(sc, res.Metrics)
	res.Cancelled = st.Orders.Cancelled
	return res, nil
}

func (r *Result) note(f sim.TickFrame) {
	ev := &r.Evidence
	var stockOrders, capOrders, capUnits int64
	for _, c := range f.Report.SKUs {
		stockOrders += c.StockBoundOrders
		capOrders += c.CapacityBoundOrders
		capUnits += c.CapacityBoundUnits
	}
	if stockOrders > 0 {
		ev.StockBoundTicks++
		ev.StockBoundOrderTicks += stockOrders
		if ev.FirstStockBoundTick < 0 {
			ev.FirstStockBoundTick = int64(f.Tick)
		}
		r.StockBoundTickList = append(r.StockBoundTickList, int64(f.Tick))
	}
	if capOrders > 0 {
		ev.CapBoundTicks++
		ev.CapBoundOrderTicks += capOrders
		if ev.FirstCapBoundTick < 0 {
			ev.FirstCapBoundTick = int64(f.Tick)
		}
		if capUnits > ev.PeakCapBoundUnits {
			ev.PeakCapBoundUnits, ev.PeakCapBoundTick = capUnits, int64(f.Tick)
		}
		r.CapBoundTickList = append(r.CapBoundTickList, int64(f.Tick))
	}
	if f.OrderQueueLen > ev.MaxQueue {
		ev.MaxQueue = f.OrderQueueLen
	}
}

// Seeds returns the Seeds 1..n.
func Seeds(n int) []sim.Seed {
	out := make([]sim.Seed, n)
	for i := range out {
		out[i] = sim.Seed(i + 1)
	}
	return out
}

// RunAll runs every Arm under every Seed. Results are ordered by Arm, then Seed.
func RunAll(sc sim.Scenario, seeds []sim.Seed, arms []Arm) ([][]Result, error) {
	out := make([][]Result, len(arms))
	for a, arm := range arms {
		for _, s := range seeds {
			r, err := Run(sc, s, arm)
			if err != nil {
				return nil, fmt.Errorf("arm %q seed %d: %w", arm.Name, uint64(s), err)
			}
			out[a] = append(out[a], r)
		}
	}
	return out, nil
}

// Summary is the across-Seed summary of one Arm.
type Summary struct {
	Arm                                    string
	Seeds                                  int
	MeanProfit, MinProfit, MaxProfit       int64
	MeanServiceBps, MeanFillBps            int64
	MeanBacklog, MeanCancelled             int64
	MeanScore, MinScore, MaxScore          int64
	SeedsWithStockBound, SeedsWithCapBound int
	MeanCapBoundTicks, MeanStockBoundTicks int64
	MeanLabor                              int64
	MeanCommands, MeanSkipped              int64
}

// Summarise reduces one Arm's results.
func Summarise(rs []Result) Summary {
	s := Summary{Seeds: len(rs)}
	if len(rs) == 0 {
		return s
	}
	s.Arm = rs[0].Arm
	s.MinProfit, s.MinScore = int64(rs[0].Metrics.NetProfit), rs[0].Score.Composite
	s.MaxProfit, s.MaxScore = s.MinProfit, s.MinScore
	var profit, svc, fill, backlog, canc, score, capT, stockT, labor, cmds, skip int64
	for _, r := range rs {
		p := int64(r.Metrics.NetProfit)
		profit += p
		if p < s.MinProfit {
			s.MinProfit = p
		}
		if p > s.MaxProfit {
			s.MaxProfit = p
		}
		svc += int64(r.Metrics.ServiceLevel)
		fill += int64(r.Metrics.FillRate)
		backlog += r.Metrics.BacklogOrders
		canc += r.Cancelled
		score += r.Score.Composite
		if r.Score.Composite < s.MinScore {
			s.MinScore = r.Score.Composite
		}
		if r.Score.Composite > s.MaxScore {
			s.MaxScore = r.Score.Composite
		}
		if r.Evidence.StockBoundTicks > 0 {
			s.SeedsWithStockBound++
		}
		if r.Evidence.CapBoundTicks > 0 {
			s.SeedsWithCapBound++
		}
		capT += int64(r.Evidence.CapBoundTicks)
		stockT += int64(r.Evidence.StockBoundTicks)
		labor += int64(r.Metrics.LaborExpense)
		cmds += int64(r.Commands)
		skip += int64(r.Skipped)
	}
	n := int64(len(rs))
	s.MeanProfit, s.MeanServiceBps, s.MeanFillBps = profit/n, svc/n, fill/n
	s.MeanBacklog, s.MeanCancelled, s.MeanScore = backlog/n, canc/n, score/n
	s.MeanCapBoundTicks, s.MeanStockBoundTicks, s.MeanLabor = capT/n, stockT/n, labor/n
	s.MeanCommands, s.MeanSkipped = cmds/n, skip/n
	return s
}

func usd(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s$%d", sign, (c+50)/100)
}

func pct(bps int64) string { return fmt.Sprintf("%d.%d%%", bps/100, (bps%100)/10) }

// WriteTable writes a Markdown table of per-Arm summaries over the Seeds.
// Every figure is a mean over the Seeds unless the column says otherwise.
func WriteTable(w io.Writer, all [][]Result) {
	fmt.Fprintln(w, "| Arm | Net profit mean (min..max) | Service level | Fill rate | Backlog orders | Cancelled | Labor | Composite mean (min..max) | Seeds stock-bound | Seeds cap-bound | Cap-bound ticks | Cmds ok/skipped |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|---|")
	for _, rs := range all {
		s := Summarise(rs)
		fmt.Fprintf(w, "| %s | %s (%s..%s) | %s | %s | %d | %d | %s | %.2f (%.2f..%.2f) | %d/%d | %d/%d | %d | %d/%d |\n",
			s.Arm, usd(s.MeanProfit), usd(s.MinProfit), usd(s.MaxProfit),
			pct(s.MeanServiceBps), pct(s.MeanFillBps), s.MeanBacklog, s.MeanCancelled, usd(s.MeanLabor),
			float64(s.MeanScore)/100, float64(s.MinScore)/100, float64(s.MaxScore)/100,
			s.SeedsWithStockBound, s.Seeds, s.SeedsWithCapBound, s.Seeds, s.MeanCapBoundTicks,
			s.MeanCommands, s.MeanSkipped)
	}
}

// WriteSeedTable writes one row per Seed for one Arm (observed results).
func WriteSeedTable(w io.Writer, rs []Result) {
	fmt.Fprintln(w, "| Seed | Net profit | Service level | Fill rate | Backlog | Cancelled | Composite | First stock-bound tick | First cap-bound tick | Cap-bound ticks |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|")
	for _, r := range rs {
		fmt.Fprintf(w, "| %d | %s | %s | %s | %d | %d | %.2f | %d | %d | %d |\n", uint64(r.Seed), usd(int64(r.Metrics.NetProfit)),
			pct(int64(r.Metrics.ServiceLevel)), pct(int64(r.Metrics.FillRate)), r.Metrics.BacklogOrders, r.Cancelled,
			float64(r.Score.Composite)/100, r.Evidence.FirstStockBoundTick, r.Evidence.FirstCapBoundTick, r.Evidence.CapBoundTicks)
	}
}

// Ranges renders ascending Ticks as "a-b, c, d-e".
func Ranges(ticks []int64) string {
	if len(ticks) == 0 {
		return "none"
	}
	t := append([]int64(nil), ticks...)
	sort.Slice(t, func(i, j int) bool { return t[i] < t[j] })
	var parts []string
	start, prev := t[0], t[0]
	flush := func() {
		if start == prev {
			parts = append(parts, fmt.Sprint(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
	}
	for _, v := range t[1:] {
		if v == prev+1 {
			prev = v
			continue
		}
		flush()
		start, prev = v, v
	}
	flush()
	return strings.Join(parts, ", ")
}
