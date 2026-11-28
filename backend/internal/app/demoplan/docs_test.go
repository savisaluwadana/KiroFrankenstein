package demoplan_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

const (
	docScenarioPath = "../../../scenarios/black-friday.json"
	docFixturePath  = "../../../../web/e2e/fixtures/demo-plan.json"
	docScriptPath   = "../../../../docs/demo-script.md"
)

// planLines renders a plan's commands in the form used by the demo-plan block.
func planLines(p demoplan.Plan) []string {
	var out []string
	for _, c := range p.Commands {
		switch c.Kind {
		case "place_purchase_order":
			out = append(out, fmt.Sprintf("place_purchase_order sku=%s role=%s qty=%d", c.SKUID, c.Role, c.Qty))
		default:
			out = append(out, fmt.Sprintf("%s qty=%d", c.Kind, c.Qty))
		}
	}
	return out
}

// checkPlanBlock parses the fenced demo-plan block of doc and compares it with want.
func checkPlanBlock(doc string, want demoplan.Plan) error {
	const open = "```demo-plan\n"
	i := strings.Index(doc, open)
	if i < 0 {
		return fmt.Errorf("no demo-plan block")
	}
	rest := doc[i+len(open):]
	j := strings.Index(rest, "\n```")
	if j < 0 {
		return fmt.Errorf("unterminated demo-plan block")
	}
	lines := strings.Split(rest[:j], "\n")
	if len(lines) < 2 || lines[0] != fmt.Sprintf("forkTick: %d", want.ForkTick) {
		return fmt.Errorf("fork tick line %q, want forkTick: %d", lines[0], want.ForkTick)
	}
	if lines[1] != "commands:" {
		return fmt.Errorf("second line %q, want commands:", lines[1])
	}
	var got []string
	for _, l := range lines[2:] {
		got = append(got, strings.TrimPrefix(l, "- "))
	}
	w := planLines(want)
	if strings.Join(got, "|") != strings.Join(w, "|") {
		return fmt.Errorf("commands %q, want %q", got, w)
	}
	return nil
}

func money(c int64) string {
	neg := ""
	if c < 0 {
		neg, c = "-", -c
	}
	d := fmt.Sprint(c / 100)
	var b strings.Builder
	for i, r := range d {
		if i > 0 && (len(d)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s$%s.%02d", neg, b.String(), c%100)
}

func group(n int64) string {
	s := money(n * 100)
	return strings.TrimSuffix(strings.TrimPrefix(s, "$"), ".00")
}

func pct(bps int64) string { t := (bps + 5) / 10; return fmt.Sprintf("%d.%d%%", t/10, t%10) }

func pts(h int64) string { return fmt.Sprintf("%d.%02d", h/100, h%100) }

// advanceTo runs a branch to target, ending every decision window with no moves.
func advanceTo(t *testing.T, svc *app.BranchService, id app.BranchID, target sim.Tick) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 200; i++ {
		to := target
		res, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id, RunToTick: &to})
		if err != nil {
			t.Fatal(err)
		}
		if res.HeadTick >= target {
			return
		}
		if res.Paused == app.PauseWindow {
			if err := svc.EndDecisionWindow(ctx, id, res.HeadTick, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("branch did not reach target")
}

func TestDemoScriptMatchesPlan(t *testing.T) {
	ctx := context.Background()
	docBytes, err := os.ReadFile(docScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(docBytes)
	scData, err := os.ReadFile(docScenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := sim.ParseScenario(scData)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := demoplan.Derive(*sc, demoplan.DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	fixBytes, err := os.ReadFile(docFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fix demoplan.Plan
	if err := json.Unmarshal(fixBytes, &fix); err != nil {
		t.Fatal(err)
	}

	t.Run("plan block equals fixture and Derive", func(t *testing.T) {
		if fix.ForkTick != plan.ForkTick || strings.Join(planLines(fix), "|") != strings.Join(planLines(plan), "|") {
			t.Fatalf("fixture %v at %d differs from Derive %v at %d", planLines(fix), fix.ForkTick, planLines(plan), plan.ForkTick)
		}
		if err := checkPlanBlock(doc, fix); err != nil {
			t.Fatalf("doc vs fixture: %v", err)
		}
		if err := checkPlanBlock(doc, plan); err != nil {
			t.Fatalf("doc vs Derive: %v", err)
		}
	})

	t.Run("a disagreeing block is detected", func(t *testing.T) {
		for _, tc := range [][2]string{
			{"forkTick: 40", "forkTick: 41"},
			{"qty=339", "qty=340"},
			{"hire_temp_workers qty=3", "hire_temp_workers qty=4"},
			{"role=backup", "role=primary"},
		} {
			mutated := strings.Replace(doc, tc[0], tc[1], 1)
			if mutated == doc {
				t.Fatalf("doc has no %q to mutate", tc[0])
			}
			if err := checkPlanBlock(mutated, fix); err == nil {
				t.Errorf("block mutated %q -> %q was not detected against the fixture", tc[0], tc[1])
			}
			if err := checkPlanBlock(mutated, plan); err == nil {
				t.Errorf("block mutated %q -> %q was not detected against Derive", tc[0], tc[1])
			}
		}
	})

	t.Run("no scripted winner", func(t *testing.T) {
		low := strings.ToLower(doc)
		for _, p := range []string{"ai will win", "human will win", "will win", "guaranteed", "always wins", "will beat", "is better than the human"} {
			if strings.Contains(low, p) {
				t.Errorf("demo script contains scripted-outcome phrase %q", p)
			}
		}
	})

	// Live in-process run. Checked against the doc: profits at ticks 48 and
	// 120 of the original path and the fork, orders waiting at tick 48,
	// both state hashes at tick 120 with net_profit_cents and
	// service_level_bps, and the duel's scores, profits, on-time rates,
	// orders waiting and cancelled orders.
	data := scData
	svc := app.NewBranchService(memory.New(), nil, nil)
	_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "doc check", ScenarioJSON: data, Seed: demoplan.DefaultSeed})
	if err != nil {
		t.Fatal(err)
	}
	horizon := sim.Tick(sc.HorizonTicks)
	fork := sim.Tick(plan.ForkTick)
	advanceTo(t, svc, root.ID, horizon)
	fb, err := svc.ForkBranch(ctx, root.ID, fork, "demo fork", app.ManagerHuman)
	if err != nil {
		t.Fatal(err)
	}
	sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: fb.ID, WindowTick: fork, Commands: plan.SimCommands(), Metadata: app.Metadata{Provenance: app.ProvHuman}})
	if err != nil || len(sr.Rejected) != 0 || len(sr.Accepted) != len(plan.Commands) {
		t.Fatalf("submit plan: %+v %v", sr, err)
	}
	advanceTo(t, svc, fb.ID, horizon)

	metrics := func(id app.BranchID, tk sim.Tick) sim.Metrics {
		m, err := svc.MetricsAt(ctx, id, tk)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	must := func(what, want string) {
		t.Helper()
		if !strings.Contains(doc, want) {
			t.Errorf("docs/demo-script.md does not contain %s %q", what, want)
		}
	}
	r48, f48 := metrics(root.ID, 48), metrics(fb.ID, 48)
	r120, f120 := metrics(root.ID, horizon), metrics(fb.ID, horizon)
	must("original profit at tick 48", money(int64(r48.NetProfit)))
	must("fork profit at tick 48", money(int64(f48.NetProfit)))
	must("original orders waiting at tick 48", fmt.Sprintf("orders waiting %s", group(r48.BacklogOrders)))
	must("fork orders waiting at tick 48", fmt.Sprintf("orders waiting %s", group(f48.BacklogOrders)))
	must("original profit at tick 120", money(int64(r120.NetProfit)))
	must("fork profit at tick 120", money(int64(f120.NetProfit)))
	must("original net_profit_cents", fmt.Sprintf("net_profit_cents: %d", int64(r120.NetProfit)))
	must("fork net_profit_cents", fmt.Sprintf("net_profit_cents: %d", int64(f120.NetProfit)))
	must("original service_level_bps", fmt.Sprintf("service_level_bps: %d", int64(r120.ServiceLevel)))
	must("fork service_level_bps", fmt.Sprintf("service_level_bps: %d", int64(f120.ServiceLevel)))
	for name, id := range map[string]app.BranchID{"original": root.ID, "fork": fb.ID} {
		h, err := svc.HashAt(ctx, id, horizon)
		if err != nil {
			t.Fatal(err)
		}
		must(name+" state hash at tick 120", hex.EncodeToString(h[:]))
	}

	// Duel at tick 0: the human submits the plan at the fork window and ends
	// every other window with no moves.
	ds := app.NewDuelService(svc, nil)
	du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: 0})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if i > 400 {
			t.Fatal("duel did not reach the horizon")
		}
		s, err := ds.DuelStatus(ctx, du.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.HumanHead >= horizon && s.AIHead >= horizon {
			break
		}
		if s.WaitingOnHuman {
			if s.HumanHead == fork {
				r, err := ds.SubmitHuman(ctx, du.ID, app.SubmitRequest{WindowTick: fork, Commands: plan.SimCommands(), Metadata: app.Metadata{Provenance: app.ProvHuman}})
				if err != nil || len(r.Rejected) != 0 {
					t.Fatalf("duel submit: %+v %v", r, err)
				}
			}
			if err := ds.EndHumanWindow(ctx, du.ID, s.HumanHead, nil); err != nil {
				t.Fatal(err)
			}
		}
		to := horizon
		if s.HumanHead < fork {
			to = fork
		}
		if _, err := ds.AdvanceDuel(ctx, du.ID, &to); err != nil {
			t.Fatal(err)
		}
	}
	sc2, err := ds.DuelScore(ctx, du.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sc2.Provisional || sc2.ComparisonTick != horizon {
		t.Fatalf("duel score provisional=%v at %d", sc2.Provisional, sc2.ComparisonTick)
	}
	h, a := sc2.Human, sc2.AI
	if h.Metrics.NetProfit != f120.NetProfit {
		t.Errorf("duel human profit %d differs from the fork's %d; the doc says they are equal", h.Metrics.NetProfit, f120.NetProfit)
	}
	must("duel human points", pts(h.Score.Composite))
	must("duel AI points", pts(a.Score.Composite))
	must("duel table points row", fmt.Sprintf("| Points (of 100) | %s | %s |", pts(h.Score.Composite), pts(a.Score.Composite)))
	must("duel table profit row", fmt.Sprintf("| Profit so far | %s | %s |", money(int64(h.Metrics.NetProfit)), money(int64(a.Metrics.NetProfit))))
	must("duel table on-time row", fmt.Sprintf("| Orders delivered on time | %s | %s |", pct(int64(h.Metrics.ServiceLevel)), pct(int64(a.Metrics.ServiceLevel))))
	must("duel table waiting row", fmt.Sprintf("| Orders waiting | %s | %s |", group(h.Metrics.BacklogOrders), group(a.Metrics.BacklogOrders)))
	must("duel table cancelled row", fmt.Sprintf("| Orders cancelled | %s | %s |", group(h.Cancelled), group(a.Cancelled)))
	if sc2.Outcome != app.DuelOutcome("ai_wins") {
		t.Logf("duel outcome %q (the doc says the scoreboard showed \"AI won\")", sc2.Outcome)
	}
}
