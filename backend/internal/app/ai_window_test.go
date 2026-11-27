package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

func bfJSON(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// deciderFunc adapts a function to app.AIDecider.
type deciderFunc func(ctx context.Context, b app.Branch, obs sim.Observation, budget int) (app.Decision, error)

func (f deciderFunc) Decide(ctx context.Context, b app.Branch, obs sim.Observation, budget int) (app.Decision, error) {
	return f(ctx, b, obs, budget)
}

func factoryOf(f deciderFunc) app.Option {
	return app.WithAIDecider(app.ManagerAIRuleBased, func(sim.Scenario) app.AIDecider { return f })
}

func newAIWorld(t *testing.T, st *memory.Store, data []byte, opts ...app.Option) (*app.BranchService, app.Branch) {
	t.Helper()
	svc := app.NewBranchService(st, nil, nil, opts...)
	_, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{
		Name: "ai", ScenarioJSON: data, Seed: 2026, Manager: app.ManagerAIRuleBased,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, b
}

func runTo(t *testing.T, svc *app.BranchService, id app.BranchID, tick sim.Tick) app.AdvanceResult {
	t.Helper()
	res, err := svc.Advance(context.Background(), app.AdvanceRequest{Branch: id, RunToTick: &tick})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func canon(t *testing.T, o sim.Observation) string {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// recordingDecider captures the Observation of every window and delegates to
// the built-in rule-based decider.
type recordingDecider struct {
	mu   sync.Mutex
	obs  map[sim.Tick]sim.Observation
	real app.AIDecider
}

func (r *recordingDecider) Decide(ctx context.Context, b app.Branch, obs sim.Observation, budget int) (app.Decision, error) {
	r.mu.Lock()
	r.obs[obs.Tick] = obs
	r.mu.Unlock()
	return r.real.Decide(ctx, b, obs, budget)
}

func TestAIWindowObservationByteEqualToHuman(t *testing.T) {
	ctx := context.Background()
	data := bfJSON(t)
	rec := &recordingDecider{obs: map[sim.Tick]sim.Observation{}}
	st := memory.New()
	svc, b := newAIWorld(t, st, data, app.WithAIDecider(app.ManagerAIRuleBased, func(sc sim.Scenario) app.AIDecider {
		rec.real = app.RuleBasedDecider(sc)
		return rec
	}))

	// A Human world over the same scenario and seed: same Observation at tick 0.
	hs := app.NewBranchService(memory.New(), nil, nil)
	_, hb, err := hs.CreateWorld(ctx, app.CreateWorldRequest{Name: "h", ScenarioJSON: data, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	hobs, err := hs.ObservationAt(ctx, hb.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := canon(t, rec.obs[0]); got != canon(t, hobs) {
		t.Fatalf("AI tick-0 observation differs from the Human's:\n ai    %s\n human %s", got, canon(t, hobs))
	}

	// Later windows: what the AI saw equals what a Human reads for that branch and tick.
	runTo(t, svc, b.ID, 24)
	recs, err := svc.DecisionRecords(ctx, b.ID)
	if err != nil || len(recs) < 6 {
		t.Fatalf("records %d err %v", len(recs), err)
	}
	for _, r := range recs {
		want, err := svc.ObservationAt(ctx, b.ID, r.WindowTick)
		if err != nil {
			t.Fatal(err)
		}
		if got := canon(t, rec.obs[r.WindowTick]); got != canon(t, want) {
			t.Fatalf("tick %d: AI observation differs from ObservationAt", r.WindowTick)
		}
	}
}

func TestAIWindowRecordsProvenanceAndRationale(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil)
	_, b, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "ai", ScenarioJSON: bfJSON(t), Seed: 2026, Manager: app.ManagerAIRuleBased})
	if err != nil {
		t.Fatal(err)
	}
	// The window at tick 0 is decided as soon as the World exists.
	if recs, _ := svc.DecisionRecords(ctx, b.ID); len(recs) != 1 || recs[0].WindowTick != 0 {
		t.Fatalf("records after create = %+v", recs)
	}
	runTo(t, svc, b.ID, 48)
	recs, err := svc.DecisionRecords(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	var accepted int
	for i, r := range recs {
		if r.WindowTick != sim.Tick(4*i) || r.Provenance != app.ProvAIRuleBased || r.ManagerKind != app.ManagerAIRuleBased || r.FallbackReason != "" {
			t.Fatalf("record %d = %+v", i, r)
		}
		accepted += r.AcceptedCount
	}
	inputs, _ := st.InputsForBranch(ctx, b.ID, 1000)
	if accepted == 0 || len(inputs) != accepted {
		t.Fatalf("accepted %d, stored inputs %d (the rule-based AI should act within 48 ticks)", accepted, len(inputs))
	}
	byTick := map[sim.Tick]app.DecisionRecord{}
	for _, r := range recs {
		byTick[r.WindowTick] = r
	}
	for _, in := range inputs {
		r := byTick[in.EffectiveTick-1]
		if in.Metadata.Provenance != app.ProvAIRuleBased || in.Metadata.Rationale == "" || in.Metadata.Rationale != r.Rationale {
			t.Fatalf("input %+v does not carry the provenance and rationale of %+v", in, r)
		}
	}
}

func TestAIWindowZeroCommandsStillRecorded(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	svc, b := newAIWorld(t, st, bfJSON(t), factoryOf(func(context.Context, app.Branch, sim.Observation, int) (app.Decision, error) {
		return app.Decision{Rationale: "nothing to do"}, nil
	}))
	runTo(t, svc, b.ID, 12)
	recs, _ := svc.DecisionRecords(ctx, b.ID)
	if len(recs) != 4 {
		t.Fatalf("records = %d, want windows 0,4,8,12", len(recs))
	}
	for _, r := range recs {
		if r.Provenance != app.ProvAIRuleBased || r.AcceptedCount != 0 || len(r.Proposed) != 0 || r.Rationale != "nothing to do" {
			t.Fatalf("record = %+v", r)
		}
	}
	if in, _ := st.InputsForBranch(ctx, b.ID, 1000); len(in) != 0 {
		t.Fatalf("zero-command windows stored %d inputs", len(in))
	}
}

func TestAIWindowInvalidProposalsDroppedAndRecorded(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	bad := []sim.Command{
		{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: sim.MaxOrderQuantity}, // far over cash
		{Kind: sim.CmdPlacePurchaseOrder, SKU: 99, Role: sim.SupplierPrimary, Qty: 1},                   // unknown SKU
	}
	svc, b := newAIWorld(t, st, bfJSON(t), factoryOf(func(context.Context, app.Branch, sim.Observation, int) (app.Decision, error) {
		return app.Decision{Commands: bad, Rationale: "buy everything"}, nil
	}))
	runTo(t, svc, b.ID, 8)

	recs, _ := svc.DecisionRecords(ctx, b.ID)
	if len(recs) != 3 {
		t.Fatalf("records = %d", len(recs))
	}
	for _, r := range recs {
		if r.AcceptedCount != 0 || len(r.Proposed) != 2 || len(r.Rejected) != 2 ||
			r.Rejected[0].Code != string(sim.ReasonInsufficientCash) || r.Rejected[1].Code != string(sim.ReasonUnknownSKU) {
			t.Fatalf("record = %+v", r)
		}
	}
	if in, _ := st.InputsForBranch(ctx, b.ID, 1000); len(in) != 0 {
		t.Fatalf("rejected proposals reached the input log: %d", len(in))
	}
	// The engine saw nothing: same hash as a Human branch that did nothing.
	hs := app.NewBranchService(memory.New(), nil, nil)
	_, hb, _ := hs.CreateWorld(ctx, app.CreateWorldRequest{Name: "h", ScenarioJSON: bfJSON(t), Seed: 2026})
	runTo(t, hs, hb.ID, 8)
	ha, _ := svc.HashAt(ctx, b.ID, 8)
	hh, _ := hs.HashAt(ctx, hb.ID, 8)
	if ha != hh {
		t.Fatalf("hash differs from a no-command Human run: %x vs %x", ha, hh)
	}
}

func TestAIWindowReplayNeverCallsManager(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	svc, b := newAIWorld(t, st, bfJSON(t))
	res := runTo(t, svc, b.ID, 36)
	if res.HeadTick != 36 {
		t.Fatalf("head %d", res.HeadTick)
	}
	want := map[sim.Tick][32]byte{}
	for tick := sim.Tick(0); tick <= 36; tick++ {
		h, err := svc.HashAt(ctx, b.ID, tick)
		if err != nil {
			t.Fatal(err)
		}
		want[tick] = h
	}

	// Restart: a cold service whose AI manager panics when called.
	cold := app.NewBranchService(st, nil, nil, factoryOf(func(context.Context, app.Branch, sim.Observation, int) (app.Decision, error) {
		panic("manager called during replay")
	}))
	for tick := sim.Tick(0); tick <= 36; tick++ {
		h, err := cold.HashAt(ctx, b.ID, tick)
		if err != nil || h != want[tick] {
			t.Fatalf("tick %d: cold hash %x err %v, want %x", tick, h, err, want[tick])
		}
		if _, err := cold.StateAt(ctx, b.ID, tick); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cold.ExportInputs(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	// The window at the head was already decided, so a restart records no second decision.
	if recs, _ := cold.DecisionRecords(ctx, b.ID); len(recs) != 10 {
		t.Fatalf("records after restart = %d, want windows 0..36", len(recs))
	}
}

func TestAIWindowRunThroughAndWindowEnded(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	svc, b := newAIWorld(t, st, bfJSON(t))

	// Without a run-to tick the AI branch stops at the next window, already decided.
	res, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID})
	if err != nil || res.HeadTick != 4 || res.Paused != app.PauseWindow {
		t.Fatalf("advance = %+v err %v", res, err)
	}
	if recs, _ := svc.DecisionRecords(ctx, b.ID); len(recs) != 2 || recs[1].WindowTick != 4 {
		t.Fatalf("records = %+v", recs)
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 4,
		Commands: []sim.Command{{Kind: sim.CmdScheduleOvertime, Qty: 1}}}); !errors.Is(err, app.ErrWindowNotOpen) {
		t.Fatalf("human submit into an AI-decided window: %v", err)
	}

	// With a run-to tick it runs through every window, including disruption onsets.
	res = runTo(t, svc, b.ID, 72)
	if res.HeadTick != 72 || res.Paused != app.PauseRunTo {
		t.Fatalf("run-to = tick %d paused %q", res.HeadTick, res.Paused)
	}
	recs, _ := svc.DecisionRecords(ctx, b.ID)
	if len(recs) < 19 {
		t.Fatalf("records = %d, want a record for every window through tick 72", len(recs))
	}
	// To the horizon.
	res = runTo(t, svc, b.ID, 1000)
	if res.Paused != app.PauseHorizon {
		t.Fatalf("paused %q", res.Paused)
	}
}

func TestAIWindowForkDecidesAtForkTick(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil)
	_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "h", ScenarioJSON: bfJSON(t), Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, svc, root.ID, 10)
	child, err := svc.ForkBranch(ctx, root.ID, 8, "ai", app.ManagerAIRuleBased)
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := svc.DecisionRecords(ctx, child.ID)
	if len(recs) != 1 || recs[0].WindowTick != 8 || recs[0].Provenance != app.ProvAIRuleBased {
		t.Fatalf("fork records = %+v", recs)
	}
	if rr, _ := svc.DecisionRecords(ctx, root.ID); len(rr) != 0 {
		t.Fatalf("human parent has records: %+v", rr)
	}
	// An LLM-kind branch without an LLM decider uses the rule-based fallback, recorded.
	llm, err := svc.ForkBranch(ctx, root.ID, 8, "llm", app.ManagerAILLM)
	if err != nil {
		t.Fatal(err)
	}
	lr, _ := svc.DecisionRecords(ctx, llm.ID)
	if len(lr) != 1 || lr[0].Provenance != app.ProvAIRuleBasedFallback || lr[0].FallbackReason != app.FallbackLLMUnavailable {
		t.Fatalf("llm-kind records = %+v", lr)
	}
}

func TestAIWindowIgnoresDisruptionScript(t *testing.T) {
	ctx := context.Background()
	sc, err := sim.ParseScenario(bfJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	firstOnset := int64(1 << 30)
	for _, d := range sc.DisruptionScript {
		firstOnset = min(firstOnset, d.OnsetTick)
	}
	if firstOnset == 1<<30 || firstOnset < 8 {
		t.Fatalf("flagship first onset %d", firstOnset)
	}
	noScript := *sc
	noScript.DisruptionScript = nil

	run := func(s sim.Scenario) []app.DecisionRecord {
		svc := app.NewBranchService(memory.New(), nil, nil)
		_, b, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "w", ScenarioJSON: sim.PrintScenario(&s), Seed: 2026, Manager: app.ManagerAIRuleBased})
		if err != nil {
			t.Fatal(err)
		}
		runTo(t, svc, b.ID, sim.Tick(firstOnset))
		recs, _ := svc.DecisionRecords(ctx, b.ID)
		return recs
	}
	with, without := run(*sc), run(noScript)
	n := 0
	for i := range with {
		if int64(with[i].WindowTick) >= firstOnset {
			continue
		}
		n++
		a, _ := app.EncodeProposed(with[i].Proposed)
		b, _ := app.EncodeProposed(without[i].Proposed)
		if string(a) != string(b) || with[i].Rationale != without[i].Rationale {
			t.Fatalf("window %d: proposal depends on the unrevealed script", with[i].WindowTick)
		}
	}
	if n == 0 {
		t.Fatal("no windows compared")
	}
}

// The engine never reads the manager kind (INV-15, INV-18): a Human who submits
// the Commands the AI got accepted, at the same windows, reaches the same hashes.
func TestAIWindowEngineIgnoresManagerKind(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	svc, b := newAIWorld(t, st, bfJSON(t))
	runTo(t, svc, b.ID, 48)
	inputs, _ := st.InputsForBranch(ctx, b.ID, 1000)
	if len(inputs) == 0 {
		t.Fatal("the AI accepted no Commands")
	}
	byWindow := map[sim.Tick][]sim.Command{}
	for _, in := range inputs {
		c, err := sim.DecodeCommand(in.Payload)
		if err != nil {
			t.Fatal(err)
		}
		byWindow[in.EffectiveTick-1] = append(byWindow[in.EffectiveTick-1], c)
	}

	hs := app.NewBranchService(memory.New(), nil, nil)
	_, hb, err := hs.CreateWorld(ctx, app.CreateWorldRequest{Name: "h", ScenarioJSON: bfJSON(t), Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	for tick := sim.Tick(0); tick < 48; tick += 4 {
		if cmds := byWindow[tick]; len(cmds) > 0 {
			res, err := hs.SubmitCommands(ctx, app.SubmitRequest{Branch: hb.ID, WindowTick: tick, Commands: cmds})
			if err != nil || len(res.Rejected) != 0 {
				t.Fatalf("human submit at %d: %+v err %v", tick, res, err)
			}
		}
		if _, err := hs.Advance(ctx, app.AdvanceRequest{Branch: hb.ID}); err != nil {
			t.Fatal(err)
		}
	}
	for tick := sim.Tick(0); tick <= 48; tick++ {
		a, _ := svc.HashAt(ctx, b.ID, tick)
		h, _ := hs.HashAt(ctx, hb.ID, tick)
		if a != h {
			t.Fatalf("tick %d: AI hash %x, Human hash %x", tick, a, h)
		}
	}
}
