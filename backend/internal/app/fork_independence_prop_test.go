package app_test

import (
	"context"
	"os"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// fiSnap is everything observable about one branch that must not change when
// a different branch is modified.
type fiSnap struct {
	head   sim.Tick
	hashes [][32]byte
	inputs int
}

func fiTake(t *rapid.T, svc *app.BranchService, st *memory.Store, id app.BranchID) fiSnap {
	ctx := context.Background()
	head, err := svc.HeadTick(ctx, id)
	if err != nil {
		t.Fatalf("head %s: %v", id, err)
	}
	s := fiSnap{head: head}
	for k := sim.Tick(0); k <= head; k++ {
		h, err := svc.HashAt(ctx, id, k)
		if err != nil {
			t.Fatalf("hash %s@%d: %v", id, k, err)
		}
		s.hashes = append(s.hashes, h)
	}
	recs, err := st.InputsForBranch(ctx, id, 1<<30)
	if err != nil {
		t.Fatalf("inputs %s: %v", id, err)
	}
	s.inputs = len(recs)
	return s
}

// fiOp applies one random valid-ish mutation to a branch. Errors from
// windows that are closed or horizons reached are expected and ignored.
func fiOp(t *rapid.T, svc *app.BranchService, id app.BranchID, nSKU int64) {
	ctx := context.Background()
	switch rapid.IntRange(0, 2).Draw(t, "op") {
	case 0:
		_, _ = svc.Advance(ctx, app.AdvanceRequest{Branch: id})
	case 1:
		head, _ := svc.HeadTick(ctx, id)
		to := head + sim.Tick(rapid.IntRange(1, 6).Draw(t, "runTo"))
		_, _ = svc.Advance(ctx, app.AdvanceRequest{Branch: id, RunToTick: &to})
	default:
		head, _ := svc.HeadTick(ctx, id)
		cmd := sim.Command{
			Kind: sim.CmdPlacePurchaseOrder,
			SKU:  rapid.Int64Range(0, nSKU-1).Draw(t, "sku"),
			Role: sim.SupplierPrimary,
			Qty:  rapid.Int64Range(1, 20).Draw(t, "qty"),
		}
		_, _ = svc.SubmitCommands(ctx, app.SubmitRequest{Branch: id, WindowTick: head, Commands: []sim.Command{cmd}})
	}
}

// TestPropForkIndependence checks that mutating one branch never changes any
// hash, input count or head tick of any other branch (parent, child, sibling
// and grandchild directions).
//
// Feature: parallel-simulation-mvp, Property 10: Fork independence
//
// **Validates: Requirements 15.4**
func TestPropForkIndependence(t *testing.T) {
	data, err := os.ReadFile(simtest.FlagshipPath())
	if err != nil {
		t.Fatal(err)
	}
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		st := memory.New()
		svc := app.NewBranchService(st, nil, nil)
		seed := rapid.Int64Range(1, 1<<40).Draw(t, "seed")
		_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "w", ScenarioJSON: data, Seed: sim.Seed(seed)})
		if err != nil {
			t.Fatal(err)
		}
		const nSKU = 1 // SKU 0 always exists; keeps commands valid
		// Build the parent's prefix.
		for i, n := 0, rapid.IntRange(1, 3).Draw(t, "parentWindows"); i < n; i++ {
			if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: root.ID}); err != nil {
				t.Fatal(err)
			}
			if rapid.Bool().Draw(t, "parentCmd") {
				fiOp(t, svc, root.ID, nSKU)
			}
		}
		ids := []app.BranchID{root.ID}
		fork := func(parent app.BranchID, name string) {
			head, _ := svc.HeadTick(ctx, parent)
			at := sim.Tick(rapid.Int64Range(0, int64(head)).Draw(t, "forkAt"+name))
			b, err := svc.ForkBranch(ctx, parent, at, name, "")
			if err != nil {
				t.Fatalf("fork %s at %d: %v", name, at, err)
			}
			ids = append(ids, b.ID)
		}
		fork(root.ID, "a")
		fork(root.ID, "b")
		if rapid.Bool().Draw(t, "grandchild") {
			fork(ids[1], "g")
		}

		for step, n := 0, rapid.IntRange(1, 6).Draw(t, "steps"); step < n; step++ {
			target := rapid.IntRange(0, len(ids)-1).Draw(t, "target")
			before := make(map[app.BranchID]fiSnap, len(ids))
			for i, id := range ids {
				if i != target {
					before[id] = fiTake(t, svc, st, id)
				}
			}
			fiOp(t, svc, ids[target], nSKU)
			for i, id := range ids {
				if i == target {
					continue
				}
				after := fiTake(t, svc, st, id)
				b := before[id]
				if after.head != b.head || after.inputs != b.inputs || len(after.hashes) != len(b.hashes) {
					t.Fatalf("op on branch %d changed branch %d: head %d->%d inputs %d->%d", target, i, b.head, after.head, b.inputs, after.inputs)
				}
				for k := range b.hashes {
					if after.hashes[k] != b.hashes[k] {
						t.Fatalf("op on branch %d changed branch %d hash at tick %d", target, i, k)
					}
				}
			}
		}
	})
}
