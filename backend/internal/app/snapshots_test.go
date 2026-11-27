package app_test

import (
	"context"
	"os"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// tamperStore corrupts the blob of every loaded snapshot but keeps its hash.
type tamperStore struct{ *memory.Store }

func (t tamperStore) LoadLatestSnapshot(ctx context.Context, b app.BranchID, at sim.Tick) (app.Snapshot, bool, error) {
	s, ok, err := t.Store.LoadLatestSnapshot(ctx, b, at)
	if ok {
		blob := append([]byte(nil), s.Blob...)
		blob[len(blob)/2] ^= 0xff
		s.Blob = blob
	}
	return s, ok, err
}

type hookLog struct {
	starts []sim.Tick
	warns  []string
}

func (h *hookLog) hooks() app.SnapshotHooks {
	return app.SnapshotHooks{
		Warn:        func(_ app.BranchID, _ sim.Tick, r string) { h.warns = append(h.warns, r) },
		ReplayStart: func(_ app.BranchID, from sim.Tick) { h.starts = append(h.starts, from) },
	}
}

func advanceTo(t *testing.T, svc *app.BranchService, b app.BranchID, to sim.Tick) {
	t.Helper()
	for {
		head, err := svc.HeadTick(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		if head >= to {
			return
		}
		if _, err := svc.Advance(context.Background(), app.AdvanceRequest{Branch: b, RunToTick: &to}); err != nil {
			t.Fatal(err)
		}
	}
}

func snapshotWorld(t *testing.T, st app.Store) (*app.BranchService, app.Branch) {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewBranchService(st, nil, nil)
	_, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{Name: "w", ScenarioJSON: data, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	advanceTo(t, svc, b.ID, 60)
	return svc, b
}

func TestSnapshotsWrittenAtIntervalAndResumedFrom(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()
	svc, b := snapshotWorld(t, mem)
	for _, tick := range []sim.Tick{24, 48} {
		s, ok, err := mem.LoadLatestSnapshot(ctx, b.ID, tick)
		if err != nil || !ok || s.Tick != tick || s.Hash != sim.Hash(mustDecode(t, s.Blob)) {
			t.Fatalf("snapshot at %d: %+v ok %v err %v", tick, s.Tick, ok, err)
		}
	}
	if s, _, _ := mem.LoadLatestSnapshot(ctx, b.ID, 60); s.Tick != 48 {
		t.Fatalf("latest snapshot at 60 = %d", s.Tick)
	}

	// A fresh service has no caches: reads must resume from snapshots.
	var log hookLog
	fresh := app.NewBranchService(mem, nil, nil)
	fresh.SetSnapshotHooks(log.hooks())
	cases := []struct{ tick, wantStart sim.Tick }{{55, 48}, {30, 24}, {10, 0}, {48, 48}}
	for _, c := range cases {
		want, err := svc.HashAt(ctx, b.ID, c.tick)
		if err != nil {
			t.Fatal(err)
		}
		log.starts = nil
		st, err := fresh.StateAt(ctx, b.ID, c.tick)
		if err != nil || sim.Hash(st) != want {
			t.Fatalf("StateAt(%d) hash differs from from-scratch hash (err %v)", c.tick, err)
		}
		if got := log.starts[len(log.starts)-1]; got != c.wantStart {
			t.Fatalf("StateAt(%d) resumed from %d, want %d", c.tick, got, c.wantStart)
		}
		f, err := fresh.FrameAt(ctx, b.ID, c.tick)
		if err != nil || f.Hash != want {
			t.Fatalf("FrameAt(%d) hash differs (err %v)", c.tick, err)
		}
	}
	if len(log.warns) != 0 {
		t.Fatalf("unexpected warnings: %v", log.warns)
	}
}

func mustDecode(t *testing.T, blob []byte) sim.State {
	t.Helper()
	st, err := sim.Decode(blob)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSnapshotsTamperedBlobDiscarded(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()
	svc, b := snapshotWorld(t, mem)
	want, err := svc.HashAt(ctx, b.ID, 55)
	if err != nil {
		t.Fatal(err)
	}
	var log hookLog
	bad := app.NewBranchService(tamperStore{mem}, nil, nil)
	bad.SetSnapshotHooks(log.hooks())
	st, err := bad.StateAt(ctx, b.ID, 55)
	if err != nil || sim.Hash(st) != want {
		t.Fatalf("tampered read: hash mismatch or err %v", err)
	}
	if len(log.warns) == 0 {
		t.Fatal("expected a snapshot-mismatch warning")
	}
	for _, s := range log.starts {
		if s != 0 {
			t.Fatalf("replay resumed from %d despite tampered snapshot", s)
		}
	}
	if _, ok, _ := mem.LoadLatestSnapshot(ctx, b.ID, 55); !ok {
		t.Fatal("tampered snapshot row must not be deleted")
	}
}

func TestSnapshotsForkChildUsesAncestorSnapshots(t *testing.T) {
	ctx := context.Background()
	mem := memory.New()
	svc, root := snapshotWorld(t, mem)
	child, err := svc.ForkBranch(ctx, root.ID, 30, "alt", app.ManagerHuman)
	if err != nil {
		t.Fatal(err)
	}
	advanceTo(t, svc, child.ID, 60)
	if _, ok, _ := mem.LoadLatestSnapshot(ctx, child.ID, 30); ok {
		t.Fatal("child must not save snapshots at or before its fork tick")
	}
	if s, ok, _ := mem.LoadLatestSnapshot(ctx, child.ID, 60); !ok || s.Tick != 48 {
		t.Fatalf("child snapshot at 60 = %+v ok %v", s.Tick, ok)
	}
	var log hookLog
	fresh := app.NewBranchService(mem, nil, nil)
	fresh.SetSnapshotHooks(log.hooks())
	for _, tick := range []sim.Tick{28, 40} {
		want, err := svc.HashAt(ctx, child.ID, tick)
		if err != nil {
			t.Fatal(err)
		}
		log.starts = nil
		st, err := fresh.StateAt(ctx, child.ID, tick)
		if err != nil || sim.Hash(st) != want {
			t.Fatalf("child StateAt(%d) differs (err %v)", tick, err)
		}
		if got := log.starts[len(log.starts)-1]; got != 24 {
			t.Fatalf("child StateAt(%d) resumed from %d, want ancestor snapshot 24", tick, got)
		}
	}
	// Past the fork the child's own snapshot is used.
	log.starts = nil
	if _, err := fresh.StateAt(ctx, child.ID, 55); err != nil {
		t.Fatal(err)
	}
	if got := log.starts[len(log.starts)-1]; got != 48 {
		t.Fatalf("child StateAt(55) resumed from %d, want 48", got)
	}
}
