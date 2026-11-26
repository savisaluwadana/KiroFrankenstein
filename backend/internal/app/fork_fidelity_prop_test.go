package app_test

import (
	"bytes"
	"context"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// ffAdvance advances branch id to at least target, submitting the plan's
// commands at each decision window reached on the way.
func ffAdvance(ctx context.Context, t *rapid.T, svc *app.BranchService, id app.BranchID, plan piPlan, target sim.Tick) {
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

// ffCheck asserts the fork-fidelity relations between parent and child up to f.
func ffCheck(ctx context.Context, t *rapid.T, svc *app.BranchService, parent, child app.BranchID, f sim.Tick) {
	for tick := sim.Tick(0); tick <= f; tick++ {
		ph, err := svc.HashAt(ctx, parent, tick)
		if err != nil {
			t.Fatalf("parent hash %d: %v", tick, err)
		}
		ch, err := svc.HashAt(ctx, child, tick)
		if err != nil {
			t.Fatalf("child hash %d: %v", tick, err)
		}
		if ph != ch {
			t.Fatalf("hash at tick %d (fork %d) differs: parent %x child %x", tick, f, ph, ch)
		}
	}
	ps, err := svc.StateAt(ctx, parent, f)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := svc.StateAt(ctx, child, f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sim.Encode(ps), sim.Encode(cs)) {
		t.Fatalf("state encoding at fork tick %d differs", f)
	}
}

// TestPropForkFidelity: a child forked at tick F (including grandchildren)
// has the parent's State_Hash at F and at every earlier tick, and an
// identical state encoding at F.
//
// Feature: parallel-simulation-mvp, Property 9: Fork fidelity
// Validates: Requirements 15.3 (INV-5)
func TestPropForkFidelity(t *testing.T) {
	sc, data := piScenario(t)
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		plan := piDrawPlan(t, sc)
		svc, root := piNewBranch(t, data)
		maxHead := sim.Tick(rapid.IntRange(1, 24).Draw(t, "head"))
		ffAdvance(ctx, t, svc, root, plan, maxHead)
		head, _ := svc.HeadTick(ctx, root)

		f := sim.Tick(rapid.Int64Range(0, int64(head)).Draw(t, "forkTick"))
		child, err := svc.ForkBranch(ctx, root, f, "child", "")
		if err != nil {
			t.Fatalf("fork at %d: %v", f, err)
		}
		ffCheck(ctx, t, svc, root, child.ID, f)

		// Grandchild, forked at a tick within the child's readable prefix.
		if rapid.Bool().Draw(t, "grandchild") {
			g := sim.Tick(rapid.Int64Range(0, int64(f)).Draw(t, "grandTick"))
			gc, err := svc.ForkBranch(ctx, child.ID, g, "grandchild", "")
			if err != nil {
				t.Fatalf("grandchild fork at %d: %v", g, err)
			}
			ffCheck(ctx, t, svc, child.ID, gc.ID, g)
			ffCheck(ctx, t, svc, root, gc.ID, g)
		}
	})
}
