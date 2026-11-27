package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// dnCmd is one planned place_purchase_order command (SKU 0 always exists).
type dnCmd struct {
	qty int64
	// when is true when the command is submitted in the window at the head
	// (or fork) tick before the following advance.
	when bool
}

// dnPlan is a random recipe for a root, a child fork and an optional
// grandchild fork. Building it twice must give the same inputs.
type dnPlan struct {
	seed      int64
	rootWins  int
	rootCmds  []dnCmd
	childFork int64 // as a fraction of the root head, resolved at build time
	childCmds []int64
	childRun  int64
	grand     bool
	grandFrac int64
	grandCmds []int64
	grandRun  int64
}

func dnDrawPlan(t *rapid.T) dnPlan {
	p := dnPlan{
		seed:      rapid.Int64Range(1, 1<<40).Draw(t, "seed"),
		rootWins:  rapid.IntRange(1, 3).Draw(t, "rootWins"),
		childFork: rapid.Int64Range(0, 100).Draw(t, "childFork"),
		childRun:  rapid.Int64Range(0, 8).Draw(t, "childRun"),
		grand:     rapid.Bool().Draw(t, "grand"),
		grandFrac: rapid.Int64Range(0, 100).Draw(t, "grandFrac"),
		grandRun:  rapid.Int64Range(0, 6).Draw(t, "grandRun"),
	}
	for i := 0; i < p.rootWins; i++ {
		p.rootCmds = append(p.rootCmds, dnCmd{
			qty:  rapid.Int64Range(1, 20).Draw(t, "rootQty"),
			when: rapid.Bool().Draw(t, "rootCmd"),
		})
	}
	p.childCmds = rapid.SliceOfN(rapid.Int64Range(1, 20), 0, 2).Draw(t, "childCmds")
	p.grandCmds = rapid.SliceOfN(rapid.Int64Range(1, 20), 0, 2).Draw(t, "grandCmds")
	return p
}

// dnWorld is one independently built service with its three branches.
type dnWorld struct {
	svc          *app.BranchService
	st           *memory.Store
	root, ch, gc app.Branch
	hasGrand     bool
	worldID      app.WorldID
}

func dnCmds(qtys []int64) []sim.Command {
	out := make([]sim.Command, 0, len(qtys))
	for _, q := range qtys {
		out = append(out, sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: q})
	}
	return out
}

// dnBuild builds the plan in a fresh store. variant changes only data that
// must not matter: ids, clock, names, labels, rationale, model name and
// idempotency keys.
func dnBuild(t *rapid.T, data []byte, p dnPlan, variant int) dnWorld {
	ctx := context.Background()
	st := memory.New()
	n := 0
	newID := func() string {
		n++
		return fmt.Sprintf("v%d-id-%04d", variant, n*(variant+1)*7919%100003)
	}
	base := time.Date(2026, 1, 1+variant, 0, 0, 0, 0, time.UTC)
	k := 0
	now := func() time.Time { k++; return base.Add(time.Duration(k*(variant+1)) * time.Second) }
	svc := app.NewBranchService(st, newID, now)
	w, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{
		Name: fmt.Sprintf("world-%d", variant), ScenarioJSON: data, Seed: sim.Seed(p.seed),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	out := dnWorld{svc: svc, st: st, root: root, worldID: w.ID}
	idem := 0
	submit := func(id app.BranchID, qtys []int64) {
		if len(qtys) == 0 {
			return
		}
		head, err := svc.HeadTick(ctx, id)
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		idem++
		// Rejections (closed window, budget) are deterministic and ignored.
		_, _ = svc.SubmitCommands(ctx, app.SubmitRequest{
			Branch: id, WindowTick: head, Commands: dnCmds(qtys),
			IdempotencyKey: fmt.Sprintf("k%d-%d", variant, idem),
			Metadata: app.Metadata{
				Provenance: app.ProvHuman, Rationale: fmt.Sprintf("why %d", variant),
				ModelName: fmt.Sprintf("model-%d", variant),
			},
		})
	}
	run := func(id app.BranchID, ticks int64) {
		head, _ := svc.HeadTick(ctx, id)
		to := head + sim.Tick(ticks)
		_, _ = svc.Advance(ctx, app.AdvanceRequest{Branch: id, RunToTick: &to})
	}
	for _, c := range p.rootCmds {
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: root.ID}); err != nil {
			t.Fatalf("advance root: %v", err)
		}
		if c.when {
			submit(root.ID, []int64{c.qty})
		}
	}
	run(root.ID, 2)
	rh, _ := svc.HeadTick(ctx, root.ID)
	at := sim.Tick(int64(rh) * p.childFork / 100)
	ch, err := svc.ForkBranch(ctx, root.ID, at, fmt.Sprintf("child-%d", variant), "")
	if err != nil {
		t.Fatalf("fork child at %d: %v", at, err)
	}
	submit(ch.ID, p.childCmds)
	run(ch.ID, p.childRun)
	out.ch = ch
	if p.grand {
		chh, _ := svc.HeadTick(ctx, ch.ID)
		gat := ch.ForkTick + sim.Tick(int64(chh-ch.ForkTick)*p.grandFrac/100)
		gc, err := svc.ForkBranch(ctx, ch.ID, gat, fmt.Sprintf("grand-%d", variant), "")
		if err != nil {
			t.Fatalf("fork grandchild at %d: %v", gat, err)
		}
		submit(gc.ID, p.grandCmds)
		run(gc.ID, p.grandRun)
		out.gc, out.hasGrand = gc, true
	}
	return out
}

// dnNorm marshals a DiffResult with branch ids replaced by A/B and the world
// id by W. Ids are replaced as whole JSON strings only.
func dnNorm(t *rapid.T, r app.DiffResult, a, b app.BranchID, w app.WorldID) string {
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	s = strings.ReplaceAll(s, `"`+string(a)+`"`, `"A"`)
	s = strings.ReplaceAll(s, `"`+string(b)+`"`, `"B"`)
	s = strings.ReplaceAll(s, `"`+string(w)+`"`, `"W"`)
	return s
}

type dnPair struct {
	name string
	a, b func(dnWorld) app.Branch
}

func dnPairs(w dnWorld) []dnPair {
	ps := []dnPair{
		{"root-child", func(x dnWorld) app.Branch { return x.root }, func(x dnWorld) app.Branch { return x.ch }},
		{"child-root", func(x dnWorld) app.Branch { return x.ch }, func(x dnWorld) app.Branch { return x.root }},
	}
	if w.hasGrand {
		ps = append(ps,
			dnPair{"root-grand", func(x dnWorld) app.Branch { return x.root }, func(x dnWorld) app.Branch { return x.gc }},
			dnPair{"child-grand", func(x dnWorld) app.Branch { return x.ch }, func(x dnWorld) app.Branch { return x.gc }},
		)
	}
	return ps
}

// TestPropDiffInputDeterminism checks that a Branch_Diff depends only on the
// two Branches' recorded Inputs, seed and Scenario: two independent builds
// (other ids, clocks, names, metadata) and a cold service over the same store
// give byte-identical results after normalising ids. It also checks that the
// first divergence is never earlier than the earliest differing Input.
//
// Feature: parallel-simulation-mvp, Property 26: Diff input determinism
//
// **Validates: Requirements 23.11**
func TestPropDiffInputDeterminism(t *testing.T) {
	data, err := os.ReadFile(simtest.FlagshipPath())
	if err != nil {
		t.Fatal(err)
	}
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		p := dnDrawPlan(t)
		w1 := dnBuild(t, data, p, 0)
		w2 := dnBuild(t, data, p, 1)
		cold := app.NewBranchService(w1.st, nil, nil)
		for _, pr := range dnPairs(w1) {
			a1, b1 := pr.a(w1), pr.b(w1)
			a2, b2 := pr.a(w2), pr.b(w2)
			d1, err := w1.svc.Diff(ctx, a1.ID, b1.ID, nil)
			if err != nil {
				t.Fatalf("%s diff 1: %v", pr.name, err)
			}
			d2, err := w2.svc.Diff(ctx, a2.ID, b2.ID, nil)
			if err != nil {
				t.Fatalf("%s diff 2: %v", pr.name, err)
			}
			dc, err := cold.Diff(ctx, a1.ID, b1.ID, nil)
			if err != nil {
				t.Fatalf("%s cold diff: %v", pr.name, err)
			}
			n1 := dnNorm(t, d1, a1.ID, b1.ID, w1.worldID)
			n2 := dnNorm(t, d2, a2.ID, b2.ID, w2.worldID)
			nc := dnNorm(t, dc, a1.ID, b1.ID, w1.worldID)
			if n1 != n2 {
				t.Fatalf("%s: independent builds differ:\n%s\n%s", pr.name, n1, n2)
			}
			if n1 != nc {
				t.Fatalf("%s: cold service differs from warm:\n%s\n%s", pr.name, n1, nc)
			}
			// No early divergence: the first differing hash cannot precede
			// the earliest differing Input's effective tick.
			if d1.FirstDivergenceTick != nil && len(d1.DifferingInputs) > 0 {
				earliest := d1.DifferingInputs[0].EffectiveTick // sorted by tick
				if *d1.FirstDivergenceTick < earliest {
					t.Fatalf("%s: divergence at %d precedes earliest differing input at %d",
						pr.name, *d1.FirstDivergenceTick, earliest)
				}
			}
		}
	})
}
