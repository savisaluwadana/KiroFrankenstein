package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

func newDuel(t *testing.T, opts ...app.Option) (*app.BranchService, *app.DuelService, *memory.Store, app.Duel, sim.Scenario) {
	t.Helper()
	ctx := context.Background()
	data := bfJSON(t)
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil, opts...)
	_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "duel", ScenarioJSON: data, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	ds := app.NewDuelService(svc, nil)
	du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: 0})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := sim.ParseScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	return svc, ds, st, du, *sc
}

func noopAI() app.Option {
	return factoryOf(func(context.Context, app.Branch, sim.Observation, int) (app.Decision, error) {
		return app.Decision{Provenance: app.ProvAIRuleBased}, nil
	})
}

func heads(t *testing.T, svc *app.BranchService, du app.Duel) (sim.Tick, sim.Tick) {
	t.Helper()
	h, err := svc.HeadTick(context.Background(), du.HumanBranchID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.HeadTick(context.Background(), du.AIBranchID)
	if err != nil {
		t.Fatal(err)
	}
	return h, a
}

// finish plays the Duel to the horizon with the Human ending every window
// without Commands.
func finish(t *testing.T, ds *app.DuelService, du app.Duel, horizon sim.Tick) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 400; i++ {
		s, err := ds.DuelStatus(ctx, du.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.HumanHead >= horizon && s.AIHead >= horizon {
			return
		}
		if s.WaitingOnHuman {
			if err := ds.EndHumanWindow(ctx, du.ID, s.HumanHead, nil); err != nil {
				t.Fatal(err)
			}
		}
		to := horizon
		if _, err := ds.AdvanceDuel(ctx, du.ID, &to); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("duel did not reach the horizon")
}

func TestDuelCreateForksTwoSiblings(t *testing.T) {
	svc, ds, st, du, _ := newDuel(t)
	ctx := context.Background()
	h, _ := svc.GetBranch(ctx, du.HumanBranchID)
	a, _ := svc.GetBranch(ctx, du.AIBranchID)
	if h.ParentBranchID != du.ParentBranchID || a.ParentBranchID != du.ParentBranchID || h.ForkTick != a.ForkTick {
		t.Fatalf("not siblings: %+v %+v", h, a)
	}
	if h.ManagerKind != app.ManagerHuman || h.Label != "You" || a.ManagerKind != app.ManagerAIRuleBased {
		t.Fatalf("managers: %+v %+v", h, a)
	}
	if du.ComparisonPolicy != app.ComparisonPolicyMinHead {
		t.Fatalf("policy = %q", du.ComparisonPolicy)
	}
	// Persisted: a fresh service over the same store reads the Duel back.
	got, err := app.NewDuelService(svc, nil).GetDuel(ctx, du.ID)
	if err != nil || got != du {
		t.Fatalf("stored duel = %+v err %v", got, err)
	}
	if _, err := st.GetDuel(ctx, "missing"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("unknown duel: %v", err)
	}
	if _, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: du.ParentBranchID, AIKind: app.ManagerHuman}); !errors.Is(err, app.ErrInvalidAIKind) {
		t.Fatalf("human as ai kind: %v", err)
	}
}

func TestDuelNeitherBranchAdvancesWhileWindowOpen(t *testing.T) {
	svc, ds, _, du, _ := newDuel(t)
	ctx := context.Background()
	if _, err := ds.AdvanceDuel(ctx, du.ID, nil); !errors.Is(err, app.ErrDuelWindowOpen) {
		t.Fatalf("advance with open human window: %v", err)
	}
	if h, a := heads(t, svc, du); h != 0 || a != 0 {
		t.Fatalf("heads moved to %d/%d", h, a)
	}
	if s, _ := ds.DuelStatus(ctx, du.ID); !s.WaitingOnHuman || !s.WindowOpen {
		t.Fatalf("status = %+v", s)
	}
	if err := ds.EndHumanWindow(ctx, du.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	res, err := ds.AdvanceDuel(ctx, du.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, a := heads(t, svc, du)
	if h != a || h <= 0 || res.HeadTick != h || res.Paused != app.PauseWindow {
		t.Fatalf("heads %d/%d result %+v", h, a, res)
	}
	// At the next window the human has not ended it: neither moves.
	if _, err := ds.AdvanceDuel(ctx, du.ID, nil); !errors.Is(err, app.ErrDuelWindowOpen) {
		t.Fatalf("second advance: %v", err)
	}
	if h2, a2 := heads(t, svc, du); h2 != h || a2 != a {
		t.Fatalf("heads moved to %d/%d", h2, a2)
	}
	// The AI Branch decided at the window it reached, with provenance.
	recs, err := svc.DecisionRecords(ctx, du.AIBranchID)
	if err != nil || len(recs) < 2 || recs[0].Provenance != app.ProvAIRuleBased || recs[len(recs)-1].WindowTick != h {
		t.Fatalf("ai decisions = %+v err %v", recs, err)
	}
	if hum, _ := svc.DecisionRecords(ctx, du.HumanBranchID); len(hum) != 0 {
		t.Fatalf("human branch has ai records: %+v", hum)
	}
	to := h
	if err := ds.EndHumanWindow(ctx, du.ID, h, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.AdvanceDuel(ctx, du.ID, &to); !errors.Is(err, app.ErrInvalidTick) {
		t.Fatalf("run-to at head: %v", err)
	}
}

func TestDuelRunToTickLockstepAIDecidesAtEveryWindow(t *testing.T) {
	svc, ds, _, du, _ := newDuel(t)
	ctx := context.Background()
	if err := ds.EndHumanWindow(ctx, du.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	to := sim.Tick(100)
	res, err := ds.AdvanceDuel(ctx, du.ID, &to)
	if err != nil {
		t.Fatal(err)
	}
	// The scripted demand surge at tick 48 is a Disruption-onset window: the
	// Human run pauses there, and the AI branch stops at the same tick.
	h, a := heads(t, svc, du)
	if h != a || h != res.HeadTick || h >= 100 || res.Paused != app.PauseWindow {
		t.Fatalf("heads %d/%d result paused %q head %d", h, a, res.Paused, res.HeadTick)
	}
	recs, err := svc.DecisionRecords(ctx, du.AIBranchID)
	if err != nil {
		t.Fatal(err)
	}
	have := map[sim.Tick]bool{}
	for _, r := range recs {
		have[r.WindowTick] = true
	}
	for tick := sim.Tick(0); tick <= h; tick += 4 {
		if !have[tick] {
			t.Fatalf("no ai decision at window tick %d (have %v)", tick, have)
		}
	}
	// The Human's intervening windows were ended with no Commands.
	if s, _ := ds.DuelStatus(ctx, du.ID); !s.WaitingOnHuman {
		t.Fatalf("status at pause = %+v", s)
	}
}

func TestDuelInjectionHitsBothBranchesIdentically(t *testing.T) {
	svc, ds, _, du, _ := newDuel(t)
	ctx := context.Background()
	res, err := ds.InjectDisruption(ctx, du.ID, app.DuelInjectRequest{
		Kind: sim.DisruptionWorkforceAbsence, StartTick: 10, DurationTicks: 6, MagnitudeBps: 3000, Target: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Human.Input.EffectiveTick != 10 || res.AI.Input.EffectiveTick != 10 ||
		string(res.Human.Input.Payload) != string(res.AI.Input.Payload) || res.Human.Input.BranchID == res.AI.Input.BranchID {
		t.Fatalf("inject results differ: %+v", res)
	}
	if err := ds.EndHumanWindow(ctx, du.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	var seen bool
	for i := 0; i < 8; i++ {
		s, _ := ds.DuelStatus(ctx, du.ID)
		if s.WaitingOnHuman && s.HumanHead != 0 {
			if err := ds.EndHumanWindow(ctx, du.ID, s.HumanHead, nil); err != nil {
				t.Fatal(err)
			}
		}
		r, err := ds.AdvanceDuel(ctx, du.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if r.HeadTick < 10 {
			continue
		}
		if r.HeadTick != 10 || len(r.WindowReasons) != 1 || r.WindowReasons[0] != sim.WindowDisruptionOnset {
			t.Fatalf("pause at %d reasons %v", r.HeadTick, r.WindowReasons)
		}
		seen = true
		break
	}
	if !seen {
		t.Fatal("never paused at the injected onset")
	}
	hf, err := svc.FrameAt(ctx, du.HumanBranchID, 10)
	if err != nil {
		t.Fatal(err)
	}
	af, err := svc.FrameAt(ctx, du.AIBranchID, 10)
	if err != nil {
		t.Fatal(err)
	}
	strip := func(ds []sim.DisruptionFrame) []sim.DisruptionFrame {
		out := append([]sim.DisruptionFrame(nil), ds...)
		for i := range out {
			out[i].InputSeq = 0 // per-branch sequence number, not part of the Disruption
		}
		return out
	}
	hd, ad := strip(hf.Disruptions), strip(af.Disruptions)
	if len(hd) != 1 || len(ad) != 1 || hd[0] != ad[0] {
		t.Fatalf("disruptions differ: %+v vs %+v", hd, ad)
	}
	// Same windows at the same ticks for both managers.
	for tick := sim.Tick(0); tick <= 10; tick++ {
		ho, _ := svc.ObservationAt(ctx, du.HumanBranchID, tick)
		ao, _ := svc.ObservationAt(ctx, du.AIBranchID, tick)
		if ho.Window.Open != ao.Window.Open {
			t.Fatalf("window parity broken at tick %d", tick)
		}
	}
	// A start Tick at or below the head is history.
	if _, err := ds.InjectDisruption(ctx, du.ID, app.DuelInjectRequest{
		Kind: sim.DisruptionWorkforceAbsence, StartTick: 10, DurationTicks: 6, MagnitudeBps: 3000, Target: -1,
	}); !errors.Is(err, app.ErrHistoryImmutable) {
		t.Fatalf("inject at head: %v", err)
	}
}

func checkReport(t *testing.T, sc sim.Scenario, sc2 app.DuelScore) {
	t.Helper()
	if sc2.Weights != sc.ScoreWeights || sc2.ProfitFloorCents != sc.ProfitFloorCents ||
		sc2.ProfitTargetCents != sc.ProfitTargetCents || sc2.TieToleranceHundredths != sc.TieToleranceHundredths {
		t.Fatalf("scenario parameters not reported: %+v", sc2)
	}
	for _, b := range []app.DuelBranchScore{sc2.Human, sc2.AI} {
		if b.Metrics.Tick != sc2.ComparisonTick {
			t.Fatalf("metrics at tick %d, comparison tick %d", b.Metrics.Tick, sc2.ComparisonTick)
		}
		// Competitor independence: the score is a function of own metrics only.
		if want := sim.ComputeScore(sc, b.Metrics); want != b.Score {
			t.Fatalf("score %+v, own-metrics score %+v", b.Score, want)
		}
		if b.Score.Composite < 0 || b.Score.Composite > 10000 || b.Score.Financial < 0 || b.Score.Financial > 10000 {
			t.Fatalf("score out of bounds: %+v", b.Score)
		}
		sum := b.Weighted.Financial + b.Weighted.Fulfilment + b.Weighted.OnTime
		if d := sum - b.Score.Composite; d < -1 || d > 1 {
			t.Fatalf("weighted parts %+v do not add to composite %d", b.Weighted, b.Score.Composite)
		}
	}
	var want app.DuelOutcome
	switch sim.Compare(sc2.Human.Score, sc2.AI.Score, sc.TieToleranceHundredths) {
	case sim.ScoreWin:
		want = app.DuelHumanWins
	case sim.ScoreLoss:
		want = app.DuelAIWins
	default:
		want = app.DuelDraw
	}
	if sc2.Outcome != want {
		t.Fatalf("outcome %q, want %q", sc2.Outcome, want)
	}
}

func TestDuelScoreProvisionalReportsRawMetricsAndParameters(t *testing.T) {
	svc, ds, _, du, sc := newDuel(t)
	ctx := context.Background()
	if err := ds.EndHumanWindow(ctx, du.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.AdvanceDuel(ctx, du.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, err := ds.DuelScore(ctx, du.ID)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := heads(t, svc, du)
	if !got.Provisional || got.ComparisonTick != h || got.Horizon != sim.Tick(sc.HorizonTicks) {
		t.Fatalf("score = %+v", got)
	}
	checkReport(t, sc, got)
	m, _ := svc.MetricsAt(ctx, du.HumanBranchID, h)
	if got.Human.Metrics.NetProfit != m.NetProfit || got.Human.Metrics.FillRate != m.FillRate || got.Human.Metrics.BacklogOrders != m.BacklogOrders {
		t.Fatalf("raw metrics altered: %+v vs %+v", got.Human.Metrics, m)
	}
	if got.Human.Manager != app.ManagerHuman || got.AI.Manager != app.ManagerAIRuleBased || got.Human.Label != "You" {
		t.Fatalf("labels: %+v %+v", got.Human, got.AI)
	}
}

func TestDuelScoreUsesOneComparisonTickAtMinHead(t *testing.T) {
	svc, ds, _, du, sc := newDuel(t)
	ctx := context.Background()
	if err := ds.EndHumanWindow(ctx, du.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	// Advance only the AI branch outside the duel: the Comparison_Tick is the
	// lower head and the lockstep refuses to continue.
	to := sim.Tick(8)
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: du.AIBranchID, RunToTick: &to}); err != nil {
		t.Fatal(err)
	}
	got, err := ds.DuelScore(ctx, du.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ComparisonTick != 0 || got.Human.Metrics.Tick != 0 || got.AI.Metrics.Tick != 0 || !got.Provisional {
		t.Fatalf("score = %+v", got)
	}
	checkReport(t, sc, got)
	if _, err := ds.AdvanceDuel(ctx, du.ID, nil); !errors.Is(err, app.ErrDuelDiverged) {
		t.Fatalf("diverged duel advance: %v", err)
	}
}

func TestDuelFinalScoreIsNotProvisionalAndIdenticalPlansDraw(t *testing.T) {
	svc, ds, _, du, sc := newDuel(t, noopAI())
	ctx := context.Background()
	horizon := sim.Tick(sc.HorizonTicks)
	finish(t, ds, du, horizon)
	got, err := ds.DuelScore(ctx, du.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provisional || got.ComparisonTick != horizon {
		t.Fatalf("score = %+v", got)
	}
	// Both managers submitted nothing: identical plans, identical scores.
	hh, _ := svc.HashAt(ctx, du.HumanBranchID, horizon)
	ah, _ := svc.HashAt(ctx, du.AIBranchID, horizon)
	if hh != ah || got.Human.Score != got.AI.Score || got.Outcome != app.DuelDraw {
		t.Fatalf("identical plans: hashes equal %v, scores %+v / %+v, outcome %q", hh == ah, got.Human.Score, got.AI.Score, got.Outcome)
	}
	checkReport(t, sc, got)
	if _, err := ds.AdvanceDuel(ctx, du.ID, nil); !errors.Is(err, app.ErrHorizonReached) {
		t.Fatalf("advance at horizon: %v", err)
	}
}

func TestDuelWinnerFollowsComparisonWithTolerance(t *testing.T) {
	_, ds, _, du, sc := newDuel(t)
	ctx := context.Background()
	// The Human cuts the SKU 0 price to the floor in the first window; the
	// rule-based AI plays its own way. Nobody is scripted to win: the outcome
	// must follow sim.Compare on the two reported Composite_Scores.
	if _, err := ds.SubmitHuman(ctx, du.ID, app.SubmitRequest{WindowTick: 0, Commands: []sim.Command{
		{Kind: sim.CmdSetPrice, SKU: 0, AmountCents: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	finish(t, ds, du, sim.Tick(sc.HorizonTicks))
	got, err := ds.DuelScore(ctx, du.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provisional {
		t.Fatal("score still provisional at the horizon")
	}
	checkReport(t, sc, got)
	t.Logf("human %d ai %d outcome %s", got.Human.Score.Composite, got.AI.Score.Composite, got.Outcome)
	if got.Outcome != app.DuelAIWins { // a 1-cent price is a loss for the Human; the gap far exceeds the tolerance
		t.Fatalf("outcome %q", got.Outcome)
	}
	diff := got.Human.Score.Composite - got.AI.Score.Composite
	if diff < 0 {
		diff = -diff
	}
	if (diff <= sc.TieToleranceHundredths) != (got.Outcome == app.DuelDraw) {
		t.Fatalf("diff %d tolerance %d outcome %q", diff, sc.TieToleranceHundredths, got.Outcome)
	}
}

func TestDuelScoreCarriesEngineVersionWarning(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	old := app.NewBranchService(st, nil, nil, app.WithEngineVersion(1))
	_, root, err := old.CreateWorld(ctx, app.CreateWorldRequest{Name: "old", ScenarioJSON: bfJSON(t), Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	du, err := app.NewDuelService(old, nil).CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	now := app.NewDuelService(app.NewBranchService(st, nil, nil, app.WithEngineVersion(2)), nil)
	got, err := now.DuelScore(ctx, du.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Code != app.WarningEngineVersionMismatch {
		t.Fatalf("warnings = %+v", got.Warnings)
	}
	if _, err := now.AdvanceDuel(ctx, du.ID, nil); !errors.Is(err, app.ErrEngineVersionMismatch) {
		t.Fatalf("advance of mismatched duel: %v", err)
	}
}

func TestDuelLLMKindFallsBackToRuleBasedWithoutLLM(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil)
	_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "llm", ScenarioJSON: bfJSON(t), Seed: 5})
	if err != nil {
		t.Fatal(err)
	}
	ds := app.NewDuelService(svc, nil)
	du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, AIKind: app.ManagerAILLM})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := svc.DecisionRecords(ctx, du.AIBranchID)
	if err != nil || len(recs) != 1 || recs[0].Provenance != app.ProvAIRuleBasedFallback || recs[0].FallbackReason != app.FallbackLLMUnavailable {
		t.Fatalf("records = %+v err %v", recs, err)
	}
}
