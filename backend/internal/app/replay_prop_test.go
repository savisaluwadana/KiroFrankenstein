package app_test

import (
	"context"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// reNoSnapshots wraps a Store and hides every snapshot, forcing replay from Init.
type reNoSnapshots struct{ app.Store }

func (reNoSnapshots) LoadLatestSnapshot(context.Context, app.BranchID, sim.Tick) (app.Snapshot, bool, error) {
	return app.Snapshot{}, false, nil
}

// reAdvanceTo advances the branch window by window, submitting the plan, until head >= target.
func reAdvanceTo(ctx context.Context, t *rapid.T, svc *app.BranchService, id app.BranchID, plan piPlan, target sim.Tick) sim.Tick {
	for {
		head, err := svc.HeadTick(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if head >= target {
			return head
		}
		piSubmit(ctx, t, svc, id, plan, head)
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id}); err != nil {
			t.Fatalf("advance from %d: %v", head, err)
		}
	}
}

// TestPropReplayEquivalence: the state at any tick is the same whether it is
// rebuilt from the latest Snapshot, from Tick 0 with no Snapshots, or read
// from the live service, including on forked Branches whose Snapshots sit on
// the ancestor chain.
//
// Feature: parallel-simulation-mvp, Property 8: Replay equivalence from snapshots
// Validates: Requirements 14.3 (INV-2)
func TestPropReplayEquivalence(t *testing.T) {
	sc, data := piScenario(t)
	iv := sim.Tick(sc.SnapshotIntervalTicks)
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		plan := piDrawPlan(t, sc)
		store := memory.New()
		live := app.NewBranchService(store, nil, nil)
		_, root, err := live.CreateWorld(ctx, app.CreateWorldRequest{Name: "w", ScenarioJSON: data, Seed: 2026})
		if err != nil {
			t.Fatal(err)
		}
		target := sim.Tick(rapid.Int64Range(int64(iv)+1, 60).Draw(t, "head"))
		rootHead := reAdvanceTo(ctx, t, live, root.ID, plan, target)

		branches := []app.BranchID{root.ID}
		if rapid.Bool().Draw(t, "fork") {
			// Fork at a window tick at or before the root head.
			var windows []sim.Tick
			for tick := sim.Tick(1); tick <= rootHead; tick++ {
				if open, _ := sim.DecisionWindowOpen(sc, tick); open {
					windows = append(windows, tick)
				}
			}
			if len(windows) > 0 {
				at := rapid.SampledFrom(windows).Draw(t, "forkAt")
				child, err := live.ForkBranch(ctx, root.ID, at, "c", app.ManagerHuman)
				if err != nil {
					t.Fatalf("fork at %d: %v", at, err)
				}
				childTarget := sim.Tick(rapid.Int64Range(int64(at), 60+int64(iv)).Draw(t, "childHead"))
				// The child's own plan is drawn separately so it diverges from the parent.
				reAdvanceTo(ctx, t, live, child.ID, piDrawPlan(t, sc), childTarget)
				branches = append(branches, child.ID)
			}
		}

		var coldStart, bareStart sim.Tick
		// Fresh cold services per check: no cached head, so the read replays.
		newCold := func() *app.BranchService {
			c := app.NewBranchService(store, nil, nil)
			c.SetSnapshotHooks(app.SnapshotHooks{
				Warn:        func(b app.BranchID, tk sim.Tick, r string) { t.Fatalf("unexpected warning %s@%d: %s", b, tk, r) },
				ReplayStart: func(_ app.BranchID, from sim.Tick) { coldStart = from },
			})
			return c
		}
		newBare := func() *app.BranchService {
			c := app.NewBranchService(reNoSnapshots{store}, nil, nil)
			c.SetSnapshotHooks(app.SnapshotHooks{
				ReplayStart: func(_ app.BranchID, from sim.Tick) { bareStart = from },
			})
			return c
		}

		for _, id := range branches {
			head, err := live.HeadTick(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			for i, n := 0, rapid.IntRange(2, 4).Draw(t, "ticks"); i < n; i++ {
				at := sim.Tick(rapid.Int64Range(0, int64(head)).Draw(t, "T"))
				want, err := live.HashAt(ctx, id, at)
				if err != nil {
					t.Fatalf("live HashAt %d: %v", at, err)
				}

				coldStart, bareStart = -1, -1
				st, err := newCold().StateAt(ctx, id, at)
				if err != nil {
					t.Fatalf("cold StateAt %d: %v", at, err)
				}
				if sim.Hash(st) != want {
					t.Fatalf("branch %s tick %d: snapshot-resumed hash differs from live", id, at)
				}
				if coldStart < 0 {
					t.Fatalf("branch %s tick %d: cold service did not replay", id, at)
				}
				if wantStart := (at / iv) * iv; coldStart != wantStart {
					t.Fatalf("branch %s tick %d: replay started at %d, want latest snapshot %d", id, at, coldStart, wantStart)
				}

				bst, err := newBare().StateAt(ctx, id, at)
				if err != nil {
					t.Fatalf("bare StateAt %d: %v", at, err)
				}
				if sim.Hash(bst) != want {
					t.Fatalf("branch %s tick %d: no-snapshot replay hash differs from live", id, at)
				}
				if bareStart != 0 {
					t.Fatalf("branch %s tick %d: snapshot-less replay started at %d", id, at, bareStart)
				}

				ch, err := newCold().HashAt(ctx, id, at)
				if err != nil || ch != want {
					t.Fatalf("cold HashAt %d: err %v, match %v", at, err, err == nil && ch == want)
				}
			}
		}
	})
}
