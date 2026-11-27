package app_test

import (
	"context"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// diDrive submits the plan at every window and advances until the head is at
// least target.
func diDrive(ctx context.Context, t *rapid.T, svc *app.BranchService, id app.BranchID, plan piPlan, target sim.Tick) {
	for {
		head, err := svc.HeadTick(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if head >= target {
			return
		}
		piSubmit(ctx, t, svc, id, plan, head)
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id}); err != nil {
			t.Fatalf("advance from %d: %v", head, err)
		}
	}
}

func diAssertIdentical(t *rapid.T, what string, res app.DiffResult) {
	if res.FirstDivergenceTick != nil {
		t.Fatalf("%s: first divergence tick %d, want none", what, *res.FirstDivergenceTick)
	}
	if len(res.DifferingComponents) != 0 {
		t.Fatalf("%s: differing components %v", what, res.DifferingComponents)
	}
	if len(res.DifferingInputs) != 0 {
		t.Fatalf("%s: differing inputs %v", what, res.DifferingInputs)
	}
	if res.FirstEventA != nil || res.FirstEventB != nil {
		t.Fatalf("%s: unexpected first events", what)
	}
	for _, m := range res.Metrics {
		if m.Delta != 0 || m.A != m.B {
			t.Fatalf("%s: metric %s delta %d (A %d, B %d)", what, m.Key, m.Delta, m.A, m.B)
		}
	}
	for _, s := range res.SKUStock {
		if s.Delta != 0 || s.ValueDelta != 0 {
			t.Fatalf("%s: SKU %d stock delta %d value delta %d", what, s.SKU, s.Delta, s.ValueDelta)
		}
	}
}

// TestPropDiffIdentity: diffing a Branch with itself, with a fork taken at its
// head that has had no new Inputs, or with an independent Branch built from
// identical Inputs reports no divergence, no differing Inputs and zero deltas.
//
// Feature: parallel-simulation-mvp, Property 24: Diff identity
// Validates: Requirements 23.9
func TestPropDiffIdentity(t *testing.T) {
	sc, data := piScenario(t)
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		plan := piDrawPlan(t, sc)
		target := sim.Tick(rapid.Int64Range(0, 40).Draw(t, "head"))

		svc, root := piNewBranch(t, data)
		// Two independent branches forked from the same tick, before any advance.
		c1, err := svc.ForkBranch(ctx, root, 0, "c1", app.ManagerHuman)
		if err != nil {
			t.Fatalf("fork c1: %v", err)
		}
		c2, err := svc.ForkBranch(ctx, root, 0, "c2", app.ManagerHuman)
		if err != nil {
			t.Fatalf("fork c2: %v", err)
		}
		diDrive(ctx, t, svc, root, plan, target)
		diDrive(ctx, t, svc, c1.ID, plan, target)
		diDrive(ctx, t, svc, c2.ID, plan, target)

		// A Branch with itself.
		res, err := svc.Diff(ctx, root, root, nil)
		if err != nil {
			t.Fatalf("self diff: %v", err)
		}
		diAssertIdentical(t, "self", res)

		// A fork taken at the head, with no new Inputs.
		head, err := svc.HeadTick(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		if open, _ := sim.DecisionWindowOpen(sc, head); open {
			child, err := svc.ForkBranch(ctx, root, head, "atHead", app.ManagerHuman)
			if err != nil {
				t.Fatalf("fork at head %d: %v", head, err)
			}
			res, err := svc.Diff(ctx, root, child.ID, nil)
			if err != nil {
				t.Fatalf("fork diff: %v", err)
			}
			diAssertIdentical(t, "fork at head", res)
		}

		// Two independent Branches built from identical Inputs.
		res, err = svc.Diff(ctx, c1.ID, c2.ID, nil)
		if err != nil {
			t.Fatalf("sibling diff: %v", err)
		}
		diAssertIdentical(t, "siblings", res)
	})
}
