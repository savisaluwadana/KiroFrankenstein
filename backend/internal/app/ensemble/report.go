package ensemble

import (
	"bytes"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Statements carried by every Ensemble_Report.
const (
	// LLMStatement states that no live LLM took part (Req 24.17).
	LLMStatement = "No live LLM was called. Every figure was computed by the deterministic engine and the rule-based manager."
	// AsymmetryStatement is stated when a fixed plan Arm is compared with an
	// adaptive Arm (Req 25.3).
	AsymmetryStatement = "The fixed plan arm replays a recorded plan that cannot adapt to each seed, while the adaptive policy arm decides per seed. " +
		"The fixed plan cannot react to what happens under each seed; the policy can."
)

// Report is the Ensemble_Report (Req 24.12-24.14, 25.2-25.9, 31.6). It holds
// integers and strings only, and its field order is the canonical JSON order.
type Report struct {
	Disclaimer             string       `json:"disclaimer"`
	LLMStatement           string       `json:"llmStatement"`
	ScenarioHash           string       `json:"scenarioHash"`
	EngineVersion          int          `json:"engineVersion"`
	HorizonTicks           int64        `json:"horizonTicks"`
	ComparisonTick         int64        `json:"comparisonTick"`
	TieToleranceHundredths int64        `json:"tieToleranceHundredths"`
	SeedCount              int          `json:"seedCount"`
	Seeds                  []uint64     `json:"seeds"`
	Arms                   []ReportArm  `json:"arms"`
	Asymmetry              string       `json:"asymmetry"`
	Runs                   []RunRow     `json:"runs"`
	Skipped                []SkippedRow `json:"skippedCommands"`
	Stats                  []ArmStats   `json:"statistics"`
	Paired                 *Paired      `json:"paired,omitempty"`
}

// ReportArm labels one Arm (Req 25.2).
type ReportArm struct {
	Index     int    `json:"index"`
	Label     string `json:"label"`
	Name      string `json:"name"`
	StartTick int64  `json:"startTick"`
}

// RunRow is one Arm under one Seed (Req 24.12).
type RunRow struct {
	Seed              uint64 `json:"seed"`
	ArmIndex          int    `json:"armIndex"`
	Arm               string `json:"arm"`
	StateHash         string `json:"stateHash"`
	NetProfitCents    int64  `json:"netProfitCents"`
	ContributionCents int64  `json:"contributionCents"`
	NetCashFlowCents  int64  `json:"netCashFlowCents"`
	ServiceLevelBps   int64  `json:"serviceLevelBps"`
	FillRateBps       int64  `json:"fillRateBps"`
	BacklogOrders     int64  `json:"backlogOrders"`
	CancelledOrders   int64  `json:"cancelledOrders"`
	CompositeScore    int64  `json:"compositeScore"`
	FinancialBps      int64  `json:"financialComponentBps"`
	FillComponentBps  int64  `json:"fillComponentBps"`
	OnTimeBps         int64  `json:"onTimeComponentBps"`
	SkippedCount      int    `json:"skippedCommandCount"`
	Decisions         int    `json:"decisions"`
}

// SkippedRow is one Skipped_Command (Req 24.6, 24.13).
type SkippedRow struct {
	Seed     uint64 `json:"seed"`
	ArmIndex int    `json:"armIndex"`
	Arm      string `json:"arm"`
	Tick     int64  `json:"tick"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
}

// Stat holds the distribution of one metric across Seeds (Req 25.5, 25.6).
type Stat struct {
	Min    int64 `json:"min"`
	P10    int64 `json:"p10"`
	Median int64 `json:"median"`
	Mean   int64 `json:"mean"`
	P90    int64 `json:"p90"`
	Max    int64 `json:"max"`
}

// MetricStat names one metric's Stat.
type MetricStat struct {
	Metric string `json:"metric"`
	Stat
}

// ArmStats is the statistics of every metric for one Arm.
type ArmStats struct {
	ArmIndex int          `json:"armIndex"`
	Arm      string       `json:"arm"`
	Metrics  []MetricStat `json:"metrics"`
}

// DiffRow is the Composite_Score difference of one Seed, second Arm minus
// first Arm (Req 25.7).
type DiffRow struct {
	Seed        uint64 `json:"seed"`
	FirstScore  int64  `json:"firstArmScore"`
	SecondScore int64  `json:"secondArmScore"`
	Difference  int64  `json:"difference"`
}

// Paired is the paired comparison of two Arms (Req 25.7, 25.8).
type Paired struct {
	Differences    []DiffRow `json:"differences"`
	DifferenceStat Stat      `json:"differenceStatistics"`
	FirstArmWins   int       `json:"firstArmWins"`
	SecondArmWins  int       `json:"secondArmWins"`
	Ties           int       `json:"ties"`
}

// Percentile is the nearest-rank P-th percentile (0 < p <= 100) of values
// sorted ascending: the value at rank ceil(p*n/100), at least 1 (Req 25.6).
func Percentile(sorted []int64, p int64) int64 {
	n := int64(len(sorted))
	rank := (p*n + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

// Summarize computes the Stat of a non-empty slice. The mean is the integer
// sum divided by the count, rounded half to even (Req 25.6). The median is the
// 50th nearest-rank percentile.
func Summarize(values []int64) Stat {
	s := append([]int64(nil), values...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	var sum int64
	for _, v := range s {
		sum += v
	}
	return Stat{
		Min: s[0], P10: Percentile(s, 10), Median: Percentile(s, 50),
		Mean: sim.RoundHalfEven(sum, int64(len(s))), P90: Percentile(s, 90), Max: s[len(s)-1],
	}
}

// BuildReport assembles the Ensemble_Report from a Result. tolerance is the
// Tie_Tolerance in hundredths of a point. The comparison Tick is the horizon.
func BuildReport(res Result, tolerance int64) Report {
	rep := Report{
		Disclaimer: sim.Disclaimer, LLMStatement: LLMStatement,
		ScenarioHash: res.ScenarioHash, EngineVersion: res.EngineVersion,
		HorizonTicks: res.HorizonTicks, ComparisonTick: res.HorizonTicks,
		TieToleranceHundredths: tolerance, SeedCount: len(res.Seeds),
		Arms: []ReportArm{}, Runs: []RunRow{}, Skipped: []SkippedRow{}, Stats: []ArmStats{},
	}
	for _, s := range res.Seeds {
		rep.Seeds = append(rep.Seeds, uint64(s))
	}
	if rep.Seeds == nil {
		rep.Seeds = []uint64{}
	}
	for i, a := range res.Arms {
		rep.Arms = append(rep.Arms, ReportArm{Index: i, Label: a.Label(), Name: a.Name, StartTick: a.StartTick})
	}
	if len(res.Arms) == 2 && (res.Arms[0].Kind == KindFixedReplay) != (res.Arms[1].Kind == KindFixedReplay) {
		rep.Asymmetry = AsymmetryStatement
	}

	runs := append([]RunResult(nil), res.Runs...)
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].Seed != runs[j].Seed {
			return runs[i].Seed < runs[j].Seed
		}
		return runs[i].ArmIndex < runs[j].ArmIndex
	})
	for _, r := range runs {
		m := r.Metrics
		rep.Runs = append(rep.Runs, RunRow{
			Seed: uint64(r.Seed), ArmIndex: r.ArmIndex, Arm: r.Arm, StateHash: hex.EncodeToString(r.Hash[:]),
			NetProfitCents: int64(m.NetProfit), ContributionCents: int64(m.Contribution), NetCashFlowCents: int64(m.NetCashFlow),
			ServiceLevelBps: int64(m.ServiceLevel), FillRateBps: int64(m.FillRate),
			BacklogOrders: m.BacklogOrders, CancelledOrders: r.Cancelled,
			CompositeScore: r.Score.Composite, FinancialBps: int64(r.Score.Financial),
			FillComponentBps: int64(r.Score.FulfilmentRate), OnTimeBps: int64(r.Score.OnTime),
			SkippedCount: len(r.Skipped), Decisions: r.Decisions,
		})
		for _, s := range r.Skipped {
			rep.Skipped = append(rep.Skipped, SkippedRow{Seed: uint64(r.Seed), ArmIndex: r.ArmIndex, Arm: r.Arm,
				Tick: s.Tick, Kind: s.Kind, Reason: s.Reason, Message: s.Message})
		}
	}

	for i, a := range res.Arms {
		var rows []RunRow
		for _, r := range rep.Runs {
			if r.ArmIndex == i {
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			continue
		}
		col := func(f func(RunRow) int64) []int64 {
			v := make([]int64, len(rows))
			for k, r := range rows {
				v[k] = f(r)
			}
			return v
		}
		defs := []struct {
			name string
			f    func(RunRow) int64
		}{
			{"netProfitCents", func(r RunRow) int64 { return r.NetProfitCents }},
			{"contributionCents", func(r RunRow) int64 { return r.ContributionCents }},
			{"netCashFlowCents", func(r RunRow) int64 { return r.NetCashFlowCents }},
			{"serviceLevelBps", func(r RunRow) int64 { return r.ServiceLevelBps }},
			{"fillRateBps", func(r RunRow) int64 { return r.FillRateBps }},
			{"backlogOrders", func(r RunRow) int64 { return r.BacklogOrders }},
			{"cancelledOrders", func(r RunRow) int64 { return r.CancelledOrders }},
			{"compositeScore", func(r RunRow) int64 { return r.CompositeScore }},
		}
		as := ArmStats{ArmIndex: i, Arm: a.Label()}
		for _, d := range defs {
			as.Metrics = append(as.Metrics, MetricStat{Metric: d.name, Stat: Summarize(col(d.f))})
		}
		rep.Stats = append(rep.Stats, as)
	}

	if len(res.Arms) == 2 {
		first := map[uint64]RunResult{}
		second := map[uint64]RunResult{}
		for _, r := range runs {
			if r.ArmIndex == 0 {
				first[uint64(r.Seed)] = r
			} else {
				second[uint64(r.Seed)] = r
			}
		}
		p := &Paired{Differences: []DiffRow{}}
		var diffs []int64
		for _, s := range rep.Seeds {
			a, b := first[s], second[s]
			d := b.Score.Composite - a.Score.Composite
			p.Differences = append(p.Differences, DiffRow{Seed: s, FirstScore: a.Score.Composite, SecondScore: b.Score.Composite, Difference: d})
			diffs = append(diffs, d)
			switch sim.Compare(a.Score, b.Score, tolerance) {
			case sim.ScoreWin:
				p.FirstArmWins++
			case sim.ScoreLoss:
				p.SecondArmWins++
			default:
				p.Ties++
			}
		}
		if len(diffs) > 0 {
			p.DifferenceStat = Summarize(diffs)
		}
		rep.Paired = p
	}
	return rep
}

// JSON returns the canonical JSON: structs only (fixed field order), compact,
// terminated by one newline.
func (r Report) JSON() ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// CSVColumns is the fixed column order of the CSV report.
var CSVColumns = []string{
	"record", "arm_index", "arm", "seed", "state_hash",
	"net_profit_cents", "contribution_cents", "net_cash_flow_cents", "service_level_bps", "fill_rate_bps",
	"backlog_orders", "cancelled_orders", "composite_score", "financial_component_bps", "fill_component_bps",
	"on_time_component_bps", "skipped_command_count", "decisions",
	"detail_tick", "detail_kind", "detail_reason", "detail_text",
}

// CSV returns the fixed-column CSV: one header row, then "header" records
// (key in detail_kind, value in detail_text), "run" records in ascending Seed
// order, and "skipped" records. Fields are quoted only when needed.
func (r Report) CSV() ([]byte, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	n := len(CSVColumns)
	row := func(set map[string]string) error {
		rec := make([]string, n)
		for i, c := range CSVColumns {
			rec[i] = set[c]
		}
		return w.Write(rec)
	}
	i64 := func(v int64) string { return strconv.FormatInt(v, 10) }
	u64 := func(v uint64) string { return strconv.FormatUint(v, 10) }
	if err := w.Write(CSVColumns); err != nil {
		return nil, err
	}
	hdr := func(k, v string) error {
		return row(map[string]string{"record": "header", "detail_kind": k, "detail_text": v})
	}
	pairs := [][2]string{
		{"disclaimer", r.Disclaimer},
		{"llm_statement", r.LLMStatement},
		{"scenario_hash", r.ScenarioHash},
		{"engine_version", strconv.Itoa(r.EngineVersion)},
		{"horizon_ticks", i64(r.HorizonTicks)},
		{"comparison_tick", i64(r.ComparisonTick)},
		{"tie_tolerance_hundredths", i64(r.TieToleranceHundredths)},
		{"seed_count", strconv.Itoa(r.SeedCount)},
	}
	for _, a := range r.Arms {
		pairs = append(pairs, [2]string{"arm_" + strconv.Itoa(a.Index), a.Label + " (start tick " + i64(a.StartTick) + ")"})
	}
	if r.Asymmetry != "" {
		pairs = append(pairs, [2]string{"asymmetry", r.Asymmetry})
	}
	for _, p := range pairs {
		if err := hdr(p[0], p[1]); err != nil {
			return nil, err
		}
	}
	for _, x := range r.Runs {
		if err := row(map[string]string{
			"record": "run", "arm_index": strconv.Itoa(x.ArmIndex), "arm": x.Arm, "seed": u64(x.Seed), "state_hash": x.StateHash,
			"net_profit_cents": i64(x.NetProfitCents), "contribution_cents": i64(x.ContributionCents),
			"net_cash_flow_cents": i64(x.NetCashFlowCents), "service_level_bps": i64(x.ServiceLevelBps),
			"fill_rate_bps": i64(x.FillRateBps), "backlog_orders": i64(x.BacklogOrders),
			"cancelled_orders": i64(x.CancelledOrders), "composite_score": i64(x.CompositeScore),
			"financial_component_bps": i64(x.FinancialBps), "fill_component_bps": i64(x.FillComponentBps),
			"on_time_component_bps": i64(x.OnTimeBps), "skipped_command_count": strconv.Itoa(x.SkippedCount),
			"decisions": strconv.Itoa(x.Decisions),
		}); err != nil {
			return nil, err
		}
	}
	for _, s := range r.Skipped {
		if err := row(map[string]string{
			"record": "skipped", "arm_index": strconv.Itoa(s.ArmIndex), "arm": s.Arm, "seed": u64(s.Seed),
			"detail_tick": i64(s.Tick), "detail_kind": s.Kind, "detail_reason": s.Reason, "detail_text": s.Message,
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
