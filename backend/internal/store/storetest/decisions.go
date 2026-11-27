package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// RunDecisions is the contract of app.DecisionRepo, run against stores built
// by f. The store must implement app.DecisionRepo.
func RunDecisions(t *testing.T, f Factory) {
	ctx := context.Background()
	rec := func(branch app.BranchID, tick sim.Tick) app.DecisionRecord {
		return app.DecisionRecord{
			BranchID: branch, WindowTick: tick, ManagerKind: app.ManagerAILLM,
			Provenance: app.ProvAIRuleBasedFallback, Model: "m", FallbackReason: "timeout", Rationale: "r <b>x</b>",
			Proposed: []sim.Command{
				{Kind: sim.CmdPlacePurchaseOrder, SKU: 1, Role: sim.SupplierBackup, Qty: 40},
				{Kind: sim.CmdSetPrice, SKU: 0, AmountCents: 1999},
			},
			AcceptedCount:   1,
			LLMCallsCharged: 1, PromptTokens: 120, CompletionTokens: 45,
			Rejected:  []app.RejectedProposal{{Index: 1, Command: sim.Command{Kind: sim.CmdSetPrice, SKU: 0, AmountCents: 1999}, Code: "price_out_of_bounds", Message: "m"}},
			CreatedAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
		}
	}

	t.Run("decisions round trip in window order", func(t *testing.T) {
		s := f(t)
		d := s.(app.DecisionRepo)
		seed(t, s, "w1", app.Branch{ID: "a", ManagerKind: app.ManagerAILLM}, app.Branch{ID: "b", ManagerKind: app.ManagerAILLM})
		for _, tick := range []sim.Tick{8, 0, 4} {
			if err := d.AppendDecision(ctx, rec("a", tick)); err != nil {
				t.Fatal(err)
			}
		}
		empty := rec("a", 12)
		empty.Proposed, empty.Rejected, empty.AcceptedCount = nil, nil, 0
		empty.Provenance, empty.FallbackReason, empty.Model, empty.Rationale = app.ProvAIRuleBased, "", "", ""
		empty.ManagerKind = app.ManagerAIRuleBased
		if err := d.AppendDecision(ctx, empty); err != nil {
			t.Fatal(err)
		}
		got, err := d.DecisionsForBranch(ctx, "a")
		if err != nil || len(got) != 4 {
			t.Fatalf("got %d records, err %v", len(got), err)
		}
		for i, tick := range []sim.Tick{0, 4, 8, 12} {
			if got[i].WindowTick != tick {
				t.Fatalf("record %d tick %d, want %d", i, got[i].WindowTick, tick)
			}
		}
		want := rec("a", 0)
		if !reflect.DeepEqual(got[0], want) {
			t.Fatalf("round trip differs:\n got %+v\nwant %+v", got[0], want)
		}
		if got[3].Provenance != app.ProvAIRuleBased || len(got[3].Proposed) != 0 || got[3].AcceptedCount != 0 || len(got[3].Rejected) != 0 {
			t.Fatalf("zero-command record = %+v", got[3])
		}
		other, err := d.DecisionsForBranch(ctx, "b")
		if err != nil || len(other) != 0 {
			t.Fatalf("branch b records %d err %v", len(other), err)
		}
	})

	t.Run("decisions are append-only per window", func(t *testing.T) {
		s := f(t)
		d := s.(app.DecisionRepo)
		seed(t, s, "w1", app.Branch{ID: "a", ManagerKind: app.ManagerAILLM})
		if err := d.AppendDecision(ctx, rec("a", 4)); err != nil {
			t.Fatal(err)
		}
		changed := rec("a", 4)
		changed.Rationale = "rewritten"
		if err := d.AppendDecision(ctx, changed); !errors.Is(err, app.ErrAlreadyExists) {
			t.Fatalf("second record for one window: %v", err)
		}
		got, _ := d.DecisionsForBranch(ctx, "a")
		if len(got) != 1 || got[0].Rationale != "r <b>x</b>" {
			t.Fatalf("record was altered: %+v", got)
		}
		if err := d.AppendDecision(ctx, rec("missing", 4)); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("unknown branch: %v", err)
		}
	})
}
