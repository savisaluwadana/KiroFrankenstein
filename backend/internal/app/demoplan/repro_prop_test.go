package demoplan

import (
	"context"
	"errors"
	"os"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

type reproRun struct {
	parent [][32]byte
	child  [][32]byte // nil when the run has no fork
}

func reproHashes(ctx context.Context, t *rapid.T, svc *app.BranchService, id app.BranchID, horizon sim.Tick) [][32]byte {
	out := make([][32]byte, 0, horizon+1)
	for tick := sim.Tick(0); tick <= horizon; tick++ {
		h, err := svc.HashAt(ctx, id, tick)
		if err != nil {
			t.Fatalf("hash at %d: %v", tick, err)
		}
		out = append(out, h)
	}
	return out
}

func reproToHorizon(ctx context.Context, t *rapid.T, svc *app.BranchService, id app.BranchID, horizon sim.Tick) {
	for {
		head, err := svc.HeadTick(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if head >= horizon {
			return
		}
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id}); err != nil {
			t.Fatalf("advance from %d: %v", head, err)
		}
	}
}

// reproExecute runs the plan from scratch. With fork false the parent runs
// alone to the horizon.
func reproExecute(ctx context.Context, t *rapid.T, data []byte, seed sim.Seed, plan Plan, horizon sim.Tick, fork bool) reproRun {
	svc := app.NewBranchService(memory.New(), nil, nil)
	_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "w", ScenarioJSON: data, Seed: seed})
	if err != nil {
		t.Fatal(err)
	}
	var child app.Branch
	if fork {
		ft := sim.Tick(plan.ForkTick)
		for {
			head, err := svc.HeadTick(ctx, root.ID)
			if err != nil {
				t.Fatal(err)
			}
			if head >= ft {
				break
			}
			if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: root.ID}); err != nil {
				t.Fatalf("advance from %d: %v", head, err)
			}
		}
		child, err = svc.ForkBranch(ctx, root.ID, ft, "demo", "")
		if err != nil {
			t.Fatalf("fork at %d: %v", ft, err)
		}
		if cmds := plan.SimCommands(); len(cmds) > 0 {
			res, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: child.ID, WindowTick: ft, Commands: cmds})
			if err != nil {
				t.Fatalf("submit: %v", err)
			}
			if len(res.Rejected) > 0 {
				t.Fatalf("demo commands rejected: %+v", res.Rejected)
			}
		}
		reproToHorizon(ctx, t, svc, child.ID, horizon)
	}
	reproToHorizon(ctx, t, svc, root.ID, horizon)
	out := reproRun{parent: reproHashes(ctx, t, svc, root.ID, horizon)}
	if fork {
		out.child = reproHashes(ctx, t, svc, child.ID, horizon)
	}
	return out
}

func reproEqual(a, b [][32]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPropDemoReproducibility: for any seed, Derive is deterministic, the
// demo plan executed twice from scratch gives identical hash sequences for
// parent and child at every tick, and the parent equals a run with no fork.
//
// Iterations: the rapid default of 100 is kept. Each iteration runs three
// full 120-tick executions (about 0.6 s), so the test takes about 60 s;
// lower it with -rapid.checks for quick local runs.
//
// Feature: parallel-simulation-mvp, Property 41: Demo reproducibility
// Validates: Requirements 34.3 (INV-1)
func TestPropDemoReproducibility(t *testing.T) {
	data, err := os.ReadFile(scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := sim.ParseScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	sc := *parsed
	horizon := sim.Tick(sc.HorizonTicks)
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		seed := sim.Seed(rapid.Uint64().Draw(t, "seed"))
		plan, err := Derive(sc, seed)
		if err != nil {
			if errors.Is(err, ErrNoCrisis) || errors.Is(err, ErrNoForkTick) {
				t.Skip("no valid plan for seed")
			}
			t.Fatalf("derive: %v", err)
		}
		again, err := Derive(sc, seed)
		if err != nil {
			t.Fatalf("second derive: %v", err)
		}
		a, _ := plan.Fixture()
		b, _ := again.Fixture()
		if string(a) != string(b) {
			t.Fatalf("Derive not deterministic for seed %d", seed)
		}

		r1 := reproExecute(ctx, t, data, seed, plan, horizon, true)
		r2 := reproExecute(ctx, t, data, seed, plan, horizon, true)
		if !reproEqual(r1.parent, r2.parent) {
			t.Fatalf("parent hashes differ between runs (seed %d)", seed)
		}
		if !reproEqual(r1.child, r2.child) {
			t.Fatalf("child hashes differ between runs (seed %d)", seed)
		}
		nofork := reproExecute(ctx, t, data, seed, plan, horizon, false)
		if !reproEqual(r1.parent, nofork.parent) {
			t.Fatalf("parent hashes differ from a no-fork run (seed %d)", seed)
		}
	})
}
