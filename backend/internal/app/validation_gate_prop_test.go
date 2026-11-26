package app_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// vgMaxWindows bounds the decision windows exercised per generated case.
const vgMaxWindows = 4

// vgCommand draws a Command that is valid, invalid or unimplemented: bad SKU,
// out-of-bounds price, bad quantity, over-cash quantity, unknown kind or role.
func vgCommand(nSKU int64) *rapid.Generator[sim.Command] {
	return rapid.Custom(func(t *rapid.T) sim.Command {
		c := sim.Command{
			Kind: sim.CommandKind(rapid.IntRange(0, 7).Draw(t, "kind")), // 6, 7 are unknown kinds
			SKU:  rapid.Int64Range(-1, nSKU+1).Draw(t, "sku"),           // -1 and nSKU, nSKU+1 are bad
			Role: uint8(rapid.IntRange(0, 2).Draw(t, "role")),
		}
		switch rapid.IntRange(0, 3).Draw(t, "qtyClass") {
		case 0:
			c.Qty = rapid.Int64Range(-3, 3).Draw(t, "qtySmall")
		case 1:
			c.Qty = rapid.Int64Range(1, 500).Draw(t, "qtyMid")
		case 2:
			c.Qty = rapid.Int64Range(100_000, 1_200_000).Draw(t, "qtyHuge") // over cash / over max
		default:
			c.Qty = rapid.Int64Range(1, 5000).Draw(t, "qtyBig")
		}
		switch rapid.IntRange(0, 2).Draw(t, "priceClass") {
		case 0:
			c.AmountCents = sim.Cents(rapid.Int64Range(-100, 100).Draw(t, "priceLow"))
		case 1:
			c.AmountCents = sim.Cents(rapid.Int64Range(100, 20_000).Draw(t, "priceMid"))
		default:
			c.AmountCents = sim.Cents(rapid.Int64Range(1, 1_000_000).Draw(t, "priceWide"))
		}
		return c
	})
}

type vgEnv struct {
	svc    *app.BranchService
	st     *memory.Store
	branch app.Branch
	sc     *sim.Scenario
}

func vgNew(t *rapid.T, seed sim.Seed) vgEnv {
	data, err := os.ReadFile(simtest.FlagshipPath())
	if err != nil {
		t.Fatalf("read flagship: %v", err)
	}
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil)
	_, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{Name: "vg", ScenarioJSON: data, Seed: seed})
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	return vgEnv{svc: svc, st: st, branch: b, sc: simtest.MustFlagship()}
}

func (e vgEnv) inputCount(t *rapid.T) int {
	recs, err := e.st.InputsForBranch(context.Background(), e.branch.ID, 1<<30)
	if err != nil {
		t.Fatalf("inputs: %v", err)
	}
	return len(recs)
}

func (e vgEnv) headHash(t *rapid.T) (sim.Tick, [32]byte) {
	ctx := context.Background()
	h, err := e.svc.HeadTick(ctx, e.branch.ID)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	hash, err := e.svc.HashAt(ctx, e.branch.ID, h)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return h, hash
}

// Feature: parallel-simulation-mvp, Property 22: Validation gate and validator determinism
// **Validates: Requirements 17.4, 17.5, 17.6**
func TestPropValidationGate(t *testing.T) {
	ctx := context.Background()
	rapid.Check(t, func(t *rapid.T) {
		seed := sim.Seed(rapid.Uint64Range(1, 1<<40).Draw(t, "seed"))
		e := vgNew(t, seed)
		nSKU := int64(len(e.sc.SKUs))
		batchGen := rapid.SliceOfN(vgCommand(nSKU), 0, int(e.sc.CommandBudget)+3)
		windows := rapid.IntRange(1, vgMaxWindows).Draw(t, "windows")

		for w := 0; w < windows; w++ {
			head, _ := e.headHash(t)
			obs, err := e.svc.ObservationAt(ctx, e.branch.ID, head)
			if err != nil {
				t.Fatalf("observation: %v", err)
			}
			if !obs.Window.Open {
				t.Fatalf("tick %d: advance stopped without an open window", head)
			}
			batch := batchGen.Draw(t, "batch")
			before := e.inputCount(t)

			// (2) Determinism, and the validator never alters a Command.
			r1 := sim.Validate(*e.sc, obs, batch, e.sc.CommandBudget)
			r2 := sim.Validate(*e.sc, obs, batch, e.sc.CommandBudget)
			if !reflect.DeepEqual(r1, r2) {
				t.Fatalf("validator not deterministic:\n%+v\n%+v", r1, r2)
			}
			var wantOK []sim.Command
			for i, r := range r1 {
				if !reflect.DeepEqual(r.Command, batch[i]) {
					t.Fatalf("validator altered command %d: %+v -> %+v", i, batch[i], r.Command)
				}
				if r.OK {
					wantOK = append(wantOK, r.Command)
				} else if r.Code == "" {
					t.Fatalf("rejected command %d has no reason code", i)
				}
			}

			// The service gate must agree with the validator.
			res, err := e.svc.SubmitCommands(ctx, app.SubmitRequest{Branch: e.branch.ID, WindowTick: head, Commands: batch})
			if err != nil {
				if len(batch) == 0 || !errors.Is(err, app.ErrBudgetExceeded) {
					t.Fatalf("submit: %v", err)
				}
			}
			if len(res.Accepted) != len(wantOK) || len(res.Rejected) != len(batch)-len(wantOK) {
				t.Fatalf("service accepted %d rejected %d, validator ok %d of %d", len(res.Accepted), len(res.Rejected), len(wantOK), len(batch))
			}
			for i, rec := range res.Accepted {
				got, err := sim.DecodeCommand(rec.Payload)
				if err != nil || got != wantOK[i] {
					t.Fatalf("stored command %d = %+v (err %v), want %+v", i, got, err, wantOK[i])
				}
			}
			for _, r := range res.Rejected {
				if r.OK {
					t.Fatalf("rejected list holds an OK result: %+v", r)
				}
			}
			if after := e.inputCount(t); after != before+len(res.Accepted) {
				t.Fatalf("input count %d -> %d with %d accepted (a rejected command was stored)", before, after, len(res.Accepted))
			}

			// (3) A batch of only invalid commands changes neither hash nor log.
			hTick, hHash := e.headHash(t)
			countBefore := e.inputCount(t)
			bad := []sim.Command{
				{Kind: sim.CmdPlacePurchaseOrder, SKU: nSKU + 5, Qty: 1},
				{Kind: sim.CmdSetPrice, SKU: 0, AmountCents: 1},
				{Kind: sim.CommandKind(200)},
				{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: sim.MaxOrderQuantity + 1},
			}
			badRes, err := e.svc.SubmitCommands(ctx, app.SubmitRequest{Branch: e.branch.ID, WindowTick: head, Commands: bad})
			if err != nil && !errors.Is(err, app.ErrBudgetExceeded) {
				t.Fatalf("submit invalid batch: %v", err)
			}
			if len(badRes.Accepted) != 0 {
				t.Fatalf("invalid batch accepted: %+v", badRes.Accepted)
			}
			if err == nil && len(badRes.Rejected) != len(bad) {
				t.Fatalf("invalid batch rejected %d of %d", len(badRes.Rejected), len(bad))
			}
			hTick2, hHash2 := e.headHash(t)
			if hTick2 != hTick || hHash2 != hHash || e.inputCount(t) != countBefore {
				t.Fatalf("rejected commands changed head (%d,%x)->(%d,%x) or input count %d->%d", hTick, hHash[:4], hTick2, hHash2[:4], countBefore, e.inputCount(t))
			}

			if _, err := e.svc.Advance(ctx, app.AdvanceRequest{Branch: e.branch.ID}); err != nil {
				if errors.Is(err, app.ErrHorizonReached) {
					break
				}
				t.Fatalf("advance: %v", err)
			}
		}

		// (1) Every stored command re-validates OK against the Observation at its
		// window, in stored order with the full window budget.
		recs, err := e.st.InputsForBranch(ctx, e.branch.ID, 1<<30)
		if err != nil {
			t.Fatalf("inputs: %v", err)
		}
		byWindow := map[sim.Tick][]sim.Command{}
		var order []sim.Tick
		for _, r := range recs {
			if r.Kind != sim.InputCommand {
				continue
			}
			c, err := sim.DecodeCommand(r.Payload)
			if err != nil {
				t.Fatalf("decode stored command: %v", err)
			}
			wt := r.EffectiveTick - 1
			if _, ok := byWindow[wt]; !ok {
				order = append(order, wt)
			}
			byWindow[wt] = append(byWindow[wt], c)
		}
		for _, wt := range order {
			obs, err := e.svc.ObservationAt(ctx, e.branch.ID, wt)
			if err != nil {
				t.Fatalf("observation at %d: %v", wt, err)
			}
			for i, r := range sim.Validate(*e.sc, obs, byWindow[wt], e.sc.CommandBudget) {
				if !r.OK {
					t.Fatalf("stored command %d of window %d fails re-validation: %+v", i, wt, r)
				}
			}
		}
	})
}
