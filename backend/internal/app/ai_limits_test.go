package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// limitStub counts requests, tracks the peak of concurrent requests, records
// the largest body and can hold requests back.
type limitStub struct {
	srv      *httptest.Server
	calls    atomic.Int64
	cur, max atomic.Int64
	bodyLen  atomic.Int64 // system+user content bytes of the last request
	hold     atomic.Pointer[chan struct{}]
	prompt   int
	compl    int
	noUsage  bool
}

func newLimitStub(t *testing.T, prompt, compl int) *limitStub {
	t.Helper()
	s := &limitStub{prompt: prompt, compl: compl}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		cur := s.cur.Add(1)
		defer s.cur.Add(-1)
		for {
			m := s.max.Load()
			if cur <= m || s.max.CompareAndSwap(m, cur) {
				break
			}
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.Unmarshal(b, &req)
		n := 0
		for _, m := range req.Messages {
			n += len(m.Content)
		}
		s.bodyLen.Store(int64(n))
		if ch := s.hold.Load(); ch != nil {
			select {
			case <-*ch:
			case <-r.Context().Done():
				return
			}
		}
		resp := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": validProposal}}}}
		if !s.noUsage {
			resp["usage"] = map[string]any{"prompt_tokens": s.prompt, "completion_tokens": s.compl, "total_tokens": s.prompt + s.compl}
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *limitStub) block() (release func()) {
	ch := make(chan struct{})
	s.hold.Store(&ch)
	var once sync.Once
	return func() { once.Do(func() { s.hold.Store(nil); close(ch) }) }
}

func (s *limitStub) waitCalls(t *testing.T, n int64) {
	t.Helper()
	for i := 0; i < 500 && s.calls.Load() < n; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.calls.Load(); got < n {
		t.Fatalf("only %d of %d provider requests arrived", got, n)
	}
}

func globalLeft(t *testing.T, svc *app.BranchService) (calls, tokens int) {
	t.Helper()
	c, tk, ok, err := svc.LLMGlobalStatus(context.Background())
	if err != nil || !ok {
		t.Fatalf("global status ok=%v err=%v", ok, err)
	}
	return c, tk
}

func lastRec(t *testing.T, svc *app.BranchService, id app.BranchID) app.DecisionRecord {
	t.Helper()
	recs, err := svc.DecisionRecords(context.Background(), id)
	if err != nil || len(recs) == 0 {
		t.Fatalf("records %v %v", len(recs), err)
	}
	return recs[len(recs)-1]
}

// A provider that reports no usage is charged the whole reservation, and the
// reservation follows the size of the exact request: a larger observation
// reserves more.
func TestLLMReservationFollowsPromptSize(t *testing.T) {
	stub := newLimitStub(t, 0, 0)
	stub.noUsage = true
	cfg := llmCfg(stub.srv.URL)
	svc, w, b := newLLMWorld(t, memory.New(), cfg, app.ManagerAILLM)
	runTo(t, svc, b.ID, 24)
	recs, _ := svc.DecisionRecords(context.Background(), b.ID)
	if len(recs) != 7 {
		t.Fatalf("records = %d", len(recs))
	}
	minTok, maxTok := 1<<30, 0
	for i, r := range recs {
		if r.Provenance != app.ProvAILLM || r.CompletionTokens != cfg.MaxTokensPerCall || r.PromptTokens < 1000 {
			t.Fatalf("window %d must be charged its full reservation: %+v", i, r)
		}
		minTok, maxTok = min(minTok, r.PromptTokens), max(maxTok, r.PromptTokens)
	}
	if maxTok <= minTok {
		t.Fatalf("reservation did not grow with the observation: min %d max %d", minTok, maxTok)
	}
	// The estimate covers the exact system+user text at 1 token per 3 bytes.
	last := recs[len(recs)-1]
	if int64(last.PromptTokens)*3 < stub.bodyLen.Load() {
		t.Fatalf("prompt estimate %d tokens does not cover a %d byte request", last.PromptTokens, stub.bodyLen.Load())
	}
	s, _ := svc.LLMStatus(context.Background(), w.ID)
	if s.TokensUsed < 7*1500 {
		t.Fatalf("status = %+v", s)
	}
}

// A provider that reports far more than the reservation is accounted in full,
// and no further request is made once the limit is exceeded.
func TestLLMOvershootAccountedAndBlocks(t *testing.T) {
	logs := logCapture(t)
	stub := newLimitStub(t, 90000, 60000) // 150000 reported
	cfg := llmCfg(stub.srv.URL)
	cfg.MaxTotalTokensPerWorld = 100000
	cfg.GlobalMaxTokens = 120000
	svc, w, b := newLLMWorld(t, memory.New(), cfg, app.ManagerAILLM)
	runTo(t, svc, b.ID, 16)
	recs, _ := svc.DecisionRecords(context.Background(), b.ID)
	for i, r := range recs {
		if i == 0 {
			if r.Provenance != app.ProvAILLM || r.PromptTokens != 90000 || r.CompletionTokens != 60000 {
				t.Fatalf("window 0 = %+v", r)
			}
			continue
		}
		if r.FallbackReason != "budget_exhausted" || r.LLMCallsCharged != 0 {
			t.Fatalf("window %d = %+v", i, r)
		}
	}
	if stub.calls.Load() != 1 {
		t.Fatalf("provider requests = %d, want 1", stub.calls.Load())
	}
	s, _ := svc.LLMStatus(context.Background(), w.ID)
	if s.TokensUsed != 150000 || s.TokensRemaining != 0 {
		t.Fatalf("status = %+v", s)
	}
	if _, tk := globalLeft(t, svc); tk != 0 {
		t.Fatalf("global tokens remaining = %d", tk)
	}
	if !strings.Contains(logs.String(), "more tokens than reserved") || strings.Contains(logs.String(), testKey) {
		t.Fatalf("overshoot must be logged without the key:\n%s", logs.String())
	}
}

func TestLLMPromptTooLargeRejectedBeforeNetwork(t *testing.T) {
	stub := newLimitStub(t, 10, 10)
	cfg := llmCfg(stub.srv.URL)
	cfg.MaxPromptBytes = llm.MinMaxPromptBytes // the system prompt alone is larger
	svc, w, b := newLLMWorld(t, memory.New(), cfg, app.ManagerAILLM)
	runTo(t, svc, b.ID, 8)
	recs, _ := svc.DecisionRecords(context.Background(), b.ID)
	for i, r := range recs {
		if r.Provenance != app.ProvAIRuleBasedFallback || r.FallbackReason != "budget_exhausted" || r.LLMCallsCharged != 0 || r.PromptTokens != 0 {
			t.Fatalf("window %d = %+v", i, r)
		}
	}
	if stub.calls.Load() != 0 {
		t.Fatalf("provider requests = %d, want 0", stub.calls.Load())
	}
	if s, _ := svc.LLMStatus(context.Background(), w.ID); s.CallsMade != 0 || s.TokensUsed != 0 {
		t.Fatalf("status = %+v", s)
	}
}

// Ten windows over three worlds at once: the per-world call cap (2) and the
// global call cap (5) both hold exactly, however the windows interleave.
func TestLLMPerWorldAndGlobalCapsConcurrent(t *testing.T) {
	ctx := context.Background()
	stub := newLimitStub(t, 100, 50)
	release := stub.block()
	defer release()
	cfg := llmCfg(stub.srv.URL)
	cfg.MaxCallsPerWorld, cfg.GlobalMaxCalls = 2, 5
	cfg.MaxConcurrentRequests = 16
	svc := newConcSvc(t, cfg)
	counts := []int{4, 3, 3}
	worlds := make([]app.World, 3)
	roots := make([]app.Branch, 3)
	for i := range worlds {
		worlds[i], roots[i] = createWorld(t, svc, app.ManagerHuman)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for wi, n := range counts {
		for j := 0; j < n; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.ForkBranch(ctx, roots[wi].ID, 0, fmt.Sprintf("ai-%d-%d", wi, j), app.ManagerAILLM)
				errs <- err
			}()
		}
	}
	stub.waitCalls(t, 5)
	time.Sleep(200 * time.Millisecond) // let any over-budget request show up
	release()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := stub.calls.Load(); got != 5 {
		t.Fatalf("provider requests = %d, want exactly 5", got)
	}
	total := 0
	for wi, w := range worlds {
		s, _ := svc.LLMStatus(ctx, w.ID)
		if s.CallsMade > 2 || s.CallsMade != 2-s.CallsRemaining {
			t.Fatalf("world %d status = %+v", wi, s)
		}
		total += s.CallsMade
		bs, _ := svc.ListBranches(ctx, w.ID)
		for _, b := range bs[1:] {
			r := lastRec(t, svc, b.ID)
			ok := r.Provenance == app.ProvAILLM || (r.FallbackReason == "budget_exhausted" && r.LLMCallsCharged == 0)
			if !ok {
				t.Fatalf("record = %+v", r)
			}
		}
	}
	if total != 5 {
		t.Fatalf("calls over worlds = %d, want 5", total)
	}
	if c, _ := globalLeft(t, svc); c != 0 {
		t.Fatalf("global calls remaining = %d", c)
	}
}

// Once the global token limit is exceeded by one world, every world falls
// back with budget_exhausted and no further request is made.
func TestLLMGlobalTokenCapExhaustedForAllWorlds(t *testing.T) {
	ctx := context.Background()
	stub := newLimitStub(t, 2000, 1000)
	cfg := llmCfg(stub.srv.URL)
	cfg.GlobalMaxTokens = 2500 // the first reservation (~1600) fits; its 3000 actual exceeds
	svc := newConcSvc(t, cfg)
	_, a := createWorld(t, svc, app.ManagerAILLM)
	_, b := createWorld(t, svc, app.ManagerAILLM)
	runTo(t, svc, a.ID, 8)
	runTo(t, svc, b.ID, 8)
	for _, id := range []app.BranchID{a.ID, b.ID} {
		recs, _ := svc.DecisionRecords(ctx, id)
		for _, r := range recs {
			if r.Provenance == app.ProvAILLM {
				continue
			}
			if r.FallbackReason != "budget_exhausted" || r.LLMCallsCharged != 0 {
				t.Fatalf("record = %+v", r)
			}
		}
	}
	if stub.calls.Load() != 1 {
		t.Fatalf("provider requests = %d, want 1", stub.calls.Load())
	}
	if _, tk := globalLeft(t, svc); tk != 0 {
		t.Fatalf("global tokens remaining = %d", tk)
	}
}

// At most LLM_MAX_CONCURRENT_REQUESTS provider requests are in flight.
func TestLLMConcurrencyLimit(t *testing.T) {
	ctx := context.Background()
	stub := newLimitStub(t, 100, 50)
	release := stub.block()
	defer release()
	cfg := llmCfg(stub.srv.URL)
	cfg.MaxConcurrentRequests = 2
	cfg.RequestTimeout = 10 * time.Second
	svc := newConcSvc(t, cfg)
	_, root := createWorld(t, svc, app.ManagerHuman)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.ForkBranch(ctx, root.ID, 0, fmt.Sprintf("ai-%d", i), app.ManagerAILLM); err != nil {
				t.Error(err)
			}
		}()
	}
	stub.waitCalls(t, 2)
	time.Sleep(200 * time.Millisecond)
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("requests in flight = %d, want 2 while the rest wait for a slot", got)
	}
	release()
	wg.Wait()
	if got, peak := stub.calls.Load(), stub.max.Load(); got != 10 || peak != 2 {
		t.Fatalf("requests = %d, peak concurrent = %d, want 10 and 2", got, peak)
	}
}

// A window that finds no free slot within the request timeout falls back with
// reason timeout and is not charged. One slot, three windows: the first holds
// the slot for the whole timeout; of the two later ones (started 100 ms after
// it) one takes the slot when it frees and the other waits out its timer.
func TestLLMSaturationFallsBackWithTimeout(t *testing.T) {
	ctx := context.Background()
	stub := newLimitStub(t, 100, 50)
	release := stub.block()
	defer release()
	cfg := llmCfg(stub.srv.URL)
	cfg.MaxConcurrentRequests = 1
	cfg.RequestTimeout = 400 * time.Millisecond
	svc := newConcSvc(t, cfg)
	w, root := createWorld(t, svc, app.ManagerHuman)
	var wg sync.WaitGroup
	fork := func(i int) {
		defer wg.Done()
		if _, err := svc.ForkBranch(ctx, root.ID, 0, fmt.Sprintf("ai-%d", i), app.ManagerAILLM); err != nil {
			t.Error(err)
		}
	}
	wg.Add(1)
	go fork(0)
	stub.waitCalls(t, 1)
	time.Sleep(100 * time.Millisecond)
	wg.Add(2)
	go fork(1)
	go fork(2)
	wg.Wait()
	bs, _ := svc.ListBranches(ctx, w.ID)
	timeouts, charged := 0, 0
	for _, b := range bs[1:] {
		r := lastRec(t, svc, b.ID)
		if r.FallbackReason == "timeout" {
			timeouts++
		}
		charged += r.LLMCallsCharged
	}
	// All three fall back with timeout (the stub never answers); two requests
	// reached the provider and the saturated window is not charged.
	if stub.calls.Load() != 2 || timeouts != 3 || charged != 2 {
		t.Fatalf("requests %d timeouts %d charged %d", stub.calls.Load(), timeouts, charged)
	}
	if s, _ := svc.LLMStatus(ctx, w.ID); s.CallsMade != 2 {
		t.Fatalf("status = %+v", s)
	}
}

// After a restart the server-wide daily usage is rebuilt from the decision
// records created since 00:00 UTC; older records do not count.
func TestLLMGlobalUsageRebuiltAfterRestart(t *testing.T) {
	ctx := context.Background()
	stub := newLimitStub(t, 100, 50)
	st := memory.New()
	cfg := llmCfg(stub.srv.URL)
	cfg.GlobalMaxCalls, cfg.GlobalMaxTokens = 100, 100000
	svc, _, b := newLLMWorld(t, st, cfg, app.ManagerAILLM)
	runTo(t, svc, b.ID, 8) // windows 0, 4, 8: 3 calls of 150 tokens
	if c, tk := globalLeft(t, svc); c != 97 || tk != 100000-450 {
		t.Fatalf("before restart: %d calls, %d tokens left", c, tk)
	}
	// A record of yesterday must not be counted.
	if err := st.AppendDecision(ctx, app.DecisionRecord{
		BranchID: b.ID, WindowTick: 999, ManagerKind: app.ManagerAILLM, Provenance: app.ProvAILLM,
		LLMCallsCharged: 1, PromptTokens: 50000, CompletionTokens: 1,
		CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	svc2 := app.NewBranchService(st, nil, nil, withLLM(t, cfg)) // restart: same records, fresh process state
	if c, tk := globalLeft(t, svc2); c != 97 || tk != 100000-450 {
		t.Fatalf("after restart: %d calls, %d tokens left", c, tk)
	}
}

func TestLLMGlobalStatusWithoutLLM(t *testing.T) {
	svc := app.NewBranchService(memory.New(), nil, nil)
	if _, _, ok, err := svc.LLMGlobalStatus(context.Background()); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
