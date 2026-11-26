package app_test

import (
	"context"
	"os"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

type piPlan map[sim.Tick][]sim.Command

func piScenario(t testing.TB) (sim.Scenario, []byte) {
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := sim.ParseScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	return *sc, data
}

func piNewBranch(t interface{ Fatal(args ...any) }, data []byte) (*app.BranchService, app.BranchID) {
	svc := app.NewBranchService(memory.New(), nil, nil)
	_, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{Name: "w", ScenarioJSON: data, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	return svc, b.ID
}

// piDrawPlan places valid commands at a random subset of Decision_Window Ticks.
func piDrawPlan(t *rapid.T, sc sim.Scenario) piPlan {
	plan := piPlan{}
	for tick := sim.Tick(0); int64(tick) < sc.HorizonTicks; tick++ {
		if open, _ := sim.DecisionWindowOpen(sc, tick); !open {
			continue
		}
		if !rapid.Bool().Draw(t, "hasCmds") {
			continue
		}
		n := rapid.IntRange(1, 2).Draw(t, "ncmds")
		var cmds []sim.Command
		for i := 0; i < n; i++ {
			sku := rapid.IntRange(0, len(sc.SKUs)-1).Draw(t, "sku")
			if rapid.Bool().Draw(t, "isPO") {
				cmds = append(cmds, sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: int64(sku),
					Role: sim.SupplierPrimary, Qty: rapid.Int64Range(1, 50).Draw(t, "qty")})
			} else {
				lo, hi := sim.PriceBounds(sc.SKUs[sku].ReferencePriceCents)
				cmds = append(cmds, sim.Command{Kind: sim.CmdSetPrice, SKU: int64(sku),
					AmountCents: sim.Cents(rapid.Int64Range(int64(lo), int64(hi)).Draw(t, "price"))})
			}
		}
		plan[tick] = cmds
	}
	return plan
}

// piSubmit submits the plan's Commands for the window at head, once.
func piSubmit(ctx context.Context, t *rapid.T, svc *app.BranchService, id app.BranchID, plan piPlan, head sim.Tick) bool {
	cmds, ok := plan[head]
	if !ok {
		return false
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: id, WindowTick: head, Commands: cmds}); err != nil {
		t.Fatalf("submit at %d: %v", head, err)
	}
	return true
}

// TestPropPresentationIndependence: how a Branch is advanced and inspected
// (Advance with or without Run_To_Tick, pausing, rewind reads, ending windows)
// never changes any State_Hash.
//
// Feature: parallel-simulation-mvp, Property 7: Presentation independence
// Validates: Requirements 33.10 (INV-18)
func TestPropPresentationIndependence(t *testing.T) {
	sc, data := piScenario(t)
	horizon := sim.Tick(sc.HorizonTicks)
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		plan := piDrawPlan(t, sc)

		// Reference: plain Advance to every window, plan submitted there.
		ref, refID := piNewBranch(t, data)
		for {
			head, err := ref.HeadTick(ctx, refID)
			if err != nil {
				t.Fatal(err)
			}
			piSubmit(ctx, t, ref, refID, plan, head)
			if head >= horizon {
				break
			}
			if _, err := ref.Advance(ctx, app.AdvanceRequest{Branch: refID}); err != nil {
				t.Fatalf("reference advance from %d: %v", head, err)
			}
			if h, _ := ref.HeadTick(ctx, refID); h >= horizon {
				break
			}
		}
		refHashes := make([][32]byte, horizon+1)
		for tick := sim.Tick(0); tick <= horizon; tick++ {
			h, err := ref.HashAt(ctx, refID, tick)
			if err != nil {
				t.Fatalf("reference hash %d: %v", tick, err)
			}
			refHashes[tick] = h
		}

		// Variant: random granularity, Run_To_Tick never skipping a planned window.
		svc, id := piNewBranch(t, data)
		stateReads := 0
		for {
			head, err := svc.HeadTick(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if head >= horizon {
				break
			}
			if piSubmit(ctx, t, svc, id, plan, head) && rapid.Bool().Draw(t, "endWindow") {
				if err := svc.EndDecisionWindow(ctx, id, head); err != nil {
					t.Fatalf("end window %d: %v", head, err)
				}
			}
			req := app.AdvanceRequest{Branch: id}
			if rapid.Bool().Draw(t, "useExpected") {
				req.ExpectedHeadTick = &head
			}
			if rapid.Bool().Draw(t, "runTo") {
				limit := horizon
				for tick := head + 1; tick < horizon; tick++ {
					if _, ok := plan[tick]; ok {
						limit = tick
						break
					}
				}
				target := head + 1 + sim.Tick(rapid.Int64Range(0, int64(limit-head-1)).Draw(t, "runToDelta"))
				req.RunToTick = &target
			}
			res, err := svc.Advance(ctx, req)
			if err != nil {
				t.Fatalf("advance from %d (runTo %v): %v", head, req.RunToTick, err)
			}
			if res.Hash != refHashes[res.HeadTick] {
				t.Fatalf("advance result hash at %d differs from reference", res.HeadTick)
			}
			// Rewind reads between advances.
			for i, n := 0, rapid.IntRange(0, 3).Draw(t, "reads"); i < n; i++ {
				at := sim.Tick(rapid.Int64Range(0, int64(res.HeadTick)).Draw(t, "readTick"))
				f, err := svc.FrameAt(ctx, id, at)
				if err != nil || f.Hash != refHashes[at] {
					t.Fatalf("FrameAt %d: err %v, hash match %v", at, err, err == nil && f.Hash == refHashes[at])
				}
				if stateReads < 2 && rapid.Bool().Draw(t, "stateRead") {
					stateReads++
					st, err := svc.StateAt(ctx, id, at)
					if err != nil || sim.Hash(st) != refHashes[at] {
						t.Fatalf("StateAt %d: err %v", at, err)
					}
				}
			}
			if h, _ := svc.HeadTick(ctx, id); h != res.HeadTick {
				t.Fatalf("rewind reads moved head: %d -> %d", res.HeadTick, h)
			}
		}

		head, _ := svc.HeadTick(ctx, id)
		if head != horizon {
			t.Fatalf("variant head %d, want %d", head, horizon)
		}
		for tick := sim.Tick(0); tick <= head; tick++ {
			h, err := svc.HashAt(ctx, id, tick)
			if err != nil || h != refHashes[tick] {
				t.Fatalf("hash at tick %d differs from reference (err %v)", tick, err)
			}
		}
	})
}
