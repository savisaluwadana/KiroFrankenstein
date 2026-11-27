package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
)

// RunDuels is the contract of app.DuelRepo, run against stores built by f.
// The store must implement app.DuelRepo.
func RunDuels(t *testing.T, f Factory) {
	ctx := context.Background()
	duel := func(id app.DuelID) app.Duel {
		return app.Duel{
			ID: id, WorldID: "w1", ParentBranchID: "root", HumanBranchID: "h", AIBranchID: "a",
			AIKind: app.ManagerAIRuleBased, ForkTick: 24, ComparisonPolicy: app.ComparisonPolicyMinHead,
			CreatedAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
		}
	}
	setup := func(t *testing.T) app.DuelRepo {
		s := f(t)
		seed(t, s, "w1", app.Branch{ID: "root"}, app.Branch{ID: "h"}, app.Branch{ID: "a", ManagerKind: app.ManagerAIRuleBased})
		return s.(app.DuelRepo)
	}

	t.Run("duel round trips", func(t *testing.T) {
		r := setup(t)
		if err := r.CreateDuel(ctx, duel("d1")); err != nil {
			t.Fatal(err)
		}
		got, err := r.GetDuel(ctx, "d1")
		if err != nil || !reflect.DeepEqual(got, duel("d1")) {
			t.Fatalf("got %+v err %v", got, err)
		}
	})

	t.Run("duels are create-only and checked", func(t *testing.T) {
		r := setup(t)
		if err := r.CreateDuel(ctx, duel("d1")); err != nil {
			t.Fatal(err)
		}
		if err := r.CreateDuel(ctx, duel("d1")); !errors.Is(err, app.ErrAlreadyExists) {
			t.Fatalf("duplicate: %v", err)
		}
		if _, err := r.GetDuel(ctx, "missing"); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("unknown duel: %v", err)
		}
		bad := duel("d2")
		bad.AIBranchID = "ghost"
		if err := r.CreateDuel(ctx, bad); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("unknown branch: %v", err)
		}
	})
}
