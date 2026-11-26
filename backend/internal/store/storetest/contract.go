// Package storetest is the reusable repository contract suite. Every
// app.Store implementation (memory, PostgreSQL) must pass Run.
package storetest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Factory returns a fresh, empty store.
type Factory func(t *testing.T) app.Store

func seed(t *testing.T, s app.Store, world app.WorldID, branches ...app.Branch) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateWorld(ctx, app.World{ID: world, Name: "w", ScenarioJSON: []byte(`{}`), Seed: 7, EngineVersion: "1"}); err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		b.WorldID = world
		if b.Label == "" {
			b.Label = "l"
		}
		if b.ManagerKind == "" {
			b.ManagerKind = app.ManagerHuman
		}
		if err := s.CreateBranch(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
}

func cmdInput(tick sim.Tick, qty int64, key string) app.NewInput {
	return app.NewCommandInput(tick, sim.Command{Kind: sim.CmdSetPrice, SKU: 1, Qty: qty}, app.Metadata{Provenance: app.ProvHuman, IdempotencyKey: key})
}

// Run executes the contract against stores built by f.
func Run(t *testing.T, f Factory) {
	ctx := context.Background()

	t.Run("world and branch round trip", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "b1"})
		w, err := s.GetWorld(ctx, "w1")
		if err != nil || w.Seed != 7 || string(w.ScenarioJSON) != "{}" {
			t.Fatalf("world %+v err %v", w, err)
		}
		if err := s.CreateWorld(ctx, app.World{ID: "w1"}); !errors.Is(err, app.ErrAlreadyExists) {
			t.Fatalf("duplicate world: %v", err)
		}
		if _, err := s.GetWorld(ctx, "nope"); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("missing world: %v", err)
		}
		if err := s.SetHeadTick(ctx, "b1", 9); err != nil {
			t.Fatal(err)
		}
		b, _ := s.GetBranch(ctx, "b1")
		if b.HeadTick != 9 {
			t.Fatalf("head tick %d", b.HeadTick)
		}
	})

	t.Run("count branches per world", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "b1"}, app.Branch{ID: "b2", ParentBranchID: "b1", ForkTick: 3})
		seed(t, s, "w2", app.Branch{ID: "b3"})
		n1, _ := s.CountBranches(ctx, "w1")
		n2, _ := s.CountBranches(ctx, "w2")
		l, _ := s.ListBranches(ctx, "w1")
		if n1 != 2 || n2 != 1 || len(l) != 2 || l[0].ID != "b1" || l[1].ID != "b2" {
			t.Fatalf("counts %d %d list %+v", n1, n2, l)
		}
	})

	t.Run("append order and per-branch sequence numbers", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "a"}, app.Branch{ID: "b"})
		for i, id := range []app.BranchID{"a", "a", "b", "a", "b"} {
			rec, err := s.AppendInput(ctx, id, cmdInput(sim.Tick(10-i), int64(i), ""))
			if err != nil {
				t.Fatal(err)
			}
			wantSeq := map[app.BranchID][]uint64{"a": {1, 2, 3}, "b": {1, 2}}[id]
			_ = wantSeq
			_ = rec
		}
		a, _ := s.InputsForBranch(ctx, "a", 100)
		b, _ := s.InputsForBranch(ctx, "b", 100)
		if len(a) != 3 || len(b) != 2 {
			t.Fatalf("lens %d %d", len(a), len(b))
		}
		// Seq is per branch and strictly increasing in append order.
		seqs := map[uint64]int64{}
		for _, r := range a {
			c, err := sim.DecodeCommand(r.Payload)
			if err != nil {
				t.Fatal(err)
			}
			seqs[r.Seq] = c.Qty
		}
		if seqs[1] != 0 || seqs[2] != 1 || seqs[3] != 3 {
			t.Fatalf("branch a seq->qty %v", seqs)
		}
		for i, r := range b {
			if r.BranchID != "b" || r.Kind != sim.InputCommand || r.Metadata.Provenance != app.ProvHuman {
				t.Fatalf("record %+v", r)
			}
			_ = i
		}
		// Result is ordered by effective tick.
		for i := 1; i < len(a); i++ {
			if a[i-1].EffectiveTick > a[i].EffectiveTick {
				t.Fatalf("not ordered by effective tick: %+v", a)
			}
		}
		// upTo filter.
		part, _ := s.InputsForBranch(ctx, "a", 7)
		for _, r := range part {
			if r.EffectiveTick > 7 {
				t.Fatalf("tick %d beyond upTo", r.EffectiveTick)
			}
		}
		if _, err := s.AppendInput(ctx, "missing", cmdInput(1, 1, "")); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("unknown branch: %v", err)
		}
	})

	t.Run("sequence numbers are 1-based and gapless under concurrency", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "a"})
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if _, err := s.AppendInput(ctx, "a", cmdInput(1, int64(i), "")); err != nil {
					t.Error(err)
				}
			}(i)
		}
		wg.Wait()
		rs, _ := s.InputsForBranch(ctx, "a", 1)
		if len(rs) != 20 {
			t.Fatalf("got %d", len(rs))
		}
		for i, r := range rs {
			if r.Seq != uint64(i+1) {
				t.Fatalf("seq %d at %d", r.Seq, i)
			}
		}
	})

	t.Run("idempotency key returns the existing input", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "a"}, app.Branch{ID: "b"})
		first, err := s.AppendInput(ctx, "a", cmdInput(2, 5, "k1"))
		if err != nil {
			t.Fatal(err)
		}
		dup, err := s.AppendInput(ctx, "a", cmdInput(3, 6, "k1"))
		if !errors.Is(err, app.ErrDuplicateIdempotencyKey) || dup.Seq != first.Seq || dup.EffectiveTick != 2 {
			t.Fatalf("dup %+v err %v", dup, err)
		}
		got, ok, _ := s.FindByIdempotencyKey(ctx, "a", "k1")
		if !ok || got.Seq != first.Seq {
			t.Fatalf("find %+v %v", got, ok)
		}
		if _, ok, _ := s.FindByIdempotencyKey(ctx, "b", "k1"); ok {
			t.Fatal("keys must be scoped to a branch")
		}
		if _, err := s.AppendInput(ctx, "b", cmdInput(2, 5, "k1")); err != nil {
			t.Fatalf("same key on another branch: %v", err)
		}
		rs, _ := s.InputsForBranch(ctx, "a", 100)
		if len(rs) != 1 {
			t.Fatalf("duplicate stored: %d", len(rs))
		}
		// Empty keys never collide.
		for i := 0; i < 2; i++ {
			if _, err := s.AppendInput(ctx, "a", cmdInput(4, 1, "")); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("fork resolves through the ancestor chain", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1",
			app.Branch{ID: "root"},
			app.Branch{ID: "child", ParentBranchID: "root", ForkTick: 5},
			app.Branch{ID: "grand", ParentBranchID: "child", ForkTick: 8},
			app.Branch{ID: "sib", ParentBranchID: "root", ForkTick: 5})
		add := func(b app.BranchID, tick sim.Tick, qty int64) {
			if _, err := s.AppendInput(ctx, b, cmdInput(tick, qty, "")); err != nil {
				t.Fatal(err)
			}
		}
		add("root", 3, 1)
		add("root", 5, 2)  // visible to child (<= fork tick)
		add("root", 6, 99) // after fork: invisible to child
		add("child", 6, 3)
		add("child", 8, 4)
		add("child", 9, 98) // after grandchild fork: invisible to grand
		add("grand", 9, 5)
		add("sib", 6, 77)
		qtys := func(b app.BranchID) []int64 {
			rs, err := s.InputsForBranch(ctx, b, 100)
			if err != nil {
				t.Fatal(err)
			}
			var out []int64
			for _, r := range rs {
				c, _ := sim.DecodeCommand(r.Payload)
				out = append(out, c.Qty)
			}
			return out
		}
		want := map[app.BranchID][]int64{
			"root":  {1, 2, 99},
			"child": {1, 2, 3, 4, 98},
			"grand": {1, 2, 3, 4, 5},
			"sib":   {1, 2, 77},
		}
		for b, w := range want {
			if got := qtys(b); !reflect.DeepEqual(got, w) {
				t.Errorf("%s: got %v want %v", b, got, w)
			}
		}
	})

	t.Run("snapshots", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "a"})
		if _, ok, _ := s.LoadLatestSnapshot(ctx, "a", 50); ok {
			t.Fatal("unexpected snapshot")
		}
		h1, h2 := [32]byte{1}, [32]byte{2}
		for _, sn := range []app.Snapshot{{BranchID: "a", Tick: 24, Hash: h1, Blob: []byte("x")}, {BranchID: "a", Tick: 48, Hash: h2, Blob: []byte("y")}} {
			if err := s.SaveSnapshot(ctx, sn); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.SaveSnapshot(ctx, app.Snapshot{BranchID: "a", Tick: 24, Hash: h1, Blob: []byte("x")}); err != nil {
			t.Fatalf("idempotent save: %v", err)
		}
		if err := s.SaveSnapshot(ctx, app.Snapshot{BranchID: "a", Tick: 24, Hash: h2, Blob: []byte("z")}); !errors.Is(err, app.ErrSnapshotConflict) {
			t.Fatalf("conflict: %v", err)
		}
		got, ok, _ := s.LoadLatestSnapshot(ctx, "a", 47)
		if !ok || got.Tick != 24 || got.Hash != h1 || string(got.Blob) != "x" {
			t.Fatalf("latest@47 %+v %v", got, ok)
		}
		got, _, _ = s.LoadLatestSnapshot(ctx, "a", 48)
		if got.Tick != 48 {
			t.Fatalf("latest@48 %+v", got)
		}
	})

	t.Run("stored data is isolated from caller mutation", func(t *testing.T) {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "a"})
		in := cmdInput(1, 1, "")
		rec, _ := s.AppendInput(ctx, "a", in)
		in.Payload[0] ^= 0xff
		rec.Payload[1] ^= 0xff
		rs, _ := s.InputsForBranch(ctx, "a", 1)
		if _, err := sim.DecodeCommand(rs[0].Payload); err != nil || rs[0].Payload[0] != byte(sim.CmdSetPrice) || rs[0].Payload[1] != 0 {
			t.Fatalf("stored payload changed: %v", rs[0].Payload)
		}
	})

	t.Run("input repo has no mutation operations", func(t *testing.T) {
		typ := reflect.TypeOf((*app.InputRepo)(nil)).Elem()
		for i := 0; i < typ.NumMethod(); i++ {
			n := typ.Method(i).Name
			for _, bad := range []string{"Update", "Delete", "Remove", "Set", "Put", "Replace", "Truncate"} {
				if strings.HasPrefix(n, bad) {
					t.Errorf("InputRepo exposes mutation method %s", n)
				}
			}
		}
	})
}
