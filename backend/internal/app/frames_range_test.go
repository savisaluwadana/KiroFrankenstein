package app_test

import (
	"context"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// A cold range read equals per-tick FrameAt for every tick and replays once.
func TestFramesRangeColdEqualsFrameAtOneReplay(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()
	svc, b := snapshotWorld(t, mem)
	want := make([]sim.TickFrame, 61)
	for i := range want {
		f, err := svc.FrameAt(ctx, b.ID, sim.Tick(i))
		if err != nil {
			t.Fatal(err)
		}
		want[i] = f
	}
	ranges := [][2]sim.Tick{{0, 60}, {10, 40}, {25, 47}, {49, 60}, {30, 30}, {0, 0}}
	for _, r := range ranges {
		var log hookLog
		fresh := app.NewBranchService(mem, nil, nil)
		if _, err := fresh.HeadTick(ctx, b.ID); err != nil { // loads the head
			t.Fatal(err)
		}
		fresh.SetSnapshotHooks(log.hooks())
		got, err := fresh.FramesRange(ctx, b.ID, r[0], r[1])
		if err != nil {
			t.Fatalf("range %v: %v", r, err)
		}
		if len(got) != int(r[1]-r[0])+1 {
			t.Fatalf("range %v: %d frames", r, len(got))
		}
		for i, f := range got {
			if f.Hash != want[int(r[0])+i].Hash || f.Tick != want[int(r[0])+i].Tick {
				t.Fatalf("range %v: frame %d differs from FrameAt", r, int(r[0])+i)
			}
		}
		if len(log.starts) > 1 {
			t.Fatalf("range %v: %d replays %v, want at most 1", r, len(log.starts), log.starts)
		}
		if len(log.warns) != 0 {
			t.Fatalf("warnings: %v", log.warns)
		}
	}
	// A mid range resumes from the best snapshot at or before from-1.
	var log hookLog
	fresh := app.NewBranchService(mem, nil, nil)
	if _, err := fresh.HeadTick(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	fresh.SetSnapshotHooks(log.hooks())
	if _, err := fresh.FramesRange(ctx, b.ID, 30, 40); err != nil {
		t.Fatal(err)
	}
	if len(log.starts) != 1 || log.starts[0] != 24 {
		t.Fatalf("replay starts %v, want [24]", log.starts)
	}
	if _, err := fresh.FramesRange(ctx, b.ID, 30, 61); err == nil {
		t.Fatal("range beyond head must fail")
	}
}

// Across a fork: the child's range (before and after the fork tick) equals
// FrameAt, resuming from an ancestor snapshot, with one replay.
func TestFramesRangeAcrossForkAncestors(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()
	svc, parent := snapshotWorld(t, mem)
	child, err := svc.ForkBranch(ctx, parent.ID, 30, "child", app.ManagerHuman)
	if err != nil {
		t.Fatal(err)
	}
	advanceTo(t, svc, child.ID, 55)
	var want []sim.TickFrame
	for i := 0; i <= 55; i++ {
		f, err := svc.FrameAt(ctx, child.ID, sim.Tick(i))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, f)
	}
	var log hookLog
	fresh := app.NewBranchService(mem, nil, nil)
	if _, err := fresh.HeadTick(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	fresh.SetSnapshotHooks(log.hooks())
	got, err := fresh.FramesRange(ctx, child.ID, 26, 52)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range got {
		if f.Hash != want[26+i].Hash {
			t.Fatalf("tick %d differs", 26+i)
		}
	}
	if len(log.starts) > 1 {
		t.Fatalf("%d replays %v, want at most 1", len(log.starts), log.starts)
	}
	if len(log.starts) == 1 && log.starts[0] != 24 {
		t.Fatalf("resumed from %d, want the ancestor snapshot 24", log.starts[0])
	}
}
