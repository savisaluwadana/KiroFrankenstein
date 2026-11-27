package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

func TestManagers(t *testing.T) {
	list := func(h http.Handler) (map[string]obj, string) {
		rec := do(t, h, "GET", "/managers", nil)
		body := mustStatus(t, rec, http.StatusOK)
		out := map[string]obj{}
		for _, m := range body["managers"].([]any) {
			mm := m.(map[string]any)
			out[mm["kind"].(string)] = obj(mm)
		}
		return out, rec.Body.String()
	}
	got, _ := list(newHandler(t, nil))
	if len(got) != 3 || got["human"]["available"] != true || got["ai_rule_based"]["available"] != true ||
		got["ai_rule_based"]["name"] != "Rule-based AI" {
		t.Fatalf("managers = %v", got)
	}
	l := got["ai_llm"]
	if l["name"] != "LLM AI" || l["available"] != false || l["unavailableReason"] != "not configured" {
		t.Fatalf("llm = %v", l)
	}
	if _, ok := l["model"]; ok {
		t.Fatal("model present without configuration")
	}

	const key = "sk-test-SECRET-xyz"
	stub := httptest.NewServer(http.NotFoundHandler())
	defer stub.Close()
	opt, err := app.WithLLM(llm.Config{
		BaseURL: stub.URL, APIKey: key, Model: "test/model-1", RequestTimeout: time.Second,
		MaxCallsPerWorld: 7, MaxTokensPerCall: 256, MaxTotalTokensPerWorld: 1000,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, body := list(newHandler(t, func(o *httpapi.Options) { o.Service = app.NewBranchService(memory.New(), nil, nil, opt) }))
	l = got["ai_llm"]
	if l["available"] != true || l["model"] != "test/model-1" {
		t.Fatalf("llm = %v", l)
	}
	if _, ok := l["unavailableReason"]; ok {
		t.Fatal("unavailableReason present when available")
	}
	if strings.Contains(body, key) || strings.Contains(body, stub.URL) {
		t.Fatal("managers response leaks the key or the endpoint")
	}
}

func TestWorldReadReportsLLMUnavailable(t *testing.T) {
	h := newHandler(t, nil)
	w := mustStatus(t, do(t, h, "GET", "/worlds/"+demo(t, h)["world"].(obj)["id"].(string), nil), http.StatusOK)
	if w["llmAvailable"] != false || num(t, w, "llmCallsMade") != 0 || num(t, w, "llmCallsRemaining") != 0 {
		t.Fatalf("world = %v", w)
	}
	if _, ok := w["llmModel"]; ok {
		t.Fatal("llmModel present without configuration")
	}
}

func TestWorldReadReportsLLMUsageWithoutKey(t *testing.T) {
	const key = "sk-test-SECRET-xyz"
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"commands\":[],\"rationale\":\"hold\"}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer stub.Close()
	opt, err := app.WithLLM(llm.Config{
		BaseURL: stub.URL, APIKey: key, Model: "test/model-1", RequestTimeout: 2 * time.Second,
		MaxCallsPerWorld: 7, MaxTokensPerCall: 256, MaxTotalTokensPerWorld: 1000,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newHandler(t, func(o *httpapi.Options) { o.Service = app.NewBranchService(memory.New(), nil, nil, opt) })

	rec := do(t, h, "POST", "/worlds", map[string]any{"name": "llm", "seed": 1, "managerKind": "ai_llm"})
	w := mustStatus(t, rec, http.StatusCreated)["world"].(obj)
	got := mustStatus(t, do(t, h, "GET", "/worlds/"+w["id"].(string), nil), http.StatusOK)
	if got["llmAvailable"] != true || got["llmModel"] != "test/model-1" ||
		num(t, got, "llmCallsMade") != 1 || num(t, got, "llmCallsRemaining") != 6 ||
		num(t, got, "llmTokensUsed") != 15 || num(t, got, "llmTokensRemaining") != 985 {
		t.Fatalf("world = %v", got)
	}
	body := do(t, h, "GET", "/worlds/"+w["id"].(string), nil).Body.String()
	if strings.Contains(body, key) || strings.Contains(body, stub.URL) {
		t.Fatal("world read leaks the key or the endpoint")
	}
}
