package postgres_test

import (
	"context"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/postgres"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

// runToHorizon advances until the branch reaches the horizon; a run-to stops
// early at disruption-onset windows, so it repeats.
func runToHorizon(t *testing.T, svc *app.BranchService, id app.BranchID, horizon sim.Tick) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 500; i++ {
		head, err := svc.HeadTick(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if head >= horizon {
			return
		}
		to := horizon
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id, RunToTick: &to}); err != nil {
			t.Fatalf("advance %s from %d: %v", id, head, err)
		}
	}
	t.Fatalf("branch %s did not reach the horizon", id)
}

// TestRestartRebuild: a cold service over the same database rebuilds every
// branch with hashes equal to service #1's and to the stored tick_summaries.
func TestRestartRebuild(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := postgres.New(pool)

	svc1 := app.NewBranchService(store, nil, nil)
	_, root, err := svc1.CreateWorld(ctx, app.CreateWorldRequest{Name: "restart", ScenarioJSON: scenarios.BlackFriday, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	var horizon sim.Tick
	{
		w, err := store.GetWorld(ctx, root.WorldID)
		if err != nil {
			t.Fatal(err)
		}
		p, err := sim.ParseScenario(w.ScenarioJSON)
		if err != nil {
			t.Fatal(err)
		}
		horizon = sim.Tick(p.HorizonTicks)
	}

	// root to tick 24, then two forks (24 and an earlier 4), each with a command
	to := sim.Tick(24)
	for {
		h, err := svc1.HeadTick(ctx, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		if h >= to {
			break
		}
		if _, err := svc1.Advance(ctx, app.AdvanceRequest{Branch: root.ID, RunToTick: &to}); err != nil {
			t.Fatal(err)
		}
	}
	po := []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 10}}
	ids := []app.BranchID{root.ID}
	for _, at := range []sim.Tick{24, 4} {
		child, err := svc1.ForkBranch(ctx, root.ID, at, "alt", "")
		if err != nil {
			t.Fatal(err)
		}
		sr, err := svc1.SubmitCommands(ctx, app.SubmitRequest{Branch: child.ID, WindowTick: at, Commands: po})
		if err != nil || len(sr.Accepted) != 1 {
			t.Fatalf("submit at %d: %+v err %v", at, sr, err)
		}
		ids = append(ids, child.ID)
	}
	for _, id := range ids {
		runToHorizon(t, svc1, id, horizon)
	}

	want := map[app.BranchID][]([32]byte){}
	for _, id := range ids {
		for tick := sim.Tick(0); tick <= horizon; tick++ {
			h, err := svc1.HashAt(ctx, id, tick)
			if err != nil {
				t.Fatal(err)
			}
			want[id] = append(want[id], h)
		}
	}

	// "restart": a cold service, same database, no in-process caches
	svc2 := app.NewBranchService(postgres.New(pool), nil, nil)
	svc2.SetSnapshotHooks(app.SnapshotHooks{
		Warn: func(b app.BranchID, tk sim.Tick, r string) { t.Errorf("warning on %s@%d: %s", b, tk, r) },
	})
	for _, id := range ids {
		b, err := store.GetBranch(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		head, err := svc2.HeadTick(ctx, id)
		if err != nil || head != horizon || b.HeadTick != horizon {
			t.Fatalf("branch %s head after restart = %d (stored %d) err %v, want %d", id, head, b.HeadTick, err, horizon)
		}
		rows, err := store.TickSummaries(ctx, id, 0, horizon)
		if err != nil {
			t.Fatal(err)
		}
		if wantRows := int(horizon - b.ForkTick); len(rows) != wantRows {
			t.Fatalf("branch %s has %d tick summaries, want %d", id, len(rows), wantRows)
		}
		for _, r := range rows {
			if r.Tick <= b.ForkTick {
				t.Fatalf("branch %s has a summary at tick %d <= fork %d", id, r.Tick, b.ForkTick)
			}
			if r.Hash != want[id][r.Tick] {
				t.Fatalf("branch %s tick %d: stored summary hash differs from service #1", id, r.Tick)
			}
			if len(r.Metrics) == 0 || len(r.FCR) == 0 {
				t.Fatalf("branch %s tick %d: empty projection payload", id, r.Tick)
			}
		}
		for tick := sim.Tick(0); tick <= horizon; tick++ {
			got, err := svc2.HashAt(ctx, id, tick)
			if err != nil {
				t.Fatalf("branch %s tick %d: %v", id, tick, err)
			}
			if got != want[id][tick] {
				t.Fatalf("branch %s tick %d: rebuilt hash differs from service #1", id, tick)
			}
		}
		if n, err := svc2.VerifyProjections(ctx, id); err != nil || n != len(rows) {
			t.Fatalf("branch %s VerifyProjections = %d, %v; want %d rows", id, n, err, len(rows))
		}
	}
}
