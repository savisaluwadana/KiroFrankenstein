package app_test

import (
	"context"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
)

// The server-wide daily LLM limits survive a process restart: a new
// BranchService and a new LLM client tracker over the same PostgreSQL database
// rebuild today's usage from the persisted decision records, so the caps keep
// holding and an exhausted day stays exhausted (no provider call).
//
// Limitation: usage is rebuilt from decision records only. The in-memory
// overshoot counter of a call that was reserved but never recorded (a crash
// between the provider answer and the record insert) is not persisted, so
// such a call is not counted after a restart.
//
// Skipped unless PARALLEL_TEST_DATABASE_URL is set. pgPool takes the shared
// advisory lock, so other PostgreSQL LLM tests cannot move the global counter
// while these assert exact numbers.

func forkLLM(t *testing.T, svc *app.BranchService, parent app.BranchID, label string) app.Branch {
	t.Helper()
	b, err := svc.ForkBranch(context.Background(), parent, 0, label, app.ManagerAILLM)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func lastReason(t *testing.T, svc *app.BranchService, id app.BranchID) (app.Provenance, string) {
	t.Helper()
	recs, err := svc.DecisionRecords(context.Background(), id)
	if err != nil || len(recs) != 1 {
		t.Fatalf("records of %s = %d err %v", id, len(recs), err)
	}
	return recs[0].Provenance, recs[0].FallbackReason
}

func TestAIRestartGlobalCallCapStaysExhausted(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)
	base := usageToday(t, pool)
	stub1 := newGateStub(t, always(stubReply{content: validProposal, prompt: 60, completion: 40}))
	cfg := pgCfg(stub1.srv.URL)
	cfg.MaxCallsPerWorld = 100
	cfg.GlobalMaxCalls = base.Calls + 2
	cfg.GlobalMaxTokens = 1_000_000_000

	svc1 := pgSvc(t, pool, cfg)
	_, root := pgWorld(t, svc1, app.ManagerHuman)
	forkLLM(t, svc1, root.ID, "a")
	forkLLM(t, svc1, root.ID, "b")
	if got := stub1.calls.Load(); got != 2 {
		t.Fatalf("provider calls before restart = %d, want 2", got)
	}

	// Restart: new service, new tracker, new provider stub.
	stub2 := newGateStub(t, always(stubReply{content: validProposal, prompt: 60, completion: 40}))
	cfg2 := cfg
	cfg2.BaseURL = stub2.srv.URL
	svc2 := pgSvc(t, pool, cfg2)
	used := usageToday(t, pool)
	calls, tokens, ok, err := svc2.LLMGlobalStatus(ctx)
	if err != nil || !ok {
		t.Fatalf("status: ok %v err %v", ok, err)
	}
	if want := cfg.GlobalMaxCalls - used.Calls; calls != want || calls != 0 {
		t.Fatalf("calls remaining after restart = %d, want config minus persisted = %d (and 0)", calls, want)
	}
	if want := cfg.GlobalMaxTokens - used.PromptTokens - used.CompletionTokens; tokens != want {
		t.Fatalf("tokens remaining after restart = %d, want %d", tokens, want)
	}
	// The exhausted day makes no provider call and records the reason.
	nb := forkLLM(t, svc2, root.ID, "after-restart")
	if got := stub2.calls.Load(); got != 0 {
		t.Fatalf("provider calls after restart = %d, want 0", got)
	}
	if prov, reason := lastReason(t, svc2, nb.ID); prov != app.ProvAIRuleBasedFallback || reason != "budget_exhausted" {
		t.Fatalf("record = %s/%s, want fallback budget_exhausted", prov, reason)
	}
	if after := usageToday(t, pool); after.Calls != used.Calls {
		t.Fatalf("an exhausted day charged %d call(s)", after.Calls-used.Calls)
	}
}

func TestAIRestartGlobalTokenCapStaysExhausted(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)
	base := usageToday(t, pool)
	// One answer reports 6000 tokens, more than the 5000 the day allows
	// (reservation estimate fits): the overshoot is recorded and blocks the rest.
	stub1 := newGateStub(t, always(stubReply{content: validProposal, prompt: 6000, completion: 0}))
	cfg := pgCfg(stub1.srv.URL)
	cfg.MaxCallsPerWorld = 100
	cfg.GlobalMaxCalls = 1_000_000
	cfg.GlobalMaxTokens = base.PromptTokens + base.CompletionTokens + 5000

	svc1 := pgSvc(t, pool, cfg)
	_, root := pgWorld(t, svc1, app.ManagerHuman)
	forkLLM(t, svc1, root.ID, "a")
	if got := stub1.calls.Load(); got != 1 {
		t.Fatalf("provider calls before restart = %d, want 1", got)
	}

	stub2 := newGateStub(t, always(stubReply{content: validProposal, prompt: 60, completion: 40}))
	cfg2 := cfg
	cfg2.BaseURL = stub2.srv.URL
	svc2 := pgSvc(t, pool, cfg2)
	used := usageToday(t, pool)
	calls, tokens, ok, err := svc2.LLMGlobalStatus(ctx)
	if err != nil || !ok {
		t.Fatalf("status: ok %v err %v", ok, err)
	}
	if want := cfg.GlobalMaxCalls - used.Calls; calls != want {
		t.Fatalf("calls remaining after restart = %d, want %d", calls, want)
	}
	if want := max(cfg.GlobalMaxTokens-used.PromptTokens-used.CompletionTokens, 0); tokens != want || tokens != 0 {
		t.Fatalf("tokens remaining after restart = %d, want %d (and 0)", tokens, want)
	}
	nb := forkLLM(t, svc2, root.ID, "after-restart")
	if got := stub2.calls.Load(); got != 0 {
		t.Fatalf("provider calls after restart = %d, want 0", got)
	}
	if prov, reason := lastReason(t, svc2, nb.ID); prov != app.ProvAIRuleBasedFallback || reason != "budget_exhausted" {
		t.Fatalf("record = %s/%s, want fallback budget_exhausted", prov, reason)
	}
}
