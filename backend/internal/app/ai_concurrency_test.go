package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// gateStub is an OpenAI-compatible endpoint whose answers can be held back,
// to simulate a slow provider.
type gateStub struct {
	srv     *httptest.Server
	calls   atomic.Int64
	arrived chan struct{} // one value per request that reached the stub
	gate    atomic.Pointer[chan struct{}]
	reply   func(n int64) stubReply
}

func newGateStub(t *testing.T, reply func(n int64) stubReply) *gateStub {
	t.Helper()
	g := &gateStub{reply: reply, arrived: make(chan struct{}, 256)}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := g.calls.Add(1)
		_, _ = readAll(r)
		g.arrived <- struct{}{}
		if ch := g.gate.Load(); ch != nil {
			select {
			case <-*ch:
			case <-r.Context().Done():
				return
			}
		}
		rep := g.reply(n)
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": rep.content}}},
			"usage":   map[string]any{"prompt_tokens": rep.prompt, "completion_tokens": rep.completion, "total_tokens": rep.prompt + rep.completion},
		})
		_, _ = w.Write(body)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// hold makes every following request wait; the returned function frees them
// all (and is idempotent).
func (g *gateStub) hold() (release func()) {
	ch := make(chan struct{})
	g.gate.Store(&ch)
	var once sync.Once
	return func() {
		once.Do(func() { g.gate.Store(nil); close(ch) })
	}
}

func (g *gateStub) waitArrived(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-g.arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d provider requests arrived", i, n)
		}
	}
}

// within fails the test if fn takes longer than d.
func within(t *testing.T, d time.Duration, what string, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(d):
		t.Fatalf("%s blocked for more than %s while a provider request was in flight", what, d)
	}
}

func newConcSvc(t *testing.T, cfg llm.Config) *app.BranchService {
	t.Helper()
	return app.NewBranchService(memory.New(), nil, nil, withLLM(t, cfg))
}

func createWorld(t *testing.T, svc *app.BranchService, mgr app.ManagerKind) (app.World, app.Branch) {
	t.Helper()
	w, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{Name: "c", ScenarioJSON: bfJSON(t), Seed: 2026, Manager: mgr})
	if err != nil {
		t.Fatal(err)
	}
	return w, b
}

type advOut struct {
	res app.AdvanceResult
	err error
}

func advanceAsync(svc *app.BranchService, ctx context.Context, id app.BranchID) <-chan advOut {
	ch := make(chan advOut, 1)
	go func() {
		r, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id})
		ch <- advOut{r, err}
	}()
	return ch
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", what)
		panic("unreachable")
	}
}

// coldHash replays the branch from the stored inputs in a fresh service (no
// cache) and returns the head hash: the clean replay of the input log.
func coldHash(t *testing.T, st *memory.Store, id app.BranchID, tick sim.Tick) [32]byte {
	t.Helper()
	fresh := app.NewBranchService(st, nil, nil)
	h, err := fresh.HashAt(context.Background(), id, tick)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// (a) A slow provider request blocks nothing else.
func TestAIConcurrencyNoBlocking(t *testing.T) {
	ctx := context.Background()
	const bound = 500 * time.Millisecond

	reads := func(t *testing.T, svc *app.BranchService, w app.WorldID, b app.BranchID, tick sim.Tick) {
		t.Helper()
		within(t, bound, "GetWorld", func() error { _, _, err := svc.GetWorld(ctx, w); return err })
		within(t, bound, "ListBranches", func() error { _, err := svc.ListBranches(ctx, w); return err })
		within(t, bound, "HashAt", func() error { _, err := svc.HashAt(ctx, b, tick); return err })
		within(t, bound, "FrameAt", func() error { _, err := svc.FrameAt(ctx, b, tick); return err })
		within(t, bound, "StateAt", func() error { _, err := svc.StateAt(ctx, b, 0); return err })
		within(t, bound, "MetricsAt", func() error { _, err := svc.MetricsAt(ctx, b, tick); return err })
		within(t, bound, "HeadTick", func() error { _, err := svc.HeadTick(ctx, b); return err })
		within(t, bound, "DecisionRecords", func() error { _, err := svc.DecisionRecords(ctx, b); return err })
		within(t, bound, "Subscribe", func() error { _, cancel := svc.Subscribe(b); cancel(); return nil })
	}

	t.Run("fork of an LLM branch", func(t *testing.T) {
		stub := newGateStub(t, always(okReply(validProposal)))
		svc := newConcSvc(t, llmCfg(stub.srv.URL))
		wa, ra := createWorld(t, svc, app.ManagerHuman)
		wb, rb := createWorld(t, svc, app.ManagerHuman)
		release := stub.hold()
		defer release()
		forked := make(chan error, 1)
		go func() { _, err := svc.ForkBranch(ctx, ra.ID, 0, "ai", app.ManagerAILLM); forked <- err }()
		stub.waitArrived(t, 1)

		reads(t, svc, wa.ID, ra.ID, 0)
		within(t, bound, "advance of world B", func() error {
			_, err := svc.Advance(ctx, app.AdvanceRequest{Branch: rb.ID})
			return err
		})
		reads(t, svc, wb.ID, rb.ID, 4)
		var child app.Branch
		within(t, bound, "read of the branch being decided", func() error {
			bs, err := svc.ListBranches(ctx, wa.ID)
			child = bs[len(bs)-1]
			return err
		})
		reads(t, svc, wa.ID, child.ID, 0)
		within(t, bound, "advance of a human branch of world A", func() error {
			_, err := svc.Advance(ctx, app.AdvanceRequest{Branch: ra.ID})
			return err
		})

		release()
		if err := recv(t, forked, "fork"); err != nil {
			t.Fatal(err)
		}
		if recs, _ := svc.DecisionRecords(ctx, child.ID); len(recs) != 1 || recs[0].Provenance != app.ProvAILLM {
			t.Fatalf("records = %+v", recs)
		}
	})

	t.Run("advance of an LLM branch", func(t *testing.T) {
		stub := newGateStub(t, always(okReply(validProposal)))
		svc := newConcSvc(t, llmCfg(stub.srv.URL))
		wa, ra := createWorld(t, svc, app.ManagerAILLM) // decided at tick 0: gate open
		_, rb := createWorld(t, svc, app.ManagerHuman)
		stub.waitArrived(t, 1)
		release := stub.hold()
		defer release()
		out := advanceAsync(svc, ctx, ra.ID)
		stub.waitArrived(t, 1)

		// The branch is stored at its window (tick 4) while the request runs.
		reads(t, svc, wa.ID, ra.ID, 4)
		within(t, bound, "advance of world B", func() error {
			_, err := svc.Advance(ctx, app.AdvanceRequest{Branch: rb.ID})
			return err
		})
		release()
		r := recv(t, out, "advance")
		if r.err != nil || r.res.HeadTick != 4 || r.res.Paused != app.PauseWindow || len(r.res.Frames) != 4 {
			t.Fatalf("advance = %+v err %v", r.res, r.err)
		}
	})
}

// (b) Concurrent Advance and decision calls for one window make one request
// and write one record.
func TestAIConcurrencySingleFlight(t *testing.T) {
	ctx := context.Background()
	stub := newGateStub(t, always(okReply(validProposal)))
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil, withLLM(t, llmCfg(stub.srv.URL)))
	_, root := createWorld(t, svc, app.ManagerAILLM)
	stub.waitArrived(t, 1) // window 0

	release := stub.hold()
	defer release()
	first := advanceAsync(svc, ctx, root.ID)
	stub.waitArrived(t, 1) // window 4 is in flight
	second := advanceAsync(svc, ctx, root.ID)

	// Callers that need the same window's decision join the request.
	joined := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { joined <- svc.EnsureDecidedForTest(ctx, root.ID) }()
	}
	time.Sleep(100 * time.Millisecond)
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("provider requests while window 4 is in flight = %d, want 2 (window 0 and 4)", got)
	}
	release()
	for i := 0; i < 3; i++ {
		if err := recv(t, joined, "joined call"); err != nil {
			t.Fatal(err)
		}
	}
	a, b := recv(t, first, "first advance"), recv(t, second, "second advance")
	if a.err != nil || b.err != nil {
		t.Fatalf("errors: %v / %v", a.err, b.err)
	}
	// The second Advance waited its turn: it is a second step from the window.
	if a.res.HeadTick != 4 || b.res.HeadTick != 8 {
		t.Fatalf("heads %d, %d", a.res.HeadTick, b.res.HeadTick)
	}
	recs, _ := svc.DecisionRecords(ctx, root.ID)
	if len(recs) != 3 || stub.calls.Load() != 3 {
		t.Fatalf("records %d, requests %d, want 3 and 3 (windows 0, 4, 8, one each)", len(recs), stub.calls.Load())
	}
	seen := map[sim.Tick]bool{}
	for _, r := range recs {
		if seen[r.WindowTick] {
			t.Fatalf("two records for window %d", r.WindowTick)
		}
		seen[r.WindowTick] = true
	}
	if svc.InflightForTest() != 0 {
		t.Fatal("flight left registered")
	}
	h, _ := svc.HashAt(ctx, root.ID, 8)
	if h != coldHash(t, st, root.ID, 8) {
		t.Fatal("hash differs from a clean replay")
	}
}

// humanSubmit puts one Command in the AI branch's open window, as another
// writer to the same Branch would.
func humanSubmit(t *testing.T, svc *app.BranchService, id app.BranchID, tick sim.Tick, n int) {
	t.Helper()
	cmds := make([]sim.Command, n)
	for i := range cmds {
		cmds[i] = sim.Command{Kind: sim.CmdSetMarketingSpend, SKU: int64(i % 3), AmountCents: sim.Cents(100 * (i + 1))}
	}
	res, err := svc.SubmitCommands(context.Background(), app.SubmitRequest{Branch: id, WindowTick: tick, Commands: cmds})
	if err != nil || len(res.Accepted) != n {
		t.Fatalf("submit: accepted %d of %d, rejected %+v, err %v", len(res.Accepted), n, res.Rejected, err)
	}
}

// (c) An answer that arrives after the Branch changed is discarded.
func TestAIConcurrencyStaleDecision(t *testing.T) {
	ctx := context.Background()

	setup := func(t *testing.T) (*gateStub, *app.BranchService, *memory.Store, app.World, app.Branch) {
		stub := newGateStub(t, always(okReply(validProposal)))
		st := memory.New()
		svc := app.NewBranchService(st, nil, nil, withLLM(t, llmCfg(stub.srv.URL)))
		w, root := createWorld(t, svc, app.ManagerAILLM)
		stub.waitArrived(t, 1)
		return stub, svc, st, w, root
	}
	inputsAt5 := func(st *memory.Store, id app.BranchID) (human, ai int) {
		in, _ := st.InputsForBranch(ctx, id, 5)
		for _, r := range in {
			if r.EffectiveTick != 5 {
				continue
			}
			if r.Metadata.Provenance == app.ProvHuman {
				human++
			} else {
				ai++
			}
		}
		return
	}

	t.Run("one change: retried once with a fresh capture", func(t *testing.T) {
		stub, svc, st, w, root := setup(t)
		release := stub.hold()
		defer release()
		out := advanceAsync(svc, ctx, root.ID)
		stub.waitArrived(t, 1)
		humanSubmit(t, svc, root.ID, 4, 1) // the Branch changes under the request
		release()
		stub.waitArrived(t, 1) // the retry's request
		r := recv(t, out, "advance")
		if r.err != nil || r.res.HeadTick != 4 {
			t.Fatalf("advance = %+v err %v", r.res, r.err)
		}
		if svc.StaleDecisions() != 1 || stub.calls.Load() != 3 {
			t.Fatalf("stale %d, requests %d", svc.StaleDecisions(), stub.calls.Load())
		}
		recs, _ := svc.DecisionRecords(ctx, root.ID)
		var rec *app.DecisionRecord
		for i := range recs {
			if recs[i].WindowTick == 4 {
				if rec != nil {
					t.Fatal("two records for window 4")
				}
				rec = &recs[i]
			}
		}
		// The discarded call is still charged; its commands are not recorded.
		if rec == nil || rec.Provenance != app.ProvAILLM || rec.LLMCallsCharged != 2 {
			t.Fatalf("record = %+v", rec)
		}
		if h, ai := inputsAt5(st, root.ID); h != 1 || ai != rec.AcceptedCount || ai != 1 {
			t.Fatalf("inputs human %d ai %d, accepted %d: stale commands reached the log", h, ai, rec.AcceptedCount)
		}
		s, _ := svc.LLMStatus(ctx, w.ID)
		if s.CallsMade != 3 {
			t.Fatalf("calls made %d, want 3 (every request counted once)", s.CallsMade)
		}
		if h, _ := svc.HashAt(ctx, root.ID, 4); h != coldHash(t, st, root.ID, 4) {
			t.Fatal("hash differs from a clean replay")
		}
		if err := svc.EndDecisionWindow(ctx, root.ID, 4, nil); !errors.Is(err, app.ErrWindowNotOpen) {
			t.Fatalf("window must be closed after the decision: %v", err)
		}
	})

	t.Run("injected disruption also invalidates", func(t *testing.T) {
		stub, svc, _, _, root := setup(t)
		release := stub.hold()
		defer release()
		out := advanceAsync(svc, ctx, root.ID)
		stub.waitArrived(t, 1)
		if _, err := svc.InjectDisruption(ctx, app.InjectRequest{Branch: root.ID, Kind: sim.DisruptionWorkforceAbsence, StartTick: 20, DurationTicks: 6, MagnitudeBps: 3000, Target: -1}); err != nil {
			t.Fatal(err)
		}
		release()
		if r := recv(t, out, "advance"); r.err != nil {
			t.Fatal(r.err)
		}
		if svc.StaleDecisions() != 1 {
			t.Fatalf("stale = %d", svc.StaleDecisions())
		}
	})

	t.Run("changed twice: error, nothing recorded, next call recovers", func(t *testing.T) {
		stub, svc, st, _, root := setup(t)
		release := stub.hold()
		defer release()
		out := advanceAsync(svc, ctx, root.ID)
		stub.waitArrived(t, 1)
		humanSubmit(t, svc, root.ID, 4, 1)
		// Free request 1 only: the gate is replaced before the retry arrives.
		release()
		release = stub.hold()
		stub.waitArrived(t, 1)
		humanSubmit(t, svc, root.ID, 4, 1)
		release()
		r := recv(t, out, "advance")
		if !errors.Is(r.err, app.ErrDecisionStale) {
			t.Fatalf("err = %v, want ErrDecisionStale", r.err)
		}
		if recs, _ := svc.DecisionRecords(ctx, root.ID); len(recs) != 1 {
			t.Fatalf("records = %d, want only window 0", len(recs))
		}
		if h, ai := inputsAt5(st, root.ID); h != 2 || ai != 0 {
			t.Fatalf("inputs human %d ai %d", h, ai)
		}
		// Not wedged: the branch waits at its window (tick 4); a later Advance
		// decides it, leaves it and stops at the next window.
		if hd, _ := svc.HeadTick(ctx, root.ID); hd != 4 {
			t.Fatalf("head after the failed advance = %d", hd)
		}
		again, err := svc.Advance(ctx, app.AdvanceRequest{Branch: root.ID})
		if err != nil || again.HeadTick != 8 {
			t.Fatalf("advance after stale: head %d err %v", again.HeadTick, err)
		}
		recs, _ := svc.DecisionRecords(ctx, root.ID)
		if len(recs) != 3 || recs[1].WindowTick != 4 || recs[1].LLMCallsCharged != 3 {
			t.Fatalf("records = %+v (the two discarded calls and the answer are charged to the window)", recs)
		}
		if h, _ := svc.HashAt(ctx, root.ID, 8); h != coldHash(t, st, root.ID, 8) {
			t.Fatal("hash differs from a clean replay")
		}
	})

	// (g) A spent Command_Budget at the retry yields no provider request.
	t.Run("spent budget after the change: no request", func(t *testing.T) {
		stub, svc, st, _, root := setup(t)
		release := stub.hold()
		defer release()
		out := advanceAsync(svc, ctx, root.ID)
		stub.waitArrived(t, 1)
		humanSubmit(t, svc, root.ID, 4, 5)
		release()
		if r := recv(t, out, "advance"); r.err != nil {
			t.Fatal(r.err)
		}
		if got := stub.calls.Load(); got != 2 {
			t.Fatalf("requests = %d, want 2 (window 0 and the discarded one; none for a spent budget)", got)
		}
		recs, _ := svc.DecisionRecords(ctx, root.ID)
		last := recs[len(recs)-1]
		if last.WindowTick != 4 || last.AcceptedCount != 0 || len(last.Proposed) != 0 || last.LLMCallsCharged != 1 {
			t.Fatalf("record = %+v", last)
		}
		if h, ai := inputsAt5(st, root.ID); h != 5 || ai != 0 {
			t.Fatalf("inputs human %d ai %d", h, ai)
		}
	})
}

// (d) The per-World caps hold when many windows are decided at once.
func TestAIConcurrencyWorldCaps(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		cfg   func(c *llm.Config)
		reply stubReply
	}{
		{"call cap", func(c *llm.Config) { c.MaxCallsPerWorld = 3 }, okReply(validProposal)},
		// A window reserves about 1440 tokens (estimated prompt + 100): 3 fit in 5000, a fourth does not.
		{"token cap", func(c *llm.Config) { c.MaxTokensPerCall, c.MaxTotalTokensPerWorld = 100, 5000 }, stubReply{content: validProposal, prompt: 60, completion: 40}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newGateStub(t, always(tc.reply))
			cfg := llmCfg(stub.srv.URL)
			tc.cfg(&cfg)
			svc := newConcSvc(t, cfg)
			w, root := createWorld(t, svc, app.ManagerHuman)
			release := stub.hold()
			defer release()

			const n = 10
			var wg sync.WaitGroup
			errs := make(chan error, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := svc.ForkBranch(ctx, root.ID, 0, fmt.Sprintf("ai-%d", i), app.ManagerAILLM)
					errs <- err
				}()
			}
			stub.waitArrived(t, 3)
			time.Sleep(200 * time.Millisecond) // let any over-budget request show up
			release()
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if got := stub.calls.Load(); got != 3 {
				t.Fatalf("provider requests = %d, want exactly 3", got)
			}
			bs, _ := svc.ListBranches(ctx, w.ID)
			var llmWins, capped int
			for _, b := range bs[1:] {
				recs, _ := svc.DecisionRecords(ctx, b.ID)
				if len(recs) != 1 {
					t.Fatalf("branch %s has %d records", b.Label, len(recs))
				}
				switch r := recs[0]; {
				case r.Provenance == app.ProvAILLM && r.LLMCallsCharged == 1:
					llmWins++
				case r.Provenance == app.ProvAIRuleBasedFallback && r.FallbackReason == "budget_exhausted" && r.LLMCallsCharged == 0:
					capped++
				default:
					t.Fatalf("record = %+v", r)
				}
			}
			if llmWins != 3 || capped != 7 {
				t.Fatalf("llm %d, budget_exhausted fallbacks %d, want 3 and 7", llmWins, capped)
			}
			s, _ := svc.LLMStatus(ctx, w.ID)
			if s.CallsMade != 3 || s.CallsRemaining > 0 && tc.name == "call cap" {
				t.Fatalf("status = %+v", s)
			}
		})
	}
}

// (e) A duel stays in lockstep and readable while the AI's request is slow.
func TestAIConcurrencyDuelLockstep(t *testing.T) {
	ctx := context.Background()
	stub := newGateStub(t, always(okReply(validProposal)))
	svc := newConcSvc(t, llmCfg(stub.srv.URL))
	ds := app.NewDuelService(svc, nil)
	_, root := createWorld(t, svc, app.ManagerHuman)
	du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: 0, AIKind: app.ManagerAILLM})
	if err != nil {
		t.Fatal(err)
	}
	stub.waitArrived(t, 1)
	if err := ds.EndHumanWindow(ctx, du.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	release := stub.hold()
	defer release()
	type res struct {
		r   app.AdvanceDuelResult
		err error
	}
	adv := make(chan res, 1)
	go func() { r, err := ds.AdvanceDuel(ctx, du.ID, nil); adv <- res{r, err} }()
	stub.waitArrived(t, 1) // the AI's window 4

	// The human side is readable and nothing has moved past the window.
	within(t, 500*time.Millisecond, "duel status", func() error {
		s, err := ds.DuelStatus(ctx, du.ID)
		if err == nil && (s.HumanHead != 4 || s.AIHead != 4) {
			err = fmt.Errorf("heads %d/%d, want 4/4", s.HumanHead, s.AIHead)
		}
		if err == nil && (!s.WaitingOnHuman) {
			err = errors.New("the human's window must be open")
		}
		return err
	})
	within(t, 500*time.Millisecond, "human reads", func() error {
		if _, err := svc.StateAt(ctx, du.HumanBranchID, 4); err != nil {
			return err
		}
		_, err := svc.HashAt(ctx, du.HumanBranchID, 4)
		return err
	})
	within(t, 500*time.Millisecond, "duel score", func() error { _, err := ds.DuelScore(ctx, du.ID); return err })
	// A second advance of the same duel waits; it cannot run ahead.
	second := make(chan res, 1)
	go func() { r, err := ds.AdvanceDuel(ctx, du.ID, nil); second <- res{r, err} }()
	select {
	case r := <-second:
		t.Fatalf("a second advance returned during the request: %+v %v", r.r, r.err)
	case <-time.After(150 * time.Millisecond):
	}
	// Another duel's lock is independent.
	within(t, 500*time.Millisecond, "duel operations of another world", func() error {
		_, r2 := createWorld(t, svc, app.ManagerHuman)
		_, err := svc.Advance(ctx, app.AdvanceRequest{Branch: r2.ID})
		return err
	})

	release()
	r := recv(t, adv, "advance")
	if r.err != nil || r.r.HeadTick != 4 || r.r.Human.HeadTick != 4 || r.r.AI.HeadTick != 4 {
		t.Fatalf("advance = %+v err %v", r.r, r.err)
	}
	// The queued advance meets the human's open window: the human is not bypassed.
	if r := recv(t, second, "second advance"); !errors.Is(r.err, app.ErrDuelWindowOpen) {
		t.Fatalf("second advance = %+v err %v, want the human's window to hold the duel", r.r, r.err)
	}
	hh, _ := svc.HeadTick(ctx, du.HumanBranchID)
	ah, _ := svc.HeadTick(ctx, du.AIBranchID)
	if hh != 4 || ah != 4 {
		t.Fatalf("heads %d/%d", hh, ah)
	}
	// Same exogenous conditions: the branches share the engine inputs that are
	// not decisions, so their order-arrival side is equal at every tick.
	for tk := sim.Tick(0); tk <= 4; tk++ {
		hs, _ := svc.StateAt(ctx, du.HumanBranchID, tk)
		as, _ := svc.StateAt(ctx, du.AIBranchID, tk)
		if hs.Orders.Created != as.Orders.Created {
			t.Fatalf("tick %d: orders created differ (%d vs %d)", tk, hs.Orders.Created, as.Orders.Created)
		}
	}
}

// (f) A caller that goes away does not lose or double-charge the decision.
func TestAIConcurrencyCancelledCaller(t *testing.T) {
	ctx := context.Background()
	stub := newGateStub(t, always(okReply(validProposal)))
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil, withLLM(t, llmCfg(stub.srv.URL)))
	w, root := createWorld(t, svc, app.ManagerAILLM)
	stub.waitArrived(t, 1)
	runtime.GC()
	before := runtime.NumGoroutine()

	release := stub.hold()
	defer release()
	cctx, cancel := context.WithCancel(ctx)
	out := advanceAsync(svc, cctx, root.ID)
	stub.waitArrived(t, 1)
	cancel()
	if r := recv(t, out, "cancelled advance"); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", r.err)
	}
	if svc.InflightForTest() != 1 {
		t.Fatal("the request must keep running for the next caller")
	}
	release()
	deadline := time.Now().Add(5 * time.Second)
	for svc.InflightForTest() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("flight never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The answer was recorded once; the next Advance reuses it and asks nothing.
	recs, _ := svc.DecisionRecords(ctx, root.ID)
	if len(recs) != 2 || recs[1].WindowTick != 4 || recs[1].LLMCallsCharged != 1 {
		t.Fatalf("records = %+v", recs)
	}
	r, err := svc.Advance(ctx, app.AdvanceRequest{Branch: root.ID})
	if err != nil || r.HeadTick != 8 {
		t.Fatalf("advance = %+v %v", r, err)
	}
	if got := stub.calls.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3 (windows 0, 4, 8)", got)
	}
	if s, _ := svc.LLMStatus(ctx, w.ID); s.CallsMade != 3 {
		t.Fatalf("calls made = %d", s.CallsMade)
	}
	// No goroutine is left behind (a small allowance for the HTTP connection pool).
	deadline = time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Fatalf("goroutines: %d before, %d after", before, n)
	}
}

// (g) The Command_Budget bounds what an AI window can store.
func TestAIConcurrencyProposalBudget(t *testing.T) {
	ctx := context.Background()

	t.Run("more proposals than the budget is a schema violation", func(t *testing.T) {
		var many []string
		for i := 0; i < 6; i++ {
			many = append(many, `{"kind":"hire_temp_workers","qty":1}`)
		}
		body := `{"commands":[` + join(many) + `],"rationale":"everything"}`
		stub := newGateStub(t, always(okReply(body)))
		st := memory.New()
		svc := app.NewBranchService(st, nil, nil, withLLM(t, llmCfg(stub.srv.URL)))
		_, root := createWorld(t, svc, app.ManagerAILLM)
		recs, _ := svc.DecisionRecords(ctx, root.ID)
		if len(recs) != 1 || recs[0].Provenance != app.ProvAIRuleBasedFallback || recs[0].FallbackReason != "schema_violation" || recs[0].LLMCallsCharged != 1 {
			t.Fatalf("record = %+v", recs)
		}
		// Nothing from the oversized proposal was stored.
		in, _ := st.InputsForBranch(ctx, root.ID, 1)
		for _, r := range in {
			if r.Metadata.Provenance == app.ProvAILLM {
				t.Fatalf("an ai_llm input was stored: %+v", r)
			}
		}
		if len(in) > 5 {
			t.Fatalf("%d inputs in one window", len(in))
		}
	})

	t.Run("a decider that overshoots the remaining budget is cut at the validator", func(t *testing.T) {
		over := deciderFunc(func(_ context.Context, _ app.Branch, _ sim.Observation, budget int) (app.Decision, error) {
			var cmds []sim.Command
			for i := 0; i < budget+3; i++ {
				cmds = append(cmds, sim.Command{Kind: sim.CmdSetMarketingSpend, SKU: int64(i % 3), AmountCents: sim.Cents(100 * (i + 1))})
			}
			return app.Decision{Commands: cmds, Rationale: "too many"}, nil
		})
		st := memory.New()
		svc, b := newAIWorld(t, st, bfJSON(t), factoryOf(over))
		recs, _ := svc.DecisionRecords(ctx, b.ID)
		if len(recs) != 1 {
			t.Fatalf("records = %+v", recs)
		}
		r := recs[0]
		in, _ := st.InputsForBranch(ctx, b.ID, 1)
		if r.AcceptedCount > 5 || len(in) > 5 || r.AcceptedCount != len(in) {
			t.Fatalf("accepted %d, stored %d, budget 5", r.AcceptedCount, len(in))
		}
		var dropped int
		for _, rj := range r.Rejected {
			if rj.Code == string(sim.ReasonBudgetExhausted) {
				dropped++
			}
		}
		if len(r.Proposed) != 8 || r.AcceptedCount+len(r.Rejected) != 8 || dropped != 3 {
			t.Fatalf("proposed %d accepted %d rejected %+v (want 3 budget_exhausted)", len(r.Proposed), r.AcceptedCount, r.Rejected)
		}
	})
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
