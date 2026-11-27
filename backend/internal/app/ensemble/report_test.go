package ensemble

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func TestReportPercentileNearestRank(t *testing.T) {
	seq := func(n int) []int64 {
		v := make([]int64, n)
		for i := range v {
			v[i] = int64(i + 1)
		}
		return v
	}
	tests := []struct {
		name          string
		vals          []int64
		p10, med, p90 int64
	}{
		// rank = ceil(P*n/100)
		{"n=1", seq(1), 1, 1, 1},
		// n=5: p10 rank ceil(0.5)=1, p50 rank ceil(2.5)=3, p90 rank ceil(4.5)=5
		{"n=5", seq(5), 1, 3, 5},
		// n=10: p10 rank 1, p50 rank 5, p90 rank 9
		{"n=10", seq(10), 1, 5, 9},
		// n=20: p10 rank 2, p50 rank 10, p90 rank 18
		{"n=20", seq(20), 2, 10, 18},
		// n=4: p50 rank ceil(2)=2 (lower middle)
		{"n=4", seq(4), 1, 2, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if g := Percentile(tc.vals, 10); g != tc.p10 {
				t.Errorf("p10 = %d, want %d", g, tc.p10)
			}
			if g := Percentile(tc.vals, 50); g != tc.med {
				t.Errorf("p50 = %d, want %d", g, tc.med)
			}
			if g := Percentile(tc.vals, 90); g != tc.p90 {
				t.Errorf("p90 = %d, want %d", g, tc.p90)
			}
		})
	}
}

func TestReportMeanHalfEven(t *testing.T) {
	tests := []struct {
		vals []int64
		want int64
	}{
		{[]int64{1, 2}, 2},      // 1.5 -> 2 (even)
		{[]int64{2, 3}, 2},      // 2.5 -> 2 (even)
		{[]int64{-1, -2}, -2},   // -1.5 -> -2 (even)
		{[]int64{-2, -3}, -2},   // -2.5 -> -2 (even)
		{[]int64{0, 1}, 0},      // 0.5 -> 0
		{[]int64{-1, 0}, 0},     // -0.5 -> 0
		{[]int64{-7, 3, 1}, -1}, // -3/3
		{[]int64{10}, 10},
	}
	for _, tc := range tests {
		if g := Summarize(tc.vals).Mean; g != tc.want {
			t.Errorf("mean(%v) = %d, want %d", tc.vals, g, tc.want)
		}
	}
}

// fiveSeed builds a hand-made two-arm Result over Seeds 1..5. Runs are given
// in descending Seed order within each Arm on purpose.
func fiveSeed() Result {
	a := []int64{100, 400, 200, 500, 300} // composite of arm 0 for seeds 1..5
	b := []int64{150, 395, 200, 700, 290} // composite of arm 1
	np := []int64{-250, 150, 0, 100, -100}
	res := Result{ScenarioHash: strings.Repeat("ab", 32), EngineVersion: 1, HorizonTicks: 120,
		Seeds: []sim.Seed{1, 2, 3, 4, 5},
		Arms: []Arm{
			{Kind: KindFixedReplay, Name: "human.json"},
			{Kind: KindAdaptiveEvaluation, Name: "rulebased", StartTick: 24},
		}}
	for arm, c := range [][]int64{a, b} {
		for i := 4; i >= 0; i-- {
			r := RunResult{Seed: sim.Seed(i + 1), ArmIndex: arm, Score: sim.Score{Composite: c[i]}}
			r.Arm = res.Arms[arm].Label()
			if arm == 0 {
				r.Metrics.NetProfit = sim.Cents(np[i])
			}
			res.Runs = append(res.Runs, r)
		}
	}
	return res
}

func findStat(t *testing.T, as ArmStats, metric string) Stat {
	t.Helper()
	for _, m := range as.Metrics {
		if m.Metric == metric {
			return m.Stat
		}
	}
	t.Fatalf("metric %s missing", metric)
	return Stat{}
}

func TestReportHandComputedFiveSeeds(t *testing.T) {
	rep := BuildReport(fiveSeed(), 5)

	if rep.SeedCount != 5 || rep.ComparisonTick != 120 || rep.HorizonTicks != 120 || rep.TieToleranceHundredths != 5 {
		t.Fatalf("header = %+v", rep)
	}
	if rep.Disclaimer != sim.Disclaimer || rep.LLMStatement == "" || rep.Asymmetry != AsymmetryStatement {
		t.Fatalf("statements missing: %q %q %q", rep.Disclaimer, rep.LLMStatement, rep.Asymmetry)
	}
	if rep.Arms[0].Label != LabelFixed || rep.Arms[1].Label != "adaptive policy: rule-based (start tick 24)" {
		t.Fatalf("labels = %+v", rep.Arms)
	}
	// Rows: ascending Seed, then arm.
	for i, r := range rep.Runs {
		if r.Seed != uint64(i/2+1) || r.ArmIndex != i%2 {
			t.Fatalf("row %d = seed %d arm %d", i, r.Seed, r.ArmIndex)
		}
	}

	// Arm 0 composite [100,400,200,500,300] sorted [100,200,300,400,500]:
	// min 100, p10 rank1 = 100, median rank3 = 300, mean 1500/5 = 300, p90 rank5 = 500, max 500.
	if got, want := findStat(t, rep.Stats[0], "compositeScore"), (Stat{100, 100, 300, 300, 500, 500}); got != want {
		t.Errorf("arm0 composite = %+v, want %+v", got, want)
	}
	// Arm 1 composite [150,395,200,700,290] sorted [150,200,290,395,700]:
	// min 150, p10 150, median 290, mean 1735/5 = 347, p90 700, max 700.
	if got, want := findStat(t, rep.Stats[1], "compositeScore"), (Stat{150, 150, 290, 347, 700, 700}); got != want {
		t.Errorf("arm1 composite = %+v, want %+v", got, want)
	}
	// Arm 0 net profit [-250,150,0,100,-100] sorted [-250,-100,0,100,150]:
	// min -250, p10 -250, median 0, mean -100/5 = -20, p90 150, max 150.
	if got, want := findStat(t, rep.Stats[0], "netProfitCents"), (Stat{-250, -250, 0, -20, 150, 150}); got != want {
		t.Errorf("arm0 profit = %+v, want %+v", got, want)
	}

	// Differences (arm 1 - arm 0): 50, -5, 0, 200, -10; sorted [-10,-5,0,50,200]:
	// min -10, p10 -10, median 0, mean 235/5 = 47, p90 200, max 200.
	p := rep.Paired
	wantDiff := []int64{50, -5, 0, 200, -10}
	for i, d := range p.Differences {
		if d.Difference != wantDiff[i] || d.Seed != uint64(i+1) {
			t.Errorf("diff %d = %+v", i, d)
		}
	}
	if p.DifferenceStat != (Stat{-10, -10, 0, 47, 200, 200}) {
		t.Errorf("diff stat = %+v", p.DifferenceStat)
	}
	// Tolerance 5: |50|,|200| decisive for arm 1; -10 for arm 0; -5 and 0 are ties.
	if p.FirstArmWins != 1 || p.SecondArmWins != 2 || p.Ties != 2 {
		t.Errorf("tol 5: %d/%d/%d", p.FirstArmWins, p.SecondArmWins, p.Ties)
	}
	// Tolerance 4: -5 now decisive for arm 0.
	q := BuildReport(fiveSeed(), 4).Paired
	if q.FirstArmWins != 2 || q.SecondArmWins != 2 || q.Ties != 1 {
		t.Errorf("tol 4: %d/%d/%d", q.FirstArmWins, q.SecondArmWins, q.Ties)
	}
	if q.FirstArmWins+q.SecondArmWins+q.Ties != rep.SeedCount {
		t.Error("conservation broken")
	}
}

func TestReportNoAsymmetryForSymmetricArms(t *testing.T) {
	res := fiveSeed()
	res.Arms[1].Kind = KindFixedReplay
	if BuildReport(res, 5).Asymmetry != "" {
		t.Fatal("asymmetry stated for two fixed arms")
	}
	res.Arms = res.Arms[:1]
	res.Runs = res.Runs[:5]
	rep := BuildReport(res, 5)
	if rep.Paired != nil || rep.Asymmetry != "" {
		t.Fatal("single arm must have no paired section")
	}
}

func skippedResult() Result {
	res := Result{ScenarioHash: strings.Repeat("cd", 32), EngineVersion: 1, HorizonTicks: 120,
		Seeds: []sim.Seed{7}, Arms: []Arm{{Kind: KindFixedReplay, Name: "plan.json"}}}
	r := RunResult{Seed: 7, Arm: LabelFixed,
		Score:     sim.Score{Financial: 7000, FulfilmentRate: 9500, OnTime: 9000, Composite: 8400},
		Skipped:   []Skipped{{Tick: 5, Kind: "place_purchase_order", Reason: "insufficient_cash", Message: "tick 5, too dear"}},
		Cancelled: 2}
	r.Hash = [32]byte{}
	for i := range r.Hash {
		r.Hash[i] = 1
	}
	r.Metrics.NetProfit, r.Metrics.Contribution, r.Metrics.NetCashFlow = 1000, 2000, -300
	r.Metrics.ServiceLevel, r.Metrics.FillRate, r.Metrics.BacklogOrders = 9000, 9500, 4
	res.Runs = []RunResult{r}
	return res
}

func TestReportCSVGolden(t *testing.T) {
	got, err := BuildReport(skippedResult(), 10).CSV()
	if err != nil {
		t.Fatal(err)
	}
	hdr := func(k, v string) string { return "header" + strings.Repeat(",", 19) + k + ",," + v + "\n" }
	want := strings.Join(CSVColumns, ",") + "\n" +
		hdr("disclaimer", sim.Disclaimer) +
		hdr("llm_statement", LLMStatement) +
		hdr("scenario_hash", strings.Repeat("cd", 32)) +
		hdr("engine_version", "1") +
		hdr("horizon_ticks", "120") +
		hdr("comparison_tick", "120") +
		hdr("tie_tolerance_hundredths", "10") +
		hdr("seed_count", "1") +
		hdr("arm_0", "fixed plan (non-adaptive) (start tick 0)") +
		"run,0,fixed plan (non-adaptive),7," + strings.Repeat("01", 32) + ",1000,2000,-300,9000,9500,4,2,8400,7000,9500,9000,1,0,,,,\n" +
		"skipped,0,fixed plan (non-adaptive),7,,,,,,,,,,,,,,,5,place_purchase_order,insufficient_cash,\"tick 5, too dear\"\n"
	if string(got) != want {
		t.Fatalf("csv mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReportSkippedCommandsListed(t *testing.T) {
	rep := BuildReport(skippedResult(), 10)
	if len(rep.Skipped) != 1 || rep.Runs[0].SkippedCount != 1 {
		t.Fatalf("skipped = %+v", rep)
	}
	s := rep.Skipped[0]
	if s.Seed != 7 || s.Tick != 5 || s.Kind != "place_purchase_order" || s.Reason != "insufficient_cash" {
		t.Fatalf("row = %+v", s)
	}
	js, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(js, []byte("}\n")) || bytes.Count(js, []byte("\n")) != 1 {
		t.Fatal("JSON must be compact with one trailing newline")
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(js, &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"disclaimer", "seedCount", "skippedCommands", "tieToleranceHundredths", "scenarioHash", "engineVersion", "comparisonTick", "horizonTicks"} {
		if _, ok := back[k]; !ok {
			t.Errorf("JSON lacks %q", k)
		}
	}
	if bytes.ContainsAny(js, ".") && strings.Contains(string(js), `":0.`) {
		t.Error("float in JSON")
	}
}

func TestReportConcurrencyIdenticalBytes(t *testing.T) {
	sc, h, f := load(t)
	seedList := seeds(t, 4)
	var js, cs [][]byte
	for _, conc := range []int{1, 8} {
		res, err := Run(context.Background(), Config{Scenario: sc, ScenarioHash: h, Seeds: seedList, Concurrency: conc,
			Arms: []Arm{{Kind: KindFixedReplay, Name: "human", File: &f}, {Kind: KindAdaptiveEvaluation, Name: "rulebased"}}})
		if err != nil {
			t.Fatal(err)
		}
		rep := BuildReport(res, sc.TieToleranceHundredths)
		j, err := rep.JSON()
		if err != nil {
			t.Fatal(err)
		}
		c, err := rep.CSV()
		if err != nil {
			t.Fatal(err)
		}
		js, cs = append(js, j), append(cs, c)
	}
	if !bytes.Equal(js[0], js[1]) || !bytes.Equal(cs[0], cs[1]) {
		t.Fatal("reports differ between concurrency 1 and 8")
	}
	var rep Report
	if err := json.Unmarshal(js[0], &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Paired == nil || rep.Paired.FirstArmWins+rep.Paired.SecondArmWins+rep.Paired.Ties != 4 || rep.Asymmetry == "" {
		t.Fatalf("paired section wrong: %+v", rep.Paired)
	}
}
