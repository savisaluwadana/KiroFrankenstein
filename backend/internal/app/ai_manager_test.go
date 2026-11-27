package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

const (
	testKey   = "sk-test-SECRET-0123456789"
	testModel = "test/model-1"
)

// stubReply is what the stub provider does for one request.
type stubReply struct {
	status     int           // 0 means 200
	content    string        // assistant message content
	raw        string        // when set, the whole response body
	prompt     int           // usage reported
	completion int           //
	delay      time.Duration // before answering
}

// llmStub is an OpenAI-compatible chat completions endpoint.
type llmStub struct {
	srv   *httptest.Server
	calls atomic.Int64
	reply func(n int64) stubReply
}

func newStub(t *testing.T, reply func(n int64) stubReply) *llmStub {
	t.Helper()
	s := &llmStub{reply: reply}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := s.calls.Add(1)
		rep := s.reply(n)
		_, _ = readAll(r)
		if rep.delay > 0 {
			select {
			case <-time.After(rep.delay):
			case <-r.Context().Done():
				return
			}
		}
		if rep.status != 0 && rep.status != http.StatusOK {
			w.WriteHeader(rep.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if rep.raw != "" {
			_, _ = w.Write([]byte(rep.raw))
			return
		}
		body, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": rep.content}}},
			"usage":   map[string]any{"prompt_tokens": rep.prompt, "completion_tokens": rep.completion, "total_tokens": rep.prompt + rep.completion},
		})
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func readAll(r *http.Request) (int64, error) {
	var b bytes.Buffer
	n, err := b.ReadFrom(r.Body)
	return n, err
}

func always(rep stubReply) func(int64) stubReply { return func(int64) stubReply { return rep } }

func okReply(content string) stubReply {
	return stubReply{content: content, prompt: 100, completion: 50}
}

func llmCfg(url string) llm.Config {
	return llm.Config{
		BaseURL: url, APIKey: testKey, Model: testModel, RequestTimeout: 2 * time.Second,
		MaxCallsPerWorld: 40, MaxTokensPerCall: 256, MaxTotalTokensPerWorld: 1_000_000,
	}
}

func withLLM(t *testing.T, cfg llm.Config) app.Option {
	t.Helper()
	opt, err := app.WithLLM(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return opt
}

// newLLMWorld creates a World whose root Branch is managed by the LLM AI.
func newLLMWorld(t *testing.T, st *memory.Store, cfg llm.Config, mgr app.ManagerKind) (*app.BranchService, app.World, app.Branch) {
	t.Helper()
	svc := app.NewBranchService(st, nil, nil, withLLM(t, cfg))
	w, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{
		Name: "llm", ScenarioJSON: bfJSON(t), Seed: 2026, Manager: mgr,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, w, b
}

// logCapture redirects slog to a buffer for the test.
func logCapture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{w: &buf}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func noKey(t *testing.T, what string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(testKey)) || bytes.Contains(b, []byte("SECRET")) {
		t.Fatalf("%s contains the API key", what)
	}
}

const validProposal = `{"commands":[{"kind":"hire_temp_workers","qty":1}],"rationale":"Add <b>staff</b> before the surge."}`

func TestLLMSuccessRecordsProvenanceModelRationaleAndUsage(t *testing.T) {
	ctx := context.Background()
	logs := logCapture(t)
	stub := newStub(t, always(okReply(validProposal)))
	st := memory.New()
	svc, w, b := newLLMWorld(t, st, llmCfg(stub.srv.URL), app.ManagerAILLM)
	runTo(t, svc, b.ID, 8)

	recs, err := svc.DecisionRecords(ctx, b.ID)
	if err != nil || len(recs) != 3 {
		t.Fatalf("records %d err %v", len(recs), err)
	}
	var accepted int
	for _, r := range recs {
		if r.Provenance != app.ProvAILLM || r.Model != testModel || r.FallbackReason != "" || r.ManagerKind != app.ManagerAILLM {
			t.Fatalf("record = %+v", r)
		}
		if r.Rationale != "Add <b>staff</b> before the surge." {
			t.Fatalf("rationale = %q", r.Rationale)
		}
		if r.LLMCallsCharged != 1 || r.PromptTokens != 100 || r.CompletionTokens != 50 {
			t.Fatalf("usage = %d/%d/%d", r.LLMCallsCharged, r.PromptTokens, r.CompletionTokens)
		}
		accepted += r.AcceptedCount
	}
	if accepted == 0 {
		t.Fatal("no LLM command was accepted")
	}
	inputs, _ := st.InputsForBranch(ctx, b.ID, 1000)
	if len(inputs) != accepted {
		t.Fatalf("inputs %d, accepted %d", len(inputs), accepted)
	}
	for _, in := range inputs {
		if in.Metadata.Provenance != app.ProvAILLM || in.Metadata.ModelName != testModel || in.Metadata.Rationale == "" {
			t.Fatalf("input metadata = %+v", in.Metadata)
		}
	}
	if got := stub.calls.Load(); got != 3 {
		t.Fatalf("provider requests = %d, want one per window (3)", got)
	}
	// Usage accounting.
	s, err := svc.LLMStatus(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := app.LLMStatus{Available: true, Model: testModel, CallsMade: 3, CallsRemaining: 37, TokensUsed: 450, TokensRemaining: 1_000_000 - 450}
	if s != want {
		t.Fatalf("status = %+v, want %+v", s, want)
	}
	// The key never reaches a record, a status or a log line.
	noKey(t, "records", recs)
	noKey(t, "status", s)
	noKey(t, "inputs", inputs)
	if strings.Contains(logs.String(), testKey) || strings.Contains(logs.String(), stub.srv.URL) {
		t.Fatalf("log contains the key or URL:\n%s", logs.String())
	}
}

func TestLLMFallback(t *testing.T) {
	ctx := context.Background()
	budget := int(mustScenario(t).CommandBudget)
	tooMany := `{"commands":[` + strings.TrimSuffix(strings.Repeat(`{"kind":"hire_temp_workers","qty":1},`, budget+1), ",") + `],"rationale":"x"}`

	cases := []struct {
		name   string
		cfg    func(*llm.Config)
		reply  stubReply
		reason string
		calls  int // charged per window
		want   int // provider requests in 3 windows
	}{
		{"timeout", func(c *llm.Config) { c.RequestTimeout = 100 * time.Millisecond }, stubReply{content: validProposal, delay: 600 * time.Millisecond}, "timeout", 1, 3},
		{"http_error", nil, stubReply{status: 500}, "http_error", 1, 3},
		{"http_error 429", nil, stubReply{status: 429}, "http_error", 1, 3},
		{"invalid_json garbage", nil, okReply("I think you should buy more stock."), "invalid_json", 1, 3},
		{"invalid_json envelope", nil, stubReply{raw: "<html>gateway</html>"}, "invalid_json", 1, 3},
		{"invalid_json empty content", nil, okReply("   "), "invalid_json", 1, 3},
		{"invalid_json trailing data", nil, okReply(`{"commands":[],"rationale":"a"} {"commands":[]}`), "invalid_json", 1, 3},
		{"schema wrong types", nil, okReply(`{"commands":"nope","rationale":1}`), "schema_violation", 1, 3},
		{"schema extra field", nil, okReply(`{"commands":[],"rationale":"x","extra":1}`), "schema_violation", 1, 3},
		{"schema extra command field", nil, okReply(`{"commands":[{"kind":"hire_temp_workers","qty":1,"x":2}],"rationale":"x"}`), "schema_violation", 1, 3},
		{"schema unknown kind", nil, okReply(`{"commands":[{"kind":"nuke_competitor"}],"rationale":"x"}`), "schema_violation", 1, 3},
		{"schema missing parameter", nil, okReply(`{"commands":[{"kind":"place_purchase_order","sku":0,"role":"primary"}],"rationale":"x"}`), "schema_violation", 1, 3},
		{"schema negative", nil, okReply(`{"commands":[{"kind":"hire_temp_workers","qty":-3}],"rationale":"x"}`), "schema_violation", 1, 3},
		{"schema too many commands", nil, okReply(tooMany), "schema_violation", 1, 3},
		{"budget_exhausted calls", func(c *llm.Config) { c.MaxCallsPerWorld = 1 }, okReply(validProposal), "budget_exhausted", 0, 1},
		{"budget_exhausted tokens", func(c *llm.Config) { c.MaxTotalTokensPerWorld = 100 }, okReply(validProposal), "budget_exhausted", 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newStub(t, always(tc.reply))
			cfg := llmCfg(stub.srv.URL)
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			st := memory.New()
			svc, _, b := newLLMWorld(t, st, cfg, app.ManagerAILLM)
			runTo(t, svc, b.ID, 8)
			recs, _ := svc.DecisionRecords(ctx, b.ID)
			if len(recs) != 3 {
				t.Fatalf("records = %d", len(recs))
			}
			for i, r := range recs {
				first := tc.want == 1 && i == 0 // the single call that was allowed
				if first {
					if r.Provenance != app.ProvAILLM {
						t.Fatalf("window %d = %+v", i, r)
					}
					continue
				}
				if r.Provenance != app.ProvAIRuleBasedFallback || r.FallbackReason != tc.reason || r.ManagerKind != app.ManagerAILLM || r.Model != "" {
					t.Fatalf("window %d record = %+v, want fallback %q", i, r, tc.reason)
				}
				if r.LLMCallsCharged != tc.calls {
					t.Fatalf("window %d charged %d calls, want %d", i, r.LLMCallsCharged, tc.calls)
				}
			}
			if got := stub.calls.Load(); got != int64(tc.want) {
				t.Fatalf("provider requests = %d, want %d", got, tc.want)
			}
			inputs, _ := st.InputsForBranch(ctx, b.ID, 1000)
			for _, in := range inputs {
				if in.Metadata.Provenance == app.ProvAIRuleBasedFallback && in.Metadata.ModelName != "" {
					t.Fatalf("fallback input names a model: %+v", in.Metadata)
				}
			}
		})
	}
}

func mustScenario(t *testing.T) sim.Scenario {
	t.Helper()
	sc, err := sim.ParseScenario(bfJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	return *sc
}

// A permanently failing provider makes the LLM Branch behave exactly like the
// Rule-based AI of the same World: same proposals, same State_Hash, with the
// fallback commands recorded and validated like any others.
func TestLLMFallbackEqualsRuleBased(t *testing.T) {
	ctx := context.Background()
	stub := newStub(t, always(stubReply{status: 503}))
	svc, _, b := newLLMWorld(t, memory.New(), llmCfg(stub.srv.URL), app.ManagerAILLM)
	rsvc, rb := newAIWorld(t, memory.New(), bfJSON(t))
	runTo(t, svc, b.ID, 48)
	runTo(t, rsvc, rb.ID, 48)
	lr, _ := svc.DecisionRecords(ctx, b.ID)
	rr, _ := rsvc.DecisionRecords(ctx, rb.ID)
	if len(lr) != len(rr) {
		t.Fatalf("records %d vs %d", len(lr), len(rr))
	}
	var accepted int
	for i := range lr {
		if !reflect.DeepEqual(lr[i].Proposed, rr[i].Proposed) || lr[i].Rationale != rr[i].Rationale || lr[i].AcceptedCount != rr[i].AcceptedCount {
			t.Fatalf("window %d: fallback proposal differs from the rule-based one", i)
		}
		if lr[i].Provenance != app.ProvAIRuleBasedFallback || lr[i].FallbackReason != "http_error" {
			t.Fatalf("window %d = %+v", i, lr[i])
		}
		accepted += lr[i].AcceptedCount
	}
	if accepted == 0 {
		t.Fatal("fallback never acted; the comparison is vacuous")
	}
	lh, _ := svc.HashAt(ctx, b.ID, 48)
	rh, _ := rsvc.HashAt(ctx, rb.ID, 48)
	if lh != rh {
		t.Fatal("State_Hash differs between fallback and rule-based branches")
	}
}

func TestLLMModelOutputHandling(t *testing.T) {
	ctx := context.Background()
	t.Run("code fence is accepted", func(t *testing.T) {
		stub := newStub(t, always(okReply("```json\n"+validProposal+"\n```")))
		svc, _, b := newLLMWorld(t, memory.New(), llmCfg(stub.srv.URL), app.ManagerAILLM)
		recs, _ := svc.DecisionRecords(ctx, b.ID)
		if len(recs) != 1 || recs[0].Provenance != app.ProvAILLM {
			t.Fatalf("records = %+v", recs)
		}
	})
	t.Run("invalid commands are dropped and recorded, not replaced by a fallback", func(t *testing.T) {
		bad := fmt.Sprintf(`{"commands":[
			{"kind":"place_purchase_order","sku":0,"role":"primary","qty":%d},
			{"kind":"place_purchase_order","sku":99,"role":"primary","qty":1}],"rationale":"buy everything"}`, sim.MaxOrderQuantity)
		stub := newStub(t, always(okReply(bad)))
		st := memory.New()
		svc, _, b := newLLMWorld(t, st, llmCfg(stub.srv.URL), app.ManagerAILLM)
		runTo(t, svc, b.ID, 8)
		recs, _ := svc.DecisionRecords(ctx, b.ID)
		if len(recs) != 3 {
			t.Fatalf("records = %d", len(recs))
		}
		for _, r := range recs {
			if r.Provenance != app.ProvAILLM || r.FallbackReason != "" || r.AcceptedCount != 0 || len(r.Proposed) != 2 || len(r.Rejected) != 2 {
				t.Fatalf("record = %+v", r)
			}
			for _, rj := range r.Rejected {
				if rj.Code == "" || rj.Message == "" {
					t.Fatalf("rejection without reason: %+v", rj)
				}
			}
		}
		if in, _ := st.InputsForBranch(ctx, b.ID, 1000); len(in) != 0 {
			t.Fatalf("rejected commands reached the input log: %d", len(in))
		}
		if got := stub.calls.Load(); got != 3 {
			t.Fatalf("provider requests = %d (a rejected proposal must not trigger a second request)", got)
		}
	})
	t.Run("empty proposal is recorded as ai_llm", func(t *testing.T) {
		stub := newStub(t, always(okReply(`{"commands":[],"rationale":"hold"}`)))
		svc, _, b := newLLMWorld(t, memory.New(), llmCfg(stub.srv.URL), app.ManagerAILLM)
		recs, _ := svc.DecisionRecords(ctx, b.ID)
		if len(recs) != 1 || recs[0].Provenance != app.ProvAILLM || recs[0].Rationale != "hold" || len(recs[0].Proposed) != 0 {
			t.Fatalf("records = %+v", recs)
		}
	})
}

func TestLLMBudget(t *testing.T) {
	ctx := context.Background()

	t.Run("call cap", func(t *testing.T) {
		cfg := llmCfg("")
		stub := newStub(t, always(okReply(validProposal)))
		cfg.BaseURL, cfg.MaxCallsPerWorld = stub.srv.URL, 3
		svc, w, b := newLLMWorld(t, memory.New(), cfg, app.ManagerAILLM)
		runTo(t, svc, b.ID, 24)
		recs, _ := svc.DecisionRecords(ctx, b.ID)
		if len(recs) != 7 {
			t.Fatalf("records = %d", len(recs))
		}
		for i, r := range recs {
			if i < 3 && r.Provenance != app.ProvAILLM {
				t.Fatalf("window %d = %+v", i, r)
			}
			if i >= 3 && (r.Provenance != app.ProvAIRuleBasedFallback || r.FallbackReason != "budget_exhausted" || r.LLMCallsCharged != 0) {
				t.Fatalf("window %d = %+v", i, r)
			}
		}
		if got := stub.calls.Load(); got != 3 {
			t.Fatalf("provider requests = %d, want 3", got)
		}
		s, _ := svc.LLMStatus(ctx, w.ID)
		if s.CallsMade != 3 || s.CallsRemaining != 0 || !s.Available || s.Model != testModel {
			t.Fatalf("status = %+v", s)
		}
	})

	t.Run("token cap", func(t *testing.T) {
		stub := newStub(t, always(stubReply{content: validProposal, prompt: 400, completion: 200}))
		cfg := llmCfg(stub.srv.URL)
		cfg.MaxTotalTokensPerWorld = 1000
		svc, w, b := newLLMWorld(t, memory.New(), cfg, app.ManagerAILLM)
		runTo(t, svc, b.ID, 16)
		recs, _ := svc.DecisionRecords(ctx, b.ID)
		// 600 after the first call, 1200 after the second: the third window is capped.
		for i, r := range recs {
			if i < 2 && r.Provenance != app.ProvAILLM {
				t.Fatalf("window %d = %+v", i, r)
			}
			if i >= 2 && (r.FallbackReason != "budget_exhausted" || r.Provenance != app.ProvAIRuleBasedFallback) {
				t.Fatalf("window %d = %+v", i, r)
			}
		}
		if got := stub.calls.Load(); got != 2 {
			t.Fatalf("provider requests = %d, want 2", got)
		}
		s, _ := svc.LLMStatus(ctx, w.ID)
		if s.TokensUsed != 1200 || s.TokensRemaining != 0 {
			t.Fatalf("status = %+v", s)
		}
	})

	t.Run("counter is per World, shared by its Branches", func(t *testing.T) {
		stub := newStub(t, always(okReply(validProposal)))
		cfg := llmCfg(stub.srv.URL)
		cfg.MaxCallsPerWorld = 3
		svc, w, root := newLLMWorld(t, memory.New(), cfg, app.ManagerHuman)
		var kids []app.Branch
		for i := 0; i < 5; i++ {
			k, err := svc.ForkBranch(ctx, root.ID, 0, fmt.Sprintf("llm-%d", i), app.ManagerAILLM)
			if err != nil {
				t.Fatal(err)
			}
			kids = append(kids, k)
		}
		var llmN, capped int
		for _, k := range kids {
			recs, _ := svc.DecisionRecords(ctx, k.ID)
			if len(recs) != 1 {
				t.Fatalf("branch %s records %d", k.Label, len(recs))
			}
			switch {
			case recs[0].Provenance == app.ProvAILLM:
				llmN++
			case recs[0].FallbackReason == "budget_exhausted":
				capped++
			}
		}
		if llmN != 3 || capped != 2 || stub.calls.Load() != 3 {
			t.Fatalf("llm %d capped %d requests %d", llmN, capped, stub.calls.Load())
		}
		if s, _ := svc.LLMStatus(ctx, w.ID); s.CallsMade != 3 {
			t.Fatalf("status = %+v", s)
		}
	})

	t.Run("concurrent windows never exceed the cap", func(t *testing.T) {
		stub := newStub(t, func(int64) stubReply {
			r := okReply(validProposal)
			r.delay = 20 * time.Millisecond
			return r
		})
		cfg := llmCfg(stub.srv.URL)
		cfg.MaxCallsPerWorld = 3
		st := memory.New()
		svc, w, root := newLLMWorld(t, st, cfg, app.ManagerHuman)
		obs, err := svc.ObservationAt(ctx, root.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		d := svc.LLMDeciderForTest(mustScenario(t))
		const n = 10
		out := make([]app.Decision, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b := app.Branch{ID: app.BranchID(fmt.Sprintf("b%d", i)), WorldID: w.ID, ManagerKind: app.ManagerAILLM}
				dec, err := d.Decide(ctx, b, obs, 5)
				if err != nil {
					t.Error(err)
				}
				out[i] = dec
			}()
		}
		wg.Wait()
		var llmN, capped int
		for _, dec := range out {
			switch {
			case dec.Provenance == app.ProvAILLM:
				llmN++
			case dec.FallbackReason == "budget_exhausted":
				capped++
			}
		}
		if llmN != 3 || capped != 7 || stub.calls.Load() != 3 {
			t.Fatalf("llm %d capped %d requests %d, want 3/7/3", llmN, capped, stub.calls.Load())
		}
		if s, _ := svc.LLMStatus(ctx, w.ID); s.CallsMade != 3 || s.CallsRemaining != 0 {
			t.Fatalf("status = %+v", s)
		}
	})
}

// The per-World counters are rebuilt from the stored records after a restart.
func TestLLMUsageSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	stub := newStub(t, func(n int64) stubReply {
		if n%2 == 0 {
			return stubReply{status: 500}
		}
		return okReply(validProposal)
	})
	cfg := llmCfg(stub.srv.URL)
	cfg.MaxCallsPerWorld = 5
	st := memory.New()
	svc, w, b := newLLMWorld(t, st, cfg, app.ManagerAILLM)
	runTo(t, svc, b.ID, 12)
	before, _ := svc.LLMStatus(ctx, w.ID)
	if before.CallsMade != 4 || before.TokensUsed != 2*150 {
		t.Fatalf("before = %+v", before)
	}

	cold := app.NewBranchService(st, nil, nil, withLLM(t, cfg))
	after, err := cold.LLMStatus(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("after restart %+v, before %+v", after, before)
	}
	// The cap carries over: one more call, then budget_exhausted.
	runTo(t, cold, b.ID, 20)
	recs, _ := cold.DecisionRecords(ctx, b.ID)
	if got := stub.calls.Load(); got != 5 {
		t.Fatalf("provider requests = %d, want 5", got)
	}
	if last := recs[len(recs)-1]; last.FallbackReason != "budget_exhausted" {
		t.Fatalf("last record = %+v", last)
	}
}

func TestLLMReplayNoCall(t *testing.T) {
	ctx := context.Background()
	stub := newStub(t, always(okReply(validProposal)))
	cfg := llmCfg(stub.srv.URL)
	st := memory.New()
	svc, _, b := newLLMWorld(t, st, cfg, app.ManagerAILLM)
	runTo(t, svc, b.ID, 24)
	if stub.calls.Load() == 0 {
		t.Fatal("the LLM was never called")
	}
	want := make([][32]byte, 25)
	for i := range want {
		h, err := svc.HashAt(ctx, b.ID, sim.Tick(i))
		if err != nil {
			t.Fatal(err)
		}
		want[i] = h
	}
	before := stub.calls.Load()

	// A cold service over the same stored log, with the provider still reachable.
	cold := app.NewBranchService(st, nil, nil, withLLM(t, cfg))
	for i := range want {
		h, err := cold.HashAt(ctx, b.ID, sim.Tick(i))
		if err != nil {
			t.Fatal(err)
		}
		if h != want[i] {
			t.Fatalf("tick %d: replayed hash differs", i)
		}
		if _, err := cold.StateAt(ctx, b.ID, sim.Tick(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Rewinding and forking a Human Branch from it replays without a request too.
	if _, err := cold.ForkBranch(ctx, b.ID, 12, "human", app.ManagerHuman); err != nil {
		t.Fatal(err)
	}
	if got := stub.calls.Load(); got != before {
		t.Fatalf("replay made %d provider requests", got-before)
	}
}
