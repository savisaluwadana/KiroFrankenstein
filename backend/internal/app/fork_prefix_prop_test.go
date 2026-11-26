package app_test

import (
	"context"
	"os"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// fpSubmit submits one purchase order in the branch's open window at its head.
// It returns the accepted records; a closed window or rejection yields none.
func fpSubmit(t *rapid.T, svc *app.BranchService, id app.BranchID, qty int64) []app.InputRecord {
	ctx := context.Background()
	head, err := svc.HeadTick(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cmd := sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: qty}
	sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: id, WindowTick: head, Commands: []sim.Command{cmd}})
	if err != nil {
		return nil
	}
	return sr.Accepted
}

// fpAdvance advances to the next window; false when the horizon is reached.
func fpAdvance(t *rapid.T, svc *app.BranchService, id app.BranchID) bool {
	_, err := svc.Advance(context.Background(), app.AdvanceRequest{Branch: id})
	return err == nil
}

// fpHashes returns hashes at ticks 0..head.
func fpHashes(t *rapid.T, svc *app.BranchService, id app.BranchID) [][32]byte {
	ctx := context.Background()
	head, err := svc.HeadTick(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var out [][32]byte
	for k := sim.Tick(0); k <= head; k++ {
		h, err := svc.HashAt(ctx, id, k)
		if err != nil {
			t.Fatalf("hash at %d: %v", k, err)
		}
		out = append(out, h)
	}
	return out
}

// Feature: parallel-simulation-mvp, Property 37: Fork prefix immutability after parent advances
// **Validates: Requirements 15.8**
func TestPropForkPrefixImmutability(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		st := memory.New()
		svc := app.NewBranchService(st, nil, nil)
		_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "w", ScenarioJSON: data, Seed: sim.Seed(rapid.Uint64().Draw(t, "seed"))})
		if err != nil {
			t.Fatal(err)
		}

		// Parent runs a few windows, submitting at each, before the fork.
		pre := rapid.IntRange(1, 4).Draw(t, "preWindows")
		for i := 0; i < pre; i++ {
			if !fpAdvance(t, svc, root.ID) {
				break
			}
			fpSubmit(t, svc, root.ID, rapid.Int64Range(1, 40).Draw(t, "qty"))
		}
		head, _ := svc.HeadTick(ctx, root.ID)
		// F is any tick up to H; F == H is the case where the parent already
		// holds an input at effective tick F+1 from its own window at F.
		var f sim.Tick
		if rapid.Bool().Draw(t, "atHead") {
			f = head
		} else {
			f = sim.Tick(rapid.Int64Range(0, int64(head)).Draw(t, "forkTick"))
		}
		child, err := svc.ForkBranch(ctx, root.ID, f, "c", app.ManagerHuman)
		if err != nil {
			t.Fatal(err)
		}

		// The child acts and advances a window so it has its own inputs.
		fpSubmit(t, svc, child.ID, rapid.Int64Range(1, 40).Draw(t, "childQty"))
		fpAdvance(t, svc, child.ID)
		fpSubmit(t, svc, child.ID, rapid.Int64Range(1, 40).Draw(t, "childQty2"))

		beforeIn, err := st.InputsForBranch(ctx, child.ID, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		beforeHash := fpHashes(t, svc, child.ID)

		// The parent advances further and receives more inputs.
		var parentLater []app.InputRecord
		for i, n := 0, rapid.IntRange(1, 6).Draw(t, "postWindows"); i < n; i++ {
			parentLater = append(parentLater, fpSubmit(t, svc, root.ID, rapid.Int64Range(1, 40).Draw(t, "pqty"))...)
			if !fpAdvance(t, svc, root.ID) {
				break
			}
		}

		afterIn, err := st.InputsForBranch(ctx, child.ID, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(beforeIn, afterIn) {
			t.Fatalf("child resolved inputs changed after parent advanced:\nbefore %+v\nafter  %+v", beforeIn, afterIn)
		}
		// No parent input beyond F may appear in the child's prefix.
		for _, r := range afterIn {
			if r.BranchID == root.ID && r.EffectiveTick > f {
				t.Fatalf("parent input seq %d at effective tick %d leaked into child forked at %d", r.Seq, r.EffectiveTick, f)
			}
		}
		for _, p := range parentLater {
			for _, r := range afterIn {
				if r.BranchID == p.BranchID && r.Seq == p.Seq {
					t.Fatalf("later parent input seq %d appears in child prefix", p.Seq)
				}
			}
		}

		if got := fpHashes(t, svc, child.ID); !reflect.DeepEqual(beforeHash, got) {
			t.Fatal("child hashes changed after parent advanced")
		}
		// Cold cache: a fresh service over the same store reproduces them.
		cold := app.NewBranchService(st, nil, nil)
		if got := fpHashes(t, cold, child.ID); !reflect.DeepEqual(beforeHash, got) {
			t.Fatal("cold-cache child hashes differ")
		}
	})
}
