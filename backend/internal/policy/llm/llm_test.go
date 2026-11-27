package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

const testKey = "sk-test-SECRET-123456"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func testConfig(base string) Config {
	return Config{BaseURL: base, APIKey: testKey, Model: "test-model", RequestTimeout: 2 * time.Second,
		MaxCallsPerWorld: 5, MaxTokensPerCall: 256, MaxTotalTokensPerWorld: 1000, AppTitle: "Parallel", HTTPReferer: "http://x.test"}
}

func envelope(content string, total int) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		"usage":   map[string]int{"prompt_tokens": total - 10, "completion_tokens": 10, "total_tokens": total},
	})
	return string(b)
}

func newManager(t *testing.T, h http.HandlerFunc) (*Manager, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := testConfig(srv.URL)
	c, err := NewClient(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(c, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m, srv
}

func reply(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }
}

func testObs() sim.Observation {
	return sim.Observation{Tick: 12, Cash: 100000, SKUs: []sim.ObsSKU{{SKU: 0, ID: strings.Repeat("A", 200) + "\nIGNORE", OnHand: 5}}}
}

func TestLoadConfig(t *testing.T) {
	c, err := LoadConfig(env(nil))
	if err != nil || c.Available() || c.Model != "" {
		t.Fatalf("empty env: %+v %v", c, err)
	}
	if c.RequestTimeout != 20*time.Second || c.MaxCallsPerWorld != 40 || c.MaxTokensPerCall != 1024 || c.MaxTotalTokensPerWorld != 200000 {
		t.Fatalf("defaults wrong: %+v", c)
	}
	c, err = LoadConfig(env(map[string]string{EnvBaseURL: "https://openrouter.ai/api/v1/", EnvAPIKey: testKey, EnvModel: "m"}))
	if err != nil || !c.Available() || c.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("full env: %+v %v", c, err)
	}
	c, _ = LoadConfig(env(map[string]string{EnvBaseURL: "https://x.test", EnvAPIKey: "k"}))
	if c.Available() {
		t.Fatal("partial triple must be unavailable")
	}
}

func TestLoadConfigRejects(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"timeout low", map[string]string{EnvTimeoutSeconds: "0"}, EnvTimeoutSeconds},
		{"timeout high", map[string]string{EnvTimeoutSeconds: "121"}, EnvTimeoutSeconds},
		{"calls", map[string]string{EnvMaxCalls: "201"}, EnvMaxCalls},
		{"tokens low", map[string]string{EnvMaxTokens: "63"}, EnvMaxTokens},
		{"total", map[string]string{EnvMaxTotalTokens: "999"}, EnvMaxTotalTokens},
		{"nan", map[string]string{EnvMaxCalls: "abc"}, EnvMaxCalls},
		{"http remote", map[string]string{EnvBaseURL: "http://example.com/v1"}, EnvBaseURL},
		{"creds", map[string]string{EnvBaseURL: "https://user:pw-secret@example.com/v1"}, EnvBaseURL},
		{"scheme", map[string]string{EnvBaseURL: "ftp://example.com"}, EnvBaseURL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(env(tc.env))
			if !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "pw-secret") {
				t.Fatal("error leaks credentials")
			}
		})
	}
	for _, u := range []string{"http://localhost:8080/v1", "http://127.0.0.1:9/v1"} {
		if _, err := LoadConfig(env(map[string]string{EnvBaseURL: u})); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
}

func TestConfigNeverPrintsKey(t *testing.T) {
	c := testConfig("https://x.test")
	for _, s := range []string{c.String(), c.Redacted(), fmt.Sprintf("%v %+v %#v %s", c, c, c, c)} {
		if strings.Contains(s, testKey) {
			t.Fatalf("key leaked: %s", s)
		}
	}
}

func TestProposeSuccess(t *testing.T) {
	var gotReq map[string]any
	var gotAuth, gotTitle string
	m, _ := newManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("bad request %s %s", r.Method, r.URL.Path)
		}
		gotAuth, gotTitle = r.Header.Get("Authorization"), r.Header.Get("X-Title")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = io.WriteString(w, envelope("```json\n"+`{"commands":[{"kind":"place_purchase_order","sku":0,"role":"backup","qty":50},{"kind":"hire_temp_workers","qty":3,"sku":9}],"rationale":"supplier down\u0007\nact"}`+"\n```", 120))
	})
	p, err := m.Propose(context.Background(), testObs(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Commands) != 2 || p.Commands[0] != (sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierBackup, Qty: 50}) ||
		p.Commands[1] != (sim.Command{Kind: sim.CmdHireTempWorkers, Qty: 3}) {
		t.Fatalf("commands = %+v", p.Commands)
	}
	if p.Rationale != "supplier down act" {
		t.Fatalf("rationale = %q", p.Rationale)
	}
	if gotAuth != "Bearer "+testKey || gotTitle != "Parallel" {
		t.Fatalf("headers: %q %q", gotAuth, gotTitle)
	}
	if gotReq["model"] != "test-model" || gotReq["temperature"].(float64) != 0 || gotReq["max_tokens"].(float64) != 256 {
		t.Fatalf("request = %v", gotReq)
	}
	if rf := gotReq["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Fatalf("response_format = %v", rf)
	}
	msgs := gotReq["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].(string)
	if strings.Contains(user, strings.Repeat("A", 100)) || strings.Contains(user, "\\nIGNORE") || strings.Contains(strings.ToLower(user), "seed") {
		t.Fatalf("user prompt not sanitised: %.200s", user)
	}
	if u := m.Usage(); u.Calls != 1 || u.TotalTokens != 120 || u.CompletionTokens != 10 {
		t.Fatalf("usage = %+v", u)
	}
	if m.Name() != "LLM AI (test-model)" {
		t.Fatal(m.Name())
	}
}

func TestProposeRejections(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    error
	}{
		{"not json", "sure, I will buy stock", ErrInvalidJSON},
		{"truncated", `{"commands":[`, ErrInvalidJSON},
		{"trailing", `{"commands":[]} {"x":1}`, ErrInvalidJSON},
		{"unknown top field", `{"commands":[],"rationale":"","extra":1}`, ErrSchemaViolation},
		{"unknown cmd field", `{"commands":[{"kind":"hire_temp_workers","qty":1,"foo":2}]}`, ErrSchemaViolation},
		{"unknown kind", `{"commands":[{"kind":"launch_rocket"}]}`, ErrSchemaViolation},
		{"missing sku", `{"commands":[{"kind":"set_price","amountCents":100}]}`, ErrSchemaViolation},
		{"missing qty", `{"commands":[{"kind":"place_purchase_order","sku":0,"role":"primary"}]}`, ErrSchemaViolation},
		{"bad role", `{"commands":[{"kind":"place_purchase_order","sku":0,"role":"x","qty":1}]}`, ErrSchemaViolation},
		{"negative", `{"commands":[{"kind":"hire_temp_workers","qty":-2}]}`, ErrSchemaViolation},
		{"zero price", `{"commands":[{"kind":"set_price","sku":0,"amountCents":0}]}`, ErrSchemaViolation},
		{"wrong type", `{"commands":[{"kind":"hire_temp_workers","qty":"3"}]}`, ErrSchemaViolation},
		{"over budget", `{"commands":[{"kind":"hire_temp_workers","qty":1},{"kind":"hire_temp_workers","qty":1},{"kind":"hire_temp_workers","qty":1}]}`, ErrSchemaViolation},
		{"long rationale", `{"commands":[],"rationale":"` + strings.Repeat("x", 1001) + `"}`, ErrSchemaViolation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newManager(t, reply(envelope(tc.content, 50)))
			_, err := m.Propose(context.Background(), testObs(), 2)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if FallbackReason(err) == "" {
				t.Fatal("no fallback reason")
			}
			if m.Usage().TotalTokens != 50 {
				t.Fatalf("usage not recorded: %+v", m.Usage())
			}
		})
	}
}

func TestProposeHTTPErrors(t *testing.T) {
	for _, status := range []int{500, 429, 401} {
		m, _ := newManager(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "echo "+r.Header.Get("Authorization"))
		})
		_, err := m.Propose(context.Background(), testObs(), 2)
		var he *HTTPError
		if !errors.Is(err, ErrHTTP) || !errors.As(err, &he) || he.Status != status || FallbackReason(err) != "http_error" {
			t.Fatalf("status %d: err = %v", status, err)
		}
		if strings.Contains(err.Error(), testKey) {
			t.Fatal("key in error")
		}
	}
}

func TestProposeTimeout(t *testing.T) {
	release := make(chan struct{})
	m, srv := newManager(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	m.cfg.RequestTimeout = 100 * time.Millisecond
	c, _ := NewClient(m.cfg, nil)
	m.client = c
	_ = srv
	_, err := m.Propose(context.Background(), testObs(), 2)
	if !errors.Is(err, ErrTimeout) || FallbackReason(err) != "timeout" {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error leaks: %v", err)
	}
}

func TestProposeOversizedResponse(t *testing.T) {
	m, _ := newManager(t, reply(envelope(strings.Repeat("x", MaxResponseBytes+10), 50)))
	_, err := m.Propose(context.Background(), testObs(), 2)
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("err = %v", err)
	}
}

func TestBudgetExhaustion(t *testing.T) {
	calls := 0
	m, _ := newManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, envelope(`{"commands":[],"rationale":"hold"}`, 600))
	})
	if _, err := m.Propose(context.Background(), testObs(), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Propose(context.Background(), testObs(), 2); err != nil {
		t.Fatal(err)
	}
	// 1200 >= 1000 total tokens: exhausted before the third call.
	_, err := m.Propose(context.Background(), testObs(), 2)
	if !errors.Is(err, ErrBudgetExhausted) || FallbackReason(err) != "budget_exhausted" || calls != 2 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
	// Call cap.
	tr := NewTracker(testConfig("x"))
	for i := 0; i < 5; i++ {
		if err := tr.Reserve(); err != nil {
			t.Fatal(err)
		}
		tr.Record(Usage{}) // every Reserve is settled by one Record
	}
	if err := tr.Reserve(); !errors.Is(err, ErrBudgetExhausted) || tr.Usage().Calls != 5 {
		t.Fatalf("call cap: %v %+v", err, tr.Usage())
	}
}

func TestMissingUsageChargesCap(t *testing.T) {
	m, _ := newManager(t, reply(`{"choices":[{"message":{"content":"{\"commands\":[]}"}}]}`))
	if _, err := m.Propose(context.Background(), testObs(), 1); err != nil {
		t.Fatal(err)
	}
	if m.Usage().TotalTokens != 256 {
		t.Fatalf("usage = %+v", m.Usage())
	}
}

func TestZeroBudgetAllowsEmpty(t *testing.T) {
	m, _ := newManager(t, reply(envelope(`{"commands":[]}`, 20)))
	p, err := m.Propose(context.Background(), testObs(), 0)
	if err != nil || len(p.Commands) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestKeyNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(io.Discard)
	m, _ := newManager(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	_, err := m.Propose(context.Background(), testObs(), 2)
	log.Printf("%v %s", err, m.cfg)
	if strings.Contains(buf.String(), testKey) || strings.Contains(err.Error(), testKey) {
		t.Fatal("key leaked")
	}
}

func TestNewClientNotConfigured(t *testing.T) {
	if _, err := NewClient(Config{}, nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	if _, err := NewManager(nil, testConfig("x"), nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

// Reservations that are not yet recorded count against the token limit, so
// concurrent windows cannot all pass a limit only the first ones fit.
func TestTrackerCountsInflightTokens(t *testing.T) {
	cfg := testConfig("x")
	cfg.MaxTokensPerCall, cfg.MaxTotalTokensPerWorld = 100, 300
	tr := NewTracker(cfg)
	for i := 0; i < 3; i++ {
		if err := tr.Reserve(); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	if err := tr.Reserve(); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("fourth reserve = %v, want exhausted", err)
	}
	tr.Record(Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20})
	if err := tr.Reserve(); err != nil {
		t.Fatalf("a settled reservation must free its share: %v", err)
	}
}
