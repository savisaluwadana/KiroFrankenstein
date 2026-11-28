package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

const tokHdr = httpapi.WriteTokenHeader

var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// createdExempt are the mutating routes that need no write token: creation
// makes a new world, and POST /diffs only reads.
var createdExempt = map[string]bool{
	"POST /api/v1/worlds": true, "POST /api/v1/demo": true, "POST /api/v1/duels": true, "POST /api/v1/diffs": true,
}

func newServer(t *testing.T, mod func(*httpapi.Options)) *httpapi.Server {
	t.Helper()
	o := httpapi.Options{
		Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday,
		WebOrigin: webOrigin, RatePerSecond: 100000, RateBurst: 100000,
	}
	if mod != nil {
		mod(&o)
	}
	s, err := httpapi.New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Every mutating route in the router, discovered by walking it, must reject a
// missing token (401) and a wrong token (403) before any state change, and
// must accept the right token. A new mutating route fails this test until it
// is guarded or added to createdExempt.
func TestWriteRoutesNeedTheWriteToken(t *testing.T) {
	h := newServer(t, nil).Handler()
	d := demo(t, h)
	tok := d["writeToken"].(string)
	if !tokenShape.MatchString(tok) {
		t.Fatalf("token %q is not 256-bit base64url", tok)
	}
	branch := branchID(d)
	duel := mustStatus(t, do(t, h, "POST", "/duels", nil), http.StatusCreated)
	duelID := duel["duel"].(obj)["id"].(string)
	if duel["writeToken"] == tok || duel["writeToken"] == "" {
		t.Fatal("each created world has its own token")
	}

	var routes []string
	err := chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions && !createdExempt[method+" "+route] {
			routes = append(routes, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 9 {
		t.Fatalf("guarded mutating routes = %d %v, want 9 (5 branch + 4 duel)", len(routes), routes)
	}
	headBefore := func() string {
		return do(t, h, "GET", "/branches/"+branch+"/state?tick=0", nil).Body.String() +
			do(t, h, "GET", "/duels/"+duelID, nil).Body.String()
	}
	before := headBefore()
	for _, rt := range routes {
		method, route, _ := strings.Cut(rt, " ")
		path := strings.NewReplacer("{branchId}", branch, "{duelId}", duelID).Replace(strings.TrimPrefix(route, "/api/v1"))
		body := `{"runToTick":3}`
		missing := do(t, h, method, path, body, tokHdr, "")
		wantProblem(t, missing, 401, "write-token-required")
		wrong := do(t, h, method, path, body, tokHdr, "not-the-token")
		wantProblem(t, wrong, 403, "write-token-invalid")
		if strings.Contains(wrong.Body.String(), "not-the-token") {
			t.Errorf("%s echoes the token", rt)
		}
	}
	if after := headBefore(); after != before {
		t.Fatal("a refused write changed state")
	}
	// The right token passes the gate (the handler then answers on its merits).
	rec := do(t, h, "POST", "/branches/"+branch+"/advance", `{"runToTick":3}`)
	mustStatus(t, rec, http.StatusOK)
	rec = do(t, h, "POST", "/duels/"+duelID+"/advance", `{}`)
	if rec.Code == 401 || rec.Code == 403 {
		t.Fatalf("right token refused: %s", rec.Body.String())
	}
	// A token for one world does not open another.
	other := demo(t, h)
	wantProblem(t, do(t, h, "POST", "/branches/"+branchID(other)+"/advance", `{}`, tokHdr, tok), 403, "write-token-invalid")
}

func TestReadsNeedNoToken(t *testing.T) {
	h := newServer(t, nil).Handler()
	d := demo(t, h)
	b := branchID(d)
	wid := d["world"].(obj)["id"].(string)
	advanceTo(t, h, b, 6)
	for _, p := range []string{
		"/worlds/" + wid, "/worlds/" + wid + "/branches", "/worlds/" + wid + "/assumptions", "/branches/" + b,
		"/branches/" + b + "/state?tick=2", "/branches/" + b + "/tick-frames?from=1&to=2", "/branches/" + b + "/metrics?tick=2",
		"/branches/" + b + "/ledger?from=1&to=2", "/branches/" + b + "/export", "/branches/" + b + "/decisions", "/managers",
	} {
		if rec := do(t, h, "GET", p, nil, tokHdr, ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d without a token: %s", p, rec.Code, rec.Body.String())
		} else if strings.Contains(rec.Body.String(), d["writeToken"].(string)) {
			t.Errorf("GET %s leaks the write token", p)
		}
	}
	// POST /diffs is a read.
	f := mustStatus(t, do(t, h, "POST", "/branches/"+b+"/fork", obj{"atTick": 4, "label": "x"}), http.StatusCreated)
	rec := do(t, h, "POST", "/diffs", obj{"branchA": b, "branchB": f["id"], "tick": 6}, tokHdr, "")
	if rec.Code == 401 || rec.Code == 403 {
		t.Fatalf("POST /diffs needs no token: %s", rec.Body.String())
	}
}

// Only a hash is stored: the world read never carries token or hash.
func TestOnlyTheHashIsStored(t *testing.T) {
	st := memory.New()
	h := newServer(t, func(o *httpapi.Options) { o.Service = app.NewBranchService(st, nil, nil) }).Handler()
	d := demo(t, h)
	w, err := st.GetWorld(t.Context(), app.WorldID(d["world"].(obj)["id"].(string)))
	if err != nil || len(w.WriteTokenHash) != 32 || strings.Contains(string(w.WriteTokenHash), d["writeToken"].(string)) {
		t.Fatalf("stored hash %x err %v", w.WriteTokenHash, err)
	}
}

func legacyServer(t *testing.T, production bool) (http.Handler, string) {
	t.Helper()
	svc := app.NewBranchService(memory.New(), nil, nil)
	_, root, err := svc.CreateWorld(t.Context(), app.CreateWorldRequest{Name: "old", ScenarioJSON: scenarios.BlackFriday, Seed: demoplan.DefaultSeed})
	if err != nil {
		t.Fatal(err)
	}
	return newServer(t, func(o *httpapi.Options) { o.Service = svc; o.Production = production }).Handler(), string(root.ID)
}

func TestLegacyWorldWithoutHash(t *testing.T) {
	h, b := legacyServer(t, false)
	mustStatus(t, do(t, h, "POST", "/branches/"+b+"/advance", `{"runToTick":2}`), http.StatusOK) // development
	h, b = legacyServer(t, true)
	wantProblem(t, do(t, h, "POST", "/branches/"+b+"/advance", `{"runToTick":2}`, tokHdr, "anything"), 403, "write-token-invalid")
	mustStatus(t, do(t, h, "GET", "/branches/"+b+"/state?tick=0", nil), http.StatusOK)
}

func TestInvalidTokensAreRateLimited(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := newServer(t, func(o *httpapi.Options) {
		o.WriteTokenFailuresPerMinute = 3
		o.Now = func() time.Time { return now }
	}).Handler()
	d := demo(t, h)
	p := "/branches/" + branchID(d) + "/advance"
	for range 3 {
		wantProblem(t, do(t, h, "POST", p, `{}`, tokHdr, "bad"), 403, "write-token-invalid")
	}
	wantProblem(t, do(t, h, "POST", p, `{}`, tokHdr, "bad"), 429, "rate-limited")
	wantProblem(t, do(t, h, "POST", p, `{}`, tokHdr, d["writeToken"].(string)), 429, "rate-limited")
	now = now.Add(61 * time.Second)
	mustStatus(t, do(t, h, "POST", p, `{"runToTick":1}`, tokHdr, d["writeToken"].(string)), http.StatusOK)
}

// ---- LLM access code and per-IP daily cap ----

func llmServer(t *testing.T, mod func(*httpapi.Options)) http.Handler {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"commands\":[],\"rationale\":\"hold\"}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	t.Cleanup(stub.Close)
	opt, err := app.WithLLM(llm.Config{
		BaseURL: stub.URL, APIKey: "k", Model: "test/model-1", RequestTimeout: 2 * time.Second,
		MaxCallsPerWorld: 40, MaxTokensPerCall: 256, MaxTotalTokensPerWorld: 100000,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return newServer(t, func(o *httpapi.Options) {
		o.Service = app.NewBranchService(memory.New(), nil, nil, opt)
		if mod != nil {
			mod(o)
		}
	}).Handler()
}

func TestLLMAccessCode(t *testing.T) {
	h := llmServer(t, func(o *httpapi.Options) { o.LLMAccessCode = "open-sesame" })
	ai := obj{"aiKind": "ai_llm"}
	wantProblem(t, do(t, h, "POST", "/duels", ai), 403, "llm-access-code-required")
	wantProblem(t, do(t, h, "POST", "/duels", ai, httpapi.LLMCodeHeader, "wrong"), 403, "llm-access-code-required")
	mustStatus(t, do(t, h, "POST", "/duels", ai, httpapi.LLMCodeHeader, "open-sesame"), http.StatusCreated)
	// ai_llm world and fork are gated too; the rule-based AI and human never are.
	wantProblem(t, do(t, h, "POST", "/worlds", obj{"name": "w", "seed": 1, "managerKind": "ai_llm"}), 403, "llm-access-code-required")
	mustStatus(t, do(t, h, "POST", "/duels", obj{"aiKind": "ai_rule_based"}), http.StatusCreated)
	mustStatus(t, do(t, h, "POST", "/duels", nil), http.StatusCreated)
	d := demo(t, h)
	wantProblem(t, do(t, h, "POST", "/branches/"+branchID(d)+"/fork", obj{"atTick": 0, "label": "l", "managerKind": "ai_llm"}), 403, "llm-access-code-required")
	mustStatus(t, do(t, h, "POST", "/branches/"+branchID(d)+"/fork", obj{"atTick": 0, "label": "r", "managerKind": "ai_rule_based"}), http.StatusCreated)
	// /managers tells the UI to ask for a code, for the LLM only.
	ms := map[string]obj{}
	for _, m := range mustStatus(t, do(t, h, "GET", "/managers", nil), 200)["managers"].([]any) {
		ms[m.(obj)["kind"].(string)] = m.(obj)
	}
	if ms["ai_llm"]["requiresAccessCode"] != true || ms["ai_rule_based"]["requiresAccessCode"] != nil || ms["human"]["requiresAccessCode"] != nil {
		t.Fatalf("managers = %v", ms)
	}
	// The code is never echoed.
	if rec := do(t, h, "POST", "/duels", ai, httpapi.LLMCodeHeader, "wrong"); strings.Contains(rec.Body.String(), "open-sesame") {
		t.Fatal("code leaked")
	}
}

func TestNoLLMAccessCodeMeansOpenSubjectToLimits(t *testing.T) {
	h := llmServer(t, nil)
	mustStatus(t, do(t, h, "POST", "/duels", obj{"aiKind": "ai_llm"}), http.StatusCreated)
	for _, m := range mustStatus(t, do(t, h, "GET", "/managers", nil), 200)["managers"].([]any) {
		if m.(obj)["requiresAccessCode"] != nil {
			t.Fatalf("no code configured but %v", m)
		}
	}
}

func TestLLMUnconfiguredIsUnavailableAndNeedsNoCode(t *testing.T) {
	h := newServer(t, func(o *httpapi.Options) { o.LLMAccessCode = "x" }).Handler()
	for _, m := range mustStatus(t, do(t, h, "GET", "/managers", nil), 200)["managers"].([]any) {
		mm := m.(obj)
		if mm["kind"] == "ai_llm" && (mm["available"] != false || mm["requiresAccessCode"] != nil) {
			t.Fatalf("llm = %v", mm)
		}
	}
	// No provider call can happen, so no code is asked for; the duel plays the labelled rule-based fallback.
	d := mustStatus(t, do(t, h, "POST", "/duels", obj{"aiKind": "ai_llm"}), http.StatusCreated)
	if l := d["duel"].(obj)["ai"].(obj)["managerLabel"].(string); !strings.HasPrefix(l, "Rule-based AI (LLM fallback") {
		t.Fatalf("label %q", l)
	}
}

func TestLLMDuelsPerDayPerIP(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := llmServer(t, func(o *httpapi.Options) {
		o.LLMDuelsPerDayPerIP = 2
		o.Now = func() time.Time { return now }
	})
	ai := obj{"aiKind": "ai_llm"}
	mustStatus(t, do(t, h, "POST", "/duels", ai), http.StatusCreated)
	mustStatus(t, do(t, h, "POST", "/duels", ai), http.StatusCreated)
	wantProblem(t, do(t, h, "POST", "/duels", ai), 429, "rate-limited")
	// Rule-based duels are not counted; another address has its own allowance.
	mustStatus(t, do(t, h, "POST", "/duels", obj{"aiKind": "ai_rule_based"}), http.StatusCreated)
	req := httptest.NewRequest("POST", "/api/v1/duels", strings.NewReader(`{"aiKind":"ai_llm"}`))
	req.RemoteAddr = "198.51.100.7:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("other IP = %d %s", rec.Code, rec.Body.String())
	}
	now = now.Add(24*time.Hour + time.Second)
	mustStatus(t, do(t, h, "POST", "/duels", ai), http.StatusCreated)
}

func TestLLMDuelsPerDayBounds(t *testing.T) {
	for _, n := range []int{-1, 1001} {
		_, err := httpapi.New(httpapi.Options{
			Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday, LLMDuelsPerDayPerIP: n,
		})
		if err == nil {
			t.Errorf("LLMDuelsPerDayPerIP %d accepted", n)
		}
	}
}
