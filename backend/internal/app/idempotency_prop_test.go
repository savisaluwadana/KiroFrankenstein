package app_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// ikCmd draws a valid purchase-order command from a small set so that
// different keys often carry identical commands.
func ikCmd(t *rapid.T, label string) sim.Command {
	return sim.Command{
		Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary,
		Qty: int64(rapid.IntRange(1, 3).Draw(t, label)),
	}
}

// Feature: parallel-simulation-mvp, Property 11: Input idempotency key
//
// Validates: Requirements 13.4 (INV-3)
func TestPropIdempotencyKey(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		svc, st, b := newSvc(t)
		ctx := context.Background()
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil { // head 4
			rt.Fatal(err)
		}
		const window = sim.Tick(4)

		// Command budget is 5: one two-command key plus up to three single keys.
		nSingle := rapid.IntRange(1, 3).Draw(rt, "singles")
		type sub struct {
			key  string
			cmds []sim.Command
		}
		subs := []sub{{"ik-multi", []sim.Command{ikCmd(rt, "m0"), ikCmd(rt, "m1")}}}
		for i := 0; i < nSingle; i++ {
			subs = append(subs, sub{fmt.Sprintf("ik-%d", i), []sim.Command{ikCmd(rt, fmt.Sprintf("c%d", i))}})
		}
		// Make two keys carry identical commands.
		subs[len(subs)-1].cmds = []sim.Command{subs[0].cmds[0]}

		submit := func(s sub) (app.SubmitResult, error) {
			return svc.SubmitCommands(ctx, app.SubmitRequest{
				Branch: b.ID, WindowTick: window, Commands: s.cmds, IdempotencyKey: s.key,
			})
		}
		inputs := func() []app.InputRecord {
			recs, err := st.InputsForBranch(ctx, b.ID, 1000)
			if err != nil {
				rt.Fatal(err)
			}
			return recs
		}

		originals := make([]app.SubmitResult, len(subs))
		wantCount := 0
		for i, s := range subs {
			r, err := submit(s)
			if err != nil {
				rt.Fatalf("first submit %q: %v", s.key, err)
			}
			if r.Duplicate || len(r.Rejected) != 0 || len(r.Accepted) != len(s.cmds) {
				rt.Fatalf("first submit %q = %+v", s.key, r)
			}
			originals[i] = r
			wantCount += len(s.cmds)
			// Different keys with identical commands are separate inputs.
			if got := len(inputs()); got != wantCount {
				rt.Fatalf("after key %q: %d inputs, want %d", s.key, got, wantCount)
			}
		}

		snapshot := inputs()
		headBefore, err := svc.HeadTick(ctx, b.ID)
		if err != nil {
			rt.Fatal(err)
		}
		hashes := make([][32]byte, headBefore+1)
		for tk := sim.Tick(0); tk <= headBefore; tk++ {
			if hashes[tk], err = svc.HashAt(ctx, b.ID, tk); err != nil {
				rt.Fatal(err)
			}
		}

		// Repeat keys in a random interleaved order, any number of times.
		repeats := rapid.SliceOfN(rapid.IntRange(0, len(subs)-1), 1, 20).Draw(rt, "repeats")
		for _, idx := range repeats {
			r, err := submit(subs[idx])
			if err != nil {
				rt.Fatalf("repeat %q: %v", subs[idx].key, err)
			}
			if !r.Duplicate {
				rt.Fatalf("repeat %q not flagged Duplicate", subs[idx].key)
			}
			if !reflect.DeepEqual(r.Accepted, originals[idx].Accepted) {
				rt.Fatalf("repeat %q accepted = %+v, want %+v", subs[idx].key, r.Accepted, originals[idx].Accepted)
			}
			if got := inputs(); !reflect.DeepEqual(got, snapshot) {
				rt.Fatalf("inputs changed after repeating %q", subs[idx].key)
			}
		}

		if head, _ := svc.HeadTick(ctx, b.ID); head != headBefore {
			rt.Fatalf("head tick %d, want %d", head, headBefore)
		}
		for tk := sim.Tick(0); tk <= headBefore; tk++ {
			if h, _ := svc.HashAt(ctx, b.ID, tk); h != hashes[tk] {
				rt.Fatalf("hash at tick %d changed", tk)
			}
		}

		// A cold service over the same store sees the same inputs and hash.
		cold := app.NewBranchService(st, nil, nil)
		if h, err := cold.HashAt(ctx, b.ID, headBefore); err != nil || h != hashes[headBefore] {
			rt.Fatalf("cold hash differs, err %v", err)
		}
	})
}
