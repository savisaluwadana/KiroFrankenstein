package app_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

func newSvc(t *testing.T) (*app.BranchService, *memory.Store, app.Branch) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil)
	_, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{Name: "w", ScenarioJSON: data, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, b
}

func TestBranchServiceCreateWorld(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	if b.HeadTick != 0 || b.ManagerKind != app.ManagerHuman || b.ParentBranchID != "" {
		t.Fatalf("root branch = %+v", b)
	}
	w, err := st.GetWorld(ctx, b.WorldID)
	if err != nil || len(w.ScenarioJSON) == 0 || w.EngineVersion == "" {
		t.Fatalf("world = %+v err %v", w, err)
	}
	obs, err := svc.ObservationAt(ctx, b.ID, 0)
	if err != nil || !obs.Window.Open {
		t.Fatalf("tick 0 window open = %v err %v", obs.Window.Open, err)
	}
	if _, _, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "", ScenarioJSON: w.ScenarioJSON}); !errors.Is(err, app.ErrInvalidName) {
		t.Fatalf("empty name err = %v", err)
	}
	if _, _, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "x", ScenarioJSON: w.ScenarioJSON, Overrides: map[string]int64{"nope": 1}}); !errors.Is(err, app.ErrInvalidScenario) {
		t.Fatalf("bad override err = %v", err)
	}
}

func TestBranchServiceEndToEnd(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()

	res, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.HeadTick != 4 || res.Paused != app.PauseWindow || len(res.Frames) != 4 {
		t.Fatalf("advance = tick %d paused %q frames %d", res.HeadTick, res.Paused, len(res.Frames))
	}
	if got, _ := st.GetBranch(ctx, b.ID); got.HeadTick != 4 {
		t.Fatalf("stored head = %d", got.HeadTick)
	}

	sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{
		Branch: b.ID, WindowTick: 4, IdempotencyKey: "k1",
		Commands: []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 10}},
	})
	if err != nil || len(sr.Accepted) != 1 || len(sr.Rejected) != 0 {
		t.Fatalf("submit = %+v err %v", sr, err)
	}
	if sr.Accepted[0].EffectiveTick != 5 {
		t.Fatalf("effective tick = %d", sr.Accepted[0].EffectiveTick)
	}
	// Same key again: original result, nothing appended.
	dup, err := svc.SubmitCommands(ctx, app.SubmitRequest{
		Branch: b.ID, WindowTick: 4, IdempotencyKey: "k1",
		Commands: []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 10}},
	})
	if err != nil || !dup.Duplicate || len(dup.Accepted) != 1 {
		t.Fatalf("duplicate = %+v err %v", dup, err)
	}
	// A rejected command is returned with a reason and not stored.
	rj, err := svc.SubmitCommands(ctx, app.SubmitRequest{
		Branch: b.ID, WindowTick: 4,
		Commands: []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 99, Qty: 1}},
	})
	if err != nil || len(rj.Accepted) != 0 || len(rj.Rejected) != 1 || rj.Rejected[0].Code != sim.ReasonUnknownSKU {
		t.Fatalf("rejected = %+v err %v", rj, err)
	}
	recs, _ := st.InputsForBranch(ctx, b.ID, 100)
	if len(recs) != 1 {
		t.Fatalf("stored inputs = %d, want 1", len(recs))
	}

	res, err = svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID})
	if err != nil || res.HeadTick != 8 || res.Paused != app.PauseWindow {
		t.Fatalf("second advance = %+v err %v", res, err)
	}
	// Replaying on a cold service reproduces the head hash.
	cold := app.NewBranchService(st, nil, nil)
	h1, _ := svc.HashAt(ctx, b.ID, 8)
	h2, err := cold.HashAt(ctx, b.ID, 8)
	if err != nil || h1 != h2 {
		t.Fatalf("cold replay hash differs: %x vs %x err %v", h1, h2, err)
	}
}

func TestBranchServiceHistoryImmutable(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil { // head 4
		t.Fatal(err)
	}
	cmd := []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 1}}
	for _, wt := range []sim.Tick{0, 3} { // effective 1 and 4 are <= head 4
		_, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: wt, Commands: cmd})
		if !errors.Is(err, app.ErrHistoryImmutable) {
			t.Errorf("window %d err = %v, want history-immutable", wt, err)
		}
	}
	if err := svc.EndDecisionWindow(ctx, b.ID, 0); !errors.Is(err, app.ErrHistoryImmutable) {
		t.Errorf("end old window err = %v", err)
	}
	if recs, _ := st.InputsForBranch(ctx, b.ID, 100); len(recs) != 0 {
		t.Fatalf("inputs stored: %d", len(recs))
	}
}

func TestBranchServiceRewindReadIsPure(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 4,
		Commands: []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 5}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil { // head 8
		t.Fatal(err)
	}
	before, _ := st.InputsForBranch(ctx, b.ID, 1000)
	headBefore, _ := svc.HeadTick(ctx, b.ID)

	for tick := sim.Tick(0); tick <= 8; tick++ {
		f, err := svc.FrameAt(ctx, b.ID, tick)
		if err != nil || f.Tick != tick {
			t.Fatalf("frame %d: %+v err %v", tick, f.Tick, err)
		}
		stt, err := svc.StateAt(ctx, b.ID, tick)
		if err != nil || sim.Hash(stt) != f.Hash {
			t.Fatalf("state hash at %d differs from frame hash (err %v)", tick, err)
		}
		if h, _ := svc.HashAt(ctx, b.ID, tick); h != f.Hash {
			t.Fatalf("HashAt %d", tick)
		}
		if _, err := svc.ObservationAt(ctx, b.ID, tick); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.MetricsAt(ctx, b.ID, tick); err != nil {
			t.Fatal(err)
		}
	}
	headAfter, _ := svc.HeadTick(ctx, b.ID)
	after, _ := st.InputsForBranch(ctx, b.ID, 1000)
	if headBefore != headAfter || headAfter != 8 {
		t.Fatalf("head changed: %d -> %d", headBefore, headAfter)
	}
	if len(before) != len(after) {
		t.Fatalf("input log changed: %d -> %d", len(before), len(after))
	}
	if got, _ := st.GetBranch(ctx, b.ID); got.HeadTick != 8 {
		t.Fatalf("stored head = %d", got.HeadTick)
	}
}

func TestBranchServiceNotYetSimulated(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.FrameAt(ctx, b.ID, 1); !errors.Is(err, app.ErrNotYetSimulated) {
		t.Errorf("FrameAt err = %v", err)
	}
	if _, err := svc.StateAt(ctx, b.ID, 5); !errors.Is(err, app.ErrNotYetSimulated) {
		t.Errorf("StateAt err = %v", err)
	}
	if _, err := svc.ObservationAt(ctx, b.ID, 5); !errors.Is(err, app.ErrNotYetSimulated) {
		t.Errorf("ObservationAt err = %v", err)
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 3}); !errors.Is(err, app.ErrNotYetSimulated) {
		t.Errorf("Submit err = %v", err)
	}
}

func TestBranchServiceRunToTickAndStale(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	to := sim.Tick(10)
	res, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, RunToTick: &to})
	if err != nil || res.HeadTick != 10 || res.Paused != app.PauseRunTo {
		t.Fatalf("run-to = %+v err %v", res, err)
	}
	// Stops at the disruption onset window (tick 48) before tick 100.
	to = 100
	res, err = svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, RunToTick: &to})
	if err != nil || res.HeadTick != 48 || res.Paused != app.PauseWindow {
		t.Fatalf("run-to onset = tick %d paused %q err %v", res.HeadTick, res.Paused, err)
	}
	stale := sim.Tick(1)
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, ExpectedHeadTick: &stale}); !errors.Is(err, app.ErrStaleHead) {
		t.Errorf("stale err = %v", err)
	}
	// Run to the horizon.
	to = 120
	for i := 0; i < 5; i++ {
		res, err = svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, RunToTick: &to})
		if err != nil {
			t.Fatal(err)
		}
		if res.Paused == app.PauseHorizon {
			break
		}
	}
	if res.HeadTick != 120 || res.Paused != app.PauseHorizon {
		t.Fatalf("end = tick %d paused %q", res.HeadTick, res.Paused)
	}
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); !errors.Is(err, app.ErrHorizonReached) {
		t.Errorf("past horizon err = %v", err)
	}
}

func TestBranchServiceBudgetAndWindow(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	// Window at tick 0 is open; fill the budget of 5.
	cmd := sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 1}
	sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 0, Commands: []sim.Command{cmd, cmd, cmd, cmd, cmd, cmd}})
	if err != nil || len(sr.Accepted) != 5 || len(sr.Rejected) != 1 || sr.Rejected[0].Code != sim.ReasonBudgetExhausted {
		t.Fatalf("submit = %d accepted %d rejected err %v", len(sr.Accepted), len(sr.Rejected), err)
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 0, Commands: []sim.Command{cmd}}); !errors.Is(err, app.ErrBudgetExceeded) {
		t.Errorf("budget err = %v", err)
	}
	if err := svc.EndDecisionWindow(ctx, b.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 0}); !errors.Is(err, app.ErrWindowNotOpen) {
		t.Errorf("ended window err = %v", err)
	}
	// Tick 1 has no window.
	one := sim.Tick(1)
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, RunToTick: &one}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 1, Commands: []sim.Command{cmd}}); !errors.Is(err, app.ErrWindowNotOpen) {
		t.Errorf("no-window err = %v", err)
	}
}
