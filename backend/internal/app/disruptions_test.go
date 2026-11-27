package app_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func absence(branch app.BranchID, start sim.Tick) app.InjectRequest {
	return app.InjectRequest{
		Branch: branch, Kind: sim.DisruptionWorkforceAbsence, StartTick: start,
		DurationTicks: 6, MagnitudeBps: 3000, Target: -1,
	}
}

func TestInjectDisruptionAppendsOneInputAndOpensWindow(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	res, err := svc.InjectDisruption(ctx, absence(b.ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	rec := res.Input
	if res.Duplicate || rec.Kind != sim.InputDisruption || rec.EffectiveTick != 10 || rec.Metadata.Provenance != app.ProvInjectedDisruptionLog {
		t.Fatalf("record = %+v", res)
	}
	if recs, _ := st.InputsForBranch(ctx, b.ID, 200); len(recs) != 1 {
		t.Fatalf("stored inputs = %d, want 1", len(recs))
	}
	// Tick 10 is not a cadence tick; Advance must pause there for the onset.
	var ticks []sim.Tick
	for i := 0; i < 5; i++ {
		a, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID})
		if err != nil {
			t.Fatal(err)
		}
		ticks = append(ticks, a.HeadTick)
		if a.HeadTick == 10 {
			if a.Paused != app.PauseWindow || len(a.WindowReasons) != 1 || a.WindowReasons[0] != sim.WindowDisruptionOnset {
				t.Fatalf("tick 10 pause = %q %v", a.Paused, a.WindowReasons)
			}
			break
		}
	}
	if ticks[len(ticks)-1] != 10 {
		t.Fatalf("pauses = %v, want to stop at 10", ticks)
	}
	// Commands may be submitted in the onset window.
	obs, err := svc.ObservationAt(ctx, b.ID, 10)
	if err != nil || !obs.Window.Open {
		t.Fatalf("observation at onset = %+v err %v", obs.Window, err)
	}
	// Rewind reads stay consistent: tick 9 has no onset window and no disruption.
	o9, err := svc.ObservationAt(ctx, b.ID, 9)
	if err != nil || o9.Window.Open || len(o9.Disruptions) != 0 {
		t.Fatalf("observation at 9 = %+v err %v", o9, err)
	}
	// The cold replay agrees on every hash.
	cold := app.NewBranchService(st, nil, nil)
	for tk := sim.Tick(0); tk <= 10; tk++ {
		h1, _ := svc.HashAt(ctx, b.ID, tk)
		h2, err := cold.HashAt(ctx, b.ID, tk)
		if err != nil || h1 != h2 {
			t.Fatalf("hash differs at %d: err %v", tk, err)
		}
	}
	// The injection changes the world compared with no injection.
	base, _, bb := newSvc(t)
	if _, err := base.Advance(ctx, app.AdvanceRequest{Branch: bb.ID, RunToTick: tickp(12)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, RunToTick: tickp(12)}); err != nil {
		t.Fatal(err)
	}
	hb, _ := base.HashAt(ctx, bb.ID, 12)
	hi, _ := svc.HashAt(ctx, b.ID, 12)
	if hb == hi {
		t.Error("injection did not change the state hash")
	}
}

func TestInjectDisruptionRejections(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil { // head 4
		t.Fatal(err)
	}
	for _, start := range []sim.Tick{0, 3, 4} {
		if _, err := svc.InjectDisruption(ctx, absence(b.ID, start)); !errors.Is(err, app.ErrHistoryImmutable) {
			t.Errorf("start %d err = %v, want history-immutable", start, err)
		}
	}
	bad := absence(b.ID, 8)
	bad.MagnitudeBps = 10001
	if _, err := svc.InjectDisruption(ctx, bad); !errors.Is(err, sim.ErrBadDisruption) || !strings.Contains(err.Error(), "0..10000") {
		t.Errorf("magnitude err = %v", err)
	}
	bad = absence(b.ID, 8)
	bad.DurationTicks = 0
	if _, err := svc.InjectDisruption(ctx, bad); !errors.Is(err, sim.ErrBadDisruption) || !strings.Contains(err.Error(), "1..120") {
		t.Errorf("duration err = %v", err)
	}
	bad = absence(b.ID, 121)
	if _, err := svc.InjectDisruption(ctx, bad); !errors.Is(err, sim.ErrBadDisruption) {
		t.Errorf("start beyond horizon err = %v", err)
	}
	stale := absence(b.ID, 8)
	stale.ExpectedHeadTick = tickp(2)
	if _, err := svc.InjectDisruption(ctx, stale); !errors.Is(err, app.ErrStaleHead) {
		t.Errorf("stale err = %v", err)
	}
	if recs, _ := st.InputsForBranch(ctx, b.ID, 200); len(recs) != 0 {
		t.Fatalf("rejected injections stored %d inputs", len(recs))
	}
}

func TestInjectDisruptionIdempotent(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	req := absence(b.ID, 10)
	req.IdempotencyKey = "k"
	first, err := svc.InjectDisruption(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.InjectDisruption(ctx, req)
	if err != nil || !again.Duplicate || again.Input.Seq != first.Input.Seq {
		t.Fatalf("duplicate = %+v err %v", again, err)
	}
	if recs, _ := st.InputsForBranch(ctx, b.ID, 200); len(recs) != 1 {
		t.Fatalf("stored inputs = %d, want 1", len(recs))
	}
}

// A disruption injected for the future must not show in any Observation
// before its onset Tick (hidden information).
func TestInjectDisruptionHiddenBeforeOnset(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.InjectDisruption(ctx, absence(b.ID, 10)); err != nil {
		t.Fatal(err)
	}
	for {
		head, _ := svc.HeadTick(ctx, b.ID)
		obs, err := svc.ObservationAt(ctx, b.ID, head)
		if err != nil {
			t.Fatal(err)
		}
		if head < 10 {
			if len(obs.Disruptions) != 0 {
				t.Fatalf("tick %d shows a disruption before its onset: %+v", head, obs.Disruptions)
			}
		} else {
			if len(obs.Disruptions) != 1 || obs.Disruptions[0].Origin != sim.OriginInjected || obs.Disruptions[0].OnsetTick != 10 {
				t.Fatalf("tick %d disruptions = %+v", head, obs.Disruptions)
			}
			return
		}
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInjectDisruptionOnForkedChildIsIndependent(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, RunToTick: tickp(4)}); err != nil {
		t.Fatal(err)
	}
	child, err := svc.ForkBranch(ctx, b.ID, 4, "c", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InjectDisruption(ctx, absence(child.ID, 6)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []app.BranchID{b.ID, child.ID} {
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id, RunToTick: tickp(8)}); err != nil {
			t.Fatal(err)
		}
	}
	hp, _ := svc.HashAt(ctx, b.ID, 8)
	hc, _ := svc.HashAt(ctx, child.ID, 8)
	if hp == hc {
		t.Error("child injection left the hash unchanged")
	}
	hp5, _ := svc.HashAt(ctx, b.ID, 5)
	hc5, _ := svc.HashAt(ctx, child.ID, 5)
	if hp5 != hc5 {
		t.Error("hashes differ before the injection onset")
	}
}

func TestEndDecisionWindowStaleHead(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	if err := svc.EndDecisionWindow(ctx, b.ID, 0, tickp(3)); !errors.Is(err, app.ErrStaleHead) {
		t.Fatalf("stale end err = %v", err)
	}
	if err := svc.EndDecisionWindow(ctx, b.ID, 0, tickp(0)); err != nil {
		t.Fatalf("matching end err = %v", err)
	}
}

func TestSubmitDuplicateReportsRealBudget(t *testing.T) {
	svc, _, b := newSvc(t)
	ctx := context.Background()
	req := app.SubmitRequest{
		Branch: b.ID, WindowTick: 0, IdempotencyKey: "dup",
		Commands: []sim.Command{
			{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 1},
			{Kind: sim.CmdPlacePurchaseOrder, SKU: 1, Role: sim.SupplierPrimary, Qty: 1},
		},
	}
	first, err := svc.SubmitCommands(ctx, req)
	if err != nil || len(first.Accepted) != 2 {
		t.Fatalf("first = %+v err %v", first, err)
	}
	dup, err := svc.SubmitCommands(ctx, req)
	if err != nil || !dup.Duplicate {
		t.Fatalf("dup = %+v err %v", dup, err)
	}
	if dup.BudgetRemaining != first.BudgetRemaining || dup.BudgetRemaining == 0 {
		t.Fatalf("duplicate budget = %d, fresh = %d", dup.BudgetRemaining, first.BudgetRemaining)
	}
}

// Concurrent calls on one branch are serialised; whatever order they run in,
// the resulting hashes equal a sequential replay of the stored input log.
func TestConcurrentBranchCalls(t *testing.T) {
	svc, st, b := newSvc(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	cmd := []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 1}}
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				head, err := svc.HeadTick(ctx, b.ID)
				if err != nil {
					t.Errorf("head: %v", err)
					return
				}
				switch (g + i) % 5 {
				case 0:
					_, err = svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID})
				case 1:
					_, err = svc.SubmitCommands(ctx, app.SubmitRequest{Branch: b.ID, WindowTick: head, Commands: cmd})
				case 2:
					err = svc.EndDecisionWindow(ctx, b.ID, head, nil)
				case 3:
					_, err = svc.ForkBranch(ctx, b.ID, head/2, "f", "")
				case 4:
					_, err = svc.InjectDisruption(ctx, absence(b.ID, head+1+sim.Tick(g)))
				}
				if err != nil && !expectedConcurrentErr(err) {
					t.Errorf("goroutine %d op %d: unexpected error %v", g, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	head, err := svc.HeadTick(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	cold := app.NewBranchService(st, nil, nil)
	for tk := sim.Tick(0); tk <= head; tk++ {
		h1, err1 := svc.HashAt(ctx, b.ID, tk)
		h2, err2 := cold.HashAt(ctx, b.ID, tk)
		if err1 != nil || err2 != nil || h1 != h2 {
			t.Fatalf("tick %d: warm/cold hash mismatch (%v, %v)", tk, err1, err2)
		}
	}
	bs, _ := svc.ListBranches(ctx, b.WorldID)
	for _, br := range bs {
		h, err := cold.HeadTick(ctx, br.ID)
		if err != nil || h != br.HeadTick {
			t.Fatalf("branch %s head %d vs stored %d err %v", br.ID, h, br.HeadTick, err)
		}
	}
}

func expectedConcurrentErr(err error) bool {
	for _, target := range []error{
		app.ErrHistoryImmutable, app.ErrNotYetSimulated, app.ErrWindowNotOpen, app.ErrBudgetExceeded,
		app.ErrHorizonReached, app.ErrBranchLimit, app.ErrStaleHead, sim.ErrBadDisruption,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func tickp(t sim.Tick) *sim.Tick { return &t }
