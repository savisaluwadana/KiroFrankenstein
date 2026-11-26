package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

var forkPO = []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 10}}

// parentAt4 returns a service with a root advanced to tick 4 that already
// holds a command for tick 5 (submitted in the window at 4).
func parentAt4(t *testing.T) (*app.BranchService, app.Branch, func(app.BranchID, int64) [32]byte) {
	t.Helper()
	svc, _, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil {
		t.Fatal(err)
	}
	if sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 4, Commands: forkPO}); err != nil || len(sr.Accepted) != 1 {
		t.Fatalf("submit = %+v err %v", sr, err)
	}
	hash := func(id app.BranchID, tick int64) [32]byte {
		h, err := svc.HashAt(ctx, id, sim.Tick(tick))
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	return svc, b, hash
}

func inputCount(t *testing.T, st interface {
	InputsForBranch(context.Context, app.BranchID, sim.Tick) ([]app.InputRecord, error)
}, id app.BranchID) int {
	t.Helper()
	recs, err := st.InputsForBranch(context.Background(), id, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	return len(recs)
}

func TestForkHashEqualityAndNoCopy(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil {
		t.Fatal(err)
	}
	// Input with effective tick 2 (<= fork) cannot be made via the window, so
	// use the window at 4 -> effective 5, which belongs to the parent's future.
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: 4, Commands: forkPO}); err != nil {
		t.Fatal(err)
	}
	child, err := svc.ForkBranch(ctx, b.ID, 4, "alt", "")
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentBranchID != b.ID || child.ForkTick != 4 || child.HeadTick != 4 || child.ManagerKind != app.ManagerHuman {
		t.Fatalf("child = %+v", child)
	}
	if n := inputCount(t, st, child.ID); n != 0 {
		t.Fatalf("child sees %d inputs at fork, want 0 (parent's tick-5 command is its future)", n)
	}
	recs, _ := st.InputsForBranch(ctx, child.ID, 1<<30)
	if len(recs) != 0 {
		t.Fatal("parent future input leaked into child")
	}
	ph, _ := svc.HashAt(ctx, b.ID, 4)
	ch, err := svc.HashAt(ctx, child.ID, 4)
	if err != nil || ph != ch {
		t.Fatalf("hash at fork: parent %x child %x err %v", ph, ch, err)
	}
	obs, err := svc.ObservationAt(ctx, child.ID, 4)
	if err != nil || !obs.Window.Open {
		t.Fatalf("fork window open = %v err %v", obs.Window.Open, err)
	}
	found := false
	for _, r := range obs.Window.Reasons {
		found = found || r == sim.WindowFork
	}
	if !found {
		t.Fatalf("reasons = %v, want fork window", obs.Window.Reasons)
	}
	if got, _ := svc.HeadTick(ctx, child.ID); got != 4 {
		t.Fatalf("child head = %d", got)
	}
	// Per-branch stored rows: parent keeps its one input, child has none.
	if n := inputCount(t, st, b.ID); n != 1 {
		t.Fatalf("parent rows = %d", n)
	}
}

func TestForkRejections(t *testing.T) {
	svc, b, _ := parentAt4(t)
	ctx := context.Background()
	if _, err := svc.ForkBranch(ctx, b.ID, 5, "x", ""); !errors.Is(err, app.ErrNotYetSimulated) {
		t.Fatalf("tick above head err = %v", err)
	}
	if _, err := svc.ForkBranch(ctx, b.ID, -1, "x", ""); !errors.Is(err, app.ErrInvalidTick) {
		t.Fatalf("negative tick err = %v", err)
	}
	if _, err := svc.ForkBranch(ctx, b.ID, 4, "", ""); !errors.Is(err, app.ErrInvalidName) {
		t.Fatalf("empty label err = %v", err)
	}
	if _, err := svc.ForkBranch(ctx, "nope", 0, "x", ""); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("unknown parent err = %v", err)
	}
}

func TestForkBranchLimit(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	for i := 1; i < app.MaxBranchesPerWorld; i++ { // root + 31 = 32
		if _, err := svc.ForkBranch(ctx, b.ID, 0, fmt.Sprintf("b%d", i), ""); err != nil {
			t.Fatalf("fork %d: %v", i, err)
		}
	}
	if _, err := svc.ForkBranch(ctx, b.ID, 0, "33rd", ""); !errors.Is(err, app.ErrBranchLimit) {
		t.Fatalf("33rd err = %v", err)
	}
	list, err := svc.ListBranches(ctx, b.WorldID)
	if err != nil || len(list) != app.MaxBranchesPerWorld {
		t.Fatalf("list = %d err %v", len(list), err)
	}
}

func TestForkIndependenceAndGrandchild(t *testing.T) {
	svc, b, hash := parentAt4(t)
	ctx := context.Background()
	child, err := svc.ForkBranch(ctx, b.ID, 4, "alt", app.ManagerAIRuleBased)
	if err != nil {
		t.Fatal(err)
	}
	parentBefore := [3][32]byte{hash(b.ID, 0), hash(b.ID, 4), {}}

	// Child acts at the fork window and advances.
	if sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: child.ID, WindowTick: 4, Commands: forkPO}); err != nil || len(sr.Accepted) != 1 {
		t.Fatalf("child submit = %+v err %v", sr, err)
	}
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: child.ID}); err != nil {
		t.Fatal(err)
	}
	if hash(b.ID, 0) != parentBefore[0] || hash(b.ID, 4) != parentBefore[1] {
		t.Fatal("child activity changed parent hashes")
	}
	if h, _ := svc.HeadTick(ctx, b.ID); h != 4 {
		t.Fatalf("parent head moved to %d", h)
	}

	// Parent advances after the fork; the child's computed hashes stay put.
	childHash8 := hash(child.ID, 8)
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil {
		t.Fatal(err)
	}
	if hash(child.ID, 8) != childHash8 || hash(child.ID, 4) != parentBefore[1] {
		t.Fatal("parent advance changed child hashes")
	}
	// Child's tick-5 command is its own; the parent's differs by its own row.
	if hash(child.ID, 5) != hash(b.ID, 5) {
		t.Fatal("identical commands at the fork window should give identical tick 5")
	}

	// Grandchild at tick 6 of the child sees the child's tick-5 input, not later ones.
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: child.ID, RunToTick: tickPtr(8)}); err == nil {
		t.Log("child already at/after 8")
	}
	gc, err := svc.ForkBranch(ctx, child.ID, 6, "gc", "")
	if err != nil {
		t.Fatal(err)
	}
	if hash(gc.ID, 6) != hash(child.ID, 6) {
		t.Fatal("grandchild hash differs at its fork tick")
	}
	list, _ := svc.ListBranches(ctx, b.WorldID)
	if len(list) != 3 {
		t.Fatalf("branches = %d", len(list))
	}
	got, err := svc.GetBranch(ctx, gc.ID)
	if err != nil || got.ParentBranchID != child.ID || got.ForkTick != 6 {
		t.Fatalf("grandchild = %+v err %v", got, err)
	}
}

func tickPtr(t sim.Tick) *sim.Tick { return &t }
