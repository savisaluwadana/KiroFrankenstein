package app_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

func TestEngineVersionMismatch(t *testing.T) {
	ctx := context.Background()
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	// A World recorded under engine version 1, read by the running engine.
	old := app.NewBranchService(st, nil, nil, app.WithEngineVersion(1))
	w, b, err := old.CreateWorld(ctx, app.CreateWorldRequest{Name: "old", ScenarioJSON: data, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if w.EngineVersion != "1" {
		t.Fatalf("stored engine version = %q, want 1", w.EngineVersion)
	}
	if _, err := old.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil {
		t.Fatalf("advance on a matching world: %v", err)
	}
	if ws, err := old.EngineWarnings(ctx, b.ID); err != nil || len(ws) != 0 {
		t.Fatalf("matching world warnings = %v, err %v", ws, err)
	}

	cur := app.NewBranchService(st, nil, nil)
	ws, err := cur.EngineWarnings(ctx, b.ID)
	if err != nil || len(ws) != 1 {
		t.Fatalf("warnings = %v, err %v", ws, err)
	}
	if ws[0].Code != "engine-version-mismatch" {
		t.Errorf("code = %q", ws[0].Code)
	}
	for _, want := range []string{"version 1", "version 2", "not be reproducible"} {
		if !strings.Contains(ws[0].Message, want) {
			t.Errorf("message %q lacks %q", ws[0].Message, want)
		}
	}
	if wws, err := cur.WorldEngineWarnings(ctx, w.ID); err != nil || len(wws) != 1 {
		t.Fatalf("world warnings = %v, err %v", wws, err)
	}

	// Reads still work.
	if _, err := cur.FrameAt(ctx, b.ID, 2); err != nil {
		t.Fatalf("read on a mismatched world: %v", err)
	}
	if _, err := cur.StateAt(ctx, b.ID, 2); err != nil {
		t.Fatalf("state read: %v", err)
	}

	// Mutations are refused.
	var em *app.EngineVersionMismatchError
	checks := map[string]error{}
	_, checks["advance"] = cur.Advance(ctx, app.AdvanceRequest{Branch: b.ID})
	_, checks["submit"] = cur.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 4, Commands: []sim.Command{}})
	_, checks["fork"] = cur.ForkBranch(ctx, b.ID, 2, "child", "")
	_, checks["inject"] = cur.InjectDisruption(ctx, app.InjectRequest{Branch: b.ID, Kind: sim.DisruptionDemandSurge, StartTick: 10, DurationTicks: 2, Target: -1})
	checks["end-window"] = cur.EndDecisionWindow(ctx, b.ID, 4, nil)
	for name, err := range checks {
		if !errors.Is(err, app.ErrEngineVersionMismatch) || !errors.As(err, &em) || em.Stored != "1" || em.Running != "2" {
			t.Errorf("%s err = %v, want engine version mismatch (1 vs 2)", name, err)
		}
	}
	if head, _ := cur.HeadTick(ctx, b.ID); head != 4 {
		t.Errorf("head moved to %d", head)
	}
}
