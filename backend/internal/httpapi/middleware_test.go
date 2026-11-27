package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

// rawReq sends a request to an absolute path (no /api/v1 prefix) from peer.
func rawReq(h http.Handler, method, path, peer string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if peer != "" {
		req.RemoteAddr = peer
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSecurityHeaders(t *testing.T) {
	plain := newHandler(t, nil)
	prod := newHandler(t, func(o *httpapi.Options) { o.Production = true })

	for _, path := range []string{"/api/v1/health", "/api/v1/nope", "/healthz", "/readyz"} {
		rec := rawReq(plain, "GET", path, "")
		want := map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
			"Cache-Control":           "no-store",
			"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s: %s = %q, want %q", path, k, got, v)
			}
		}
		if rec.Header().Get("Strict-Transport-Security") != "" {
			t.Errorf("%s: HSTS sent outside production", path)
		}
	}
	// A plain-HTTP production request gets no HSTS; one behind an HTTPS
	// terminator does.
	if got := rawReq(prod, "GET", "/api/v1/health", "").Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS over plain http = %q", got)
	}
	if got := rawReq(prod, "GET", "/api/v1/health", "", "X-Forwarded-Proto", "https").Header().Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=") {
		t.Errorf("HSTS behind https = %q", got)
	}
	if got := rawReq(plain, "GET", "/api/v1/health", "", "X-Forwarded-Proto", "https").Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS outside production = %q", got)
	}
}

func TestCORSStrict(t *testing.T) {
	h := newHandler(t, nil)
	rec := rawReq(h, "GET", "/api/v1/health", "", "Origin", webOrigin)
	if rec.Header().Get("Access-Control-Allow-Origin") != webOrigin {
		t.Errorf("allowed origin not echoed")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("credentials must never be allowed")
	}
	for _, o := range []string{"http://localhost:3000.evil.test", "https://localhost:3000", "http://LOCALHOST:3000", "null", webOrigin + "/"} {
		rec := rawReq(h, "GET", "/api/v1/health", "", "Origin", o)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("origin %q was allowed", o)
		}
	}
	pre := rawReq(h, "OPTIONS", "/api/v1/worlds", "", "Origin", webOrigin, "Access-Control-Request-Method", "POST")
	if pre.Code != http.StatusNoContent || pre.Header().Get("Access-Control-Max-Age") == "" ||
		pre.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("preflight = %d %v", pre.Code, pre.Header())
	}
	bad := rawReq(h, "OPTIONS", "/api/v1/worlds", "", "Origin", "https://evil.test", "Access-Control-Request-Method", "POST")
	if bad.Header().Get("Access-Control-Allow-Origin") != "" || bad.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("foreign preflight got CORS headers: %v", bad.Header())
	}
	for _, bad := range []string{"*", "https://*.example.com", "ftp://example.com", "javascript:alert(1)", "http://x/path", "http://x/", "example.com", "http://u:p@x"} {
		_, err := httpapi.New(httpapi.Options{Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday, WebOrigin: bad})
		if err == nil {
			t.Errorf("WebOrigin %q accepted", bad)
		}
	}
}

func TestTrustedProxy(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	build := func(hops int) http.Handler {
		return newHandler(t, func(o *httpapi.Options) {
			o.RatePerSecond, o.RateBurst = 0.001, 1
			o.Now = func() time.Time { return now }
			o.TrustedProxyHops = hops
		})
	}
	get := func(h http.Handler, peer string, xff ...string) int {
		var hdr []string
		for _, v := range xff {
			hdr = append(hdr, "X-Forwarded-For", v)
		}
		return rawReq(h, "GET", "/api/v1/health", peer, hdr...).Code
	}
	// Default: X-Forwarded-For is ignored; the peer address is the key.
	h := build(0)
	if get(h, "192.0.2.1:1", "203.0.113.1") != 200 || get(h, "192.0.2.1:2", "203.0.113.99") != 429 {
		t.Error("hops=0 must key on the peer address and ignore X-Forwarded-For")
	}
	if get(h, "192.0.2.2:1", "203.0.113.1") != 200 {
		t.Error("another peer has its own bucket")
	}
	// One trusted proxy: the rightmost entry is the client; spoofed entries
	// to its left do not change the key.
	h = build(1)
	if get(h, "10.0.0.1:1", "1.1.1.1, 203.0.113.5") != 200 {
		t.Fatal("first request")
	}
	if get(h, "10.0.0.1:1", "9.9.9.9, 203.0.113.5") != 429 {
		t.Error("spoofed left entry must not create a new bucket")
	}
	if get(h, "10.0.0.1:1", "1.1.1.1, 203.0.113.6") != 200 {
		t.Error("a different rightmost entry is a different client")
	}
	// Two trusted proxies: the second entry from the right.
	h = build(2)
	if get(h, "10.0.0.1:1", "198.51.100.1, 203.0.113.5, 10.0.0.9") != 200 ||
		get(h, "10.0.0.1:1", "6.6.6.6, 203.0.113.5, 10.0.0.8") != 429 {
		t.Error("hops=2 must key on the second entry from the right")
	}
	// Too few entries or a malformed entry fall back to the peer address.
	h = build(2)
	if get(h, "10.0.0.1:1", "203.0.113.5") != 200 || get(h, "10.0.0.1:2", "203.0.113.77") != 429 {
		t.Error("short chain must fall back to the peer address")
	}
	h = build(1)
	if get(h, "10.0.0.1:1", "not-an-ip") != 200 || get(h, "10.0.0.1:2", "also-bad") != 429 {
		t.Error("malformed entry must fall back to the peer address")
	}
	// Bounds.
	for _, n := range []int{-1, 6} {
		_, err := httpapi.New(httpapi.Options{Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday, TrustedProxyHops: n})
		if err == nil {
			t.Errorf("TrustedProxyHops %d accepted", n)
		}
	}
}

func TestRateLimitWorldCreate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := newHandler(t, func(o *httpapi.Options) {
		o.WorldCreatePerHourPerIP = 2
		o.Now = func() time.Time { return now }
	})
	mustStatus(t, do(t, h, "POST", "/demo", nil), http.StatusCreated)
	mustStatus(t, do(t, h, "POST", "/worlds", obj{"name": "a", "seed": 1}), http.StatusCreated)
	rec := do(t, h, "POST", "/demo", nil)
	wantProblem(t, rec, http.StatusTooManyRequests, "rate-limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	wantProblem(t, do(t, h, "POST", "/duels", obj{}), http.StatusTooManyRequests, "rate-limited")
	// Reads are not counted.
	mustStatus(t, do(t, h, "GET", "/health", nil), http.StatusOK)
	// Another address has its own allowance.
	req := httptest.NewRequest("POST", "/api/v1/demo", nil)
	req.RemoteAddr = "198.51.100.9:1"
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusCreated {
		t.Errorf("second client = %d", out.Code)
	}
	now = now.Add(time.Hour + time.Second)
	mustStatus(t, do(t, h, "POST", "/demo", nil), http.StatusCreated)
}

func TestRateLimitMaxWorlds(t *testing.T) {
	h := newHandler(t, func(o *httpapi.Options) { o.MaxWorlds = 2 })
	mustStatus(t, do(t, h, "POST", "/demo", nil), http.StatusCreated)
	// A failed creation gives its slot back.
	wantProblem(t, do(t, h, "POST", "/worlds", obj{"name": strings.Repeat("x", 65), "seed": 1}), http.StatusBadRequest, "invalid-name")
	mustStatus(t, do(t, h, "POST", "/worlds", obj{"name": "ok", "seed": 1}), http.StatusCreated)
	rec := do(t, h, "POST", "/demo", nil)
	wantProblem(t, rec, http.StatusServiceUnavailable, "unavailable")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	// Reads keep working at the cap.
	mustStatus(t, do(t, h, "GET", "/health", nil), http.StatusOK)
}

func TestRateLimitProbesExempt(t *testing.T) {
	h := newHandler(t, func(o *httpapi.Options) {
		o.RatePerSecond, o.RateBurst = 0.001, 1
		o.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	})
	for i := 0; i < 20; i++ {
		for _, p := range []string{"/healthz", "/readyz"} {
			if rec := rawReq(h, "GET", p, ""); rec.Code != http.StatusOK {
				t.Fatalf("%s #%d = %d", p, i, rec.Code)
			}
		}
	}
	if rawReq(h, "GET", "/api/v1/health", "").Code != http.StatusOK || rawReq(h, "GET", "/api/v1/health", "").Code != http.StatusTooManyRequests {
		t.Error("the API route must still be limited")
	}
}

func TestNameValidation(t *testing.T) {
	h := newHandler(t, nil)
	long := strings.Repeat("é", 65)
	ok64 := strings.Repeat("é", 64)
	for _, name := range []string{long, "tab\there", "nul\x00", "line\nbreak", "\u202eflip", ""} {
		p := wantProblem(t, do(t, h, "POST", "/worlds", obj{"name": name, "seed": 1}), http.StatusBadRequest, "invalid-name")
		if is, _ := p["issues"].([]any); len(is) != 1 || is[0].(obj)["field"] != "name" {
			t.Errorf("name %q: issues = %v", name, p["issues"])
		}
		wantProblem(t, do(t, h, "POST", "/demo", obj{"name": name}), http.StatusBadRequest, "invalid-name")
		wantProblem(t, do(t, h, "POST", "/duels", obj{"name": name}), http.StatusBadRequest, "invalid-name")
	}
	w := mustStatus(t, do(t, h, "POST", "/worlds", obj{"name": ok64, "seed": 1}), http.StatusCreated)
	root := w["rootBranch"].(obj)["id"].(string)
	for _, label := range []string{long, "bad\x07label"} {
		wantProblem(t, do(t, h, "POST", "/branches/"+root+"/fork", obj{"atTick": 0, "label": label}), http.StatusBadRequest, "invalid-name")
	}
	mustStatus(t, do(t, h, "POST", "/branches/"+root+"/fork", obj{"atTick": 0, "label": ok64}), http.StatusCreated)
}

func TestReadiness(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var pings, schemas atomic.Int32
	var pingErr atomic.Pointer[error]
	build := func() *httpapi.Server {
		s, err := httpapi.New(httpapi.Options{
			Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday, WebOrigin: webOrigin,
			RatePerSecond: 100000, RateBurst: 100000, Now: func() time.Time { return now },
			Ping: func(context.Context) error {
				pings.Add(1)
				if e := pingErr.Load(); e != nil {
					return *e
				}
				return nil
			},
			CheckSchema: func(context.Context) error { schemas.Add(1); return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := build()
	h := s.Handler()
	if rec := rawReq(h, "GET", "/readyz", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ready"`) {
		t.Fatalf("readyz = %d %s", rec.Code, rec.Body.String())
	}
	// Within the TTLs nothing is re-checked; the schema is cached for 30 s.
	rawReq(h, "GET", "/readyz", "")
	now = now.Add(2 * time.Second)
	rawReq(h, "GET", "/readyz", "")
	if pings.Load() != 2 || schemas.Load() != 1 {
		t.Errorf("pings=%d schemas=%d, want 2 and 1", pings.Load(), schemas.Load())
	}
	now = now.Add(31 * time.Second)
	rawReq(h, "GET", "/readyz", "")
	if schemas.Load() != 2 {
		t.Errorf("schema not re-checked after 30 s: %d", schemas.Load())
	}
	// A failing database gives a generic 503; liveness is unaffected.
	e := errors.New("dial tcp 10.1.2.3:5432: password=hunter2 refused")
	pingErr.Store(&e)
	now = now.Add(2 * time.Second)
	rec := rawReq(h, "GET", "/readyz", "")
	wantProblem(t, rec, http.StatusServiceUnavailable, "unavailable")
	if b := rec.Body.String(); strings.Contains(b, "hunter2") || strings.Contains(b, "10.1.2.3") {
		t.Errorf("readiness problem leaks the error: %s", b)
	}
	if rec := rawReq(h, "GET", "/healthz", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Errorf("healthz = %d %s", rec.Code, rec.Body.String())
	}
	// Recovery after the cache expires.
	pingErr.Store(nil)
	now = now.Add(2 * time.Second)
	if rawReq(h, "GET", "/readyz", "").Code != 200 {
		t.Error("readyz did not recover")
	}
	// Shutdown flips readiness at once, even when the database is fine.
	s.BeginShutdown()
	s.BeginShutdown() // idempotent
	wantProblem(t, rawReq(h, "GET", "/readyz", ""), http.StatusServiceUnavailable, "unavailable")
	if rawReq(h, "HEAD", "/healthz", "").Code != 200 {
		t.Error("healthz must stay up while draining")
	}
}

func TestReadinessPingTimeout(t *testing.T) {
	s, err := httpapi.New(httpapi.Options{
		Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday,
		Ping: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	rec := rawReq(s.Handler(), "GET", "/readyz", "")
	wantProblem(t, rec, http.StatusServiceUnavailable, "unavailable")
	if d := time.Since(start); d < time.Second || d > 4*time.Second {
		t.Errorf("ping bounded at %v, want about 2s", d)
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestSessionIsolation(t *testing.T) {
	h := newHandler(t, func(o *httpapi.Options) { o.WorldCreatePerHourPerIP = 10000 })

	// IDs are random UUIDv4 capabilities and never repeat.
	seen := map[string]bool{}
	var worlds, roots []string
	for i := 0; i < 12; i++ {
		d := demo(t, h)
		w := d["world"].(obj)["id"].(string)
		b := branchID(d)
		for _, id := range []string{w, b} {
			if !uuidV4.MatchString(id) || seen[id] {
				t.Fatalf("id %q is not a fresh UUIDv4", id)
			}
			seen[id] = true
		}
		worlds, roots = append(worlds, w), append(roots, b)
	}
	A := worlds[0]
	rootA, rootB := roots[0], roots[1]
	du := mustStatus(t, do(t, h, "POST", "/duels", obj{}), http.StatusCreated)
	duelID := du["duel"].(obj)["id"].(string)
	if !uuidV4.MatchString(duelID) {
		t.Errorf("duel id %q is not a UUIDv4", duelID)
	}

	// A world's branch list holds only its own branches.
	for i, w := range worlds {
		l := mustStatus(t, do(t, h, "GET", "/worlds/"+w+"/branches", nil), http.StatusOK)["branches"].([]any)
		if len(l) != 1 || l[0].(obj)["id"] != roots[i] || l[0].(obj)["worldId"] != w {
			t.Fatalf("world %d lists %v", i, l)
		}
	}

	// A branch of world B cannot be used through world A's routes.
	wantProblem(t, do(t, h, "GET", "/worlds/"+A+"/assumptions?branchId="+rootB, nil), http.StatusForbidden, "forbidden")
	mustStatus(t, do(t, h, "GET", "/worlds/"+A+"/assumptions?branchId="+rootA, nil), http.StatusOK)

	// Comparisons across worlds are refused.
	wantProblem(t, do(t, h, "POST", "/diffs", obj{"branchA": rootA, "branchB": rootB, "comparisonTick": 0}), http.StatusUnprocessableEntity, "incomparable-branches")

	// A duel's branches only move through the duel endpoints.
	hb := du["duel"].(obj)["human"].(obj)["branch"].(obj)["id"].(string)
	wantProblem(t, do(t, h, "POST", "/branches/"+hb+"/advance", obj{}), http.StatusConflict, "duel-branch")

	// Unknown and malformed identifiers both give the same plain 404.
	unknown := "5f0d9a3e-1b2c-4d4e-8f60-1234567890ab"
	rt, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("handler is not a chi router")
	}
	var patterns []string
	err := chi.Walk(rt, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		patterns = append(patterns, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	idRoutes := 0
	for _, mr := range patterns {
		method, route, _ := strings.Cut(mr, " ")
		if !strings.Contains(route, "{") {
			continue
		}
		idRoutes++
		for _, bad := range []string{unknown, "not-a-uuid", "0", "../" + A} {
			path := route
			for _, p := range []string{"{worldId}", "{branchId}", "{duelId}"} {
				path = strings.ReplaceAll(path, p, strings.ReplaceAll(bad, "/", "%2F"))
			}
			body := ""
			if method == "POST" {
				body = "{}"
			} else {
				path += "?from=0&to=1&tick=0&fromTick=0&limit=1"
			}
			rec := do(t, h, method, path[len("/api/v1"):], body)
			if rec.Code != http.StatusNotFound && !(method == "POST" && (rec.Code == 400 || rec.Code == 422)) {
				t.Errorf("%s %s = %d, want 404; %s", method, path, rec.Code, rec.Body.String())
			}
			if rec.Code == 404 {
				if p := decode(t, rec); p["type"] != "urn:parallel:problem:not-found" {
					t.Errorf("%s %s: %v", method, path, p)
				}
			}
		}
	}
	if idRoutes < 25 {
		t.Errorf("walked only %d id routes", idRoutes)
	}

	// There is no collection endpoint that could enumerate worlds, branches or
	// duels, and every route other than the creators and static reads is
	// addressed by an unguessable id.
	open := map[string]bool{
		"GET /api/v1/health": true, "GET /api/v1/managers": true, "POST /api/v1/worlds": true,
		"POST /api/v1/demo": true, "POST /api/v1/duels": true, "POST /api/v1/diffs": true,
		"GET /healthz": true, "GET /readyz": true, "HEAD /healthz": true, "HEAD /readyz": true,
	}
	for _, mr := range patterns {
		method, route, _ := strings.Cut(mr, " ")
		if strings.Contains(route, "{") || open[mr] {
			continue
		}
		t.Errorf("route %s %s takes no identifier and is not a known creator", method, route)
	}
	for _, p := range []string{"/worlds", "/branches", "/duels", "/worlds/", "/branches/", "/diffs"} {
		rec := do(t, h, "GET", p, nil)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s = %d, want 404 or 405", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), A) {
			t.Errorf("GET %s leaked a world id", p)
		}
	}
}
