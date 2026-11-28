package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ipReq builds a request from peer with header pairs (Add, so a header can
// be repeated).
func ipReq(method, path, peer string, hdr ...string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = peer
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Add(hdr[i], hdr[i+1])
	}
	return r
}

func serve(h http.Handler, r *http.Request) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

func TestClientIPDerivation(t *testing.T) {
	const peer = "192.0.2.1:4000"
	const xri, xff = "X-Real-IP", "X-Forwarded-For"
	tests := []struct {
		name string
		mod  func(*Options)
		hdr  []string
		want string
	}{
		{"default ignores X-Real-IP and XFF", nil, []string{xri, "203.0.113.5", xff, "203.0.113.6"}, "192.0.2.1"},
		{"hops mode unchanged", func(o *Options) { o.TrustedProxyHops = 1 }, []string{xri, "203.0.113.5", xff, "1.1.1.1, 203.0.113.6"}, "203.0.113.6"},
		{"header mode uses X-Real-IP", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "203.0.113.5"}, "203.0.113.5"},
		{"header mode trims", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "  203.0.113.5 "}, "203.0.113.5"},
		{"header mode ignores XFF", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "203.0.113.5", xff, "9.9.9.9"}, "203.0.113.5"},
		{"header mode no header uses peer, not XFF", func(o *Options) { o.ClientIPHeader = xri }, []string{xff, "9.9.9.9"}, "192.0.2.1"},
		{"comma list falls back to peer", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "1.1.1.1, 2.2.2.2"}, "192.0.2.1"},
		{"repeated header falls back to peer", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "1.1.1.1", xri, "2.2.2.2"}, "192.0.2.1"},
		{"invalid falls back to peer", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "not-an-ip"}, "192.0.2.1"},
		{"empty falls back to peer", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, ""}, "192.0.2.1"},
		{"ip with port falls back to peer", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "1.1.1.1:80"}, "192.0.2.1"},
		{"zone falls back to peer", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "fe80::1%eth0"}, "192.0.2.1"},
		{"ipv6 canonical", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "2001:DB8:0:0:0:0:0:1"}, "2001:db8::1"},
		{"ipv4-mapped ipv6 unmapped", func(o *Options) { o.ClientIPHeader = xri }, []string{xri, "::ffff:203.0.113.5"}, "203.0.113.5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newInternalServer(t, func(o *Options) {
				o.Logger = slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
				if tc.mod != nil {
					tc.mod(o)
				}
			})
			if got := s.clientIP(ipReq("GET", "/x", peer, tc.hdr...)); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPHeaderValidation(t *testing.T) {
	for _, tc := range []struct {
		header string
		hops   int
		ok     bool
	}{
		{"", 0, true}, {"", 2, true}, {"X-Real-IP", 0, true},
		{"X-Real-IP", 1, false}, {"x-real-ip", 0, false}, {"X-Forwarded-For", 0, false}, {"CF-Connecting-IP", 0, false},
	} {
		if err := ValidateClientIPHeader(tc.header, tc.hops); (err == nil) != tc.ok {
			t.Errorf("ValidateClientIPHeader(%q, %d) = %v, ok=%v", tc.header, tc.hops, err, tc.ok)
		}
	}
	if _, err := New(Options{Service: newInternalServer(t, nil).svc, FlagshipScenario: newInternalServer(t, nil).flagship, ClientIPHeader: "X-Real-IP", TrustedProxyHops: 1}); err == nil {
		t.Error("New accepted ClientIPHeader with TrustedProxyHops")
	}
}

// The warning is rate limited and never contains the header value.
func TestClientIPWarningRateLimitedAndRedacted(t *testing.T) {
	var buf bytes.Buffer
	now := time.Unix(1_700_000_000, 0)
	s := newInternalServer(t, func(o *Options) {
		o.ClientIPHeader = "X-Real-IP"
		o.Logger = slog.New(slog.NewTextHandler(&buf, nil))
		o.Now = func() time.Time { return now }
	})
	for range 5 {
		s.clientIP(ipReq("GET", "/x", "192.0.2.1:1", "X-Real-IP", "secret-garbage-value"))
	}
	if n := strings.Count(buf.String(), "client IP header"); n != 1 {
		t.Errorf("warnings = %d, want 1", n)
	}
	now = now.Add(2 * time.Minute)
	s.clientIP(ipReq("GET", "/x", "192.0.2.1:1"))
	if n := strings.Count(buf.String(), "client IP header"); n != 2 {
		t.Errorf("warnings after a minute = %d, want 2", n)
	}
	if strings.Contains(buf.String(), "secret-garbage-value") {
		t.Error("warning echoed the header value")
	}
}

func headerServer(t *testing.T, mod func(*Options)) *Server {
	return newInternalServer(t, func(o *Options) {
		o.ClientIPHeader = "X-Real-IP"
		o.MaxConcurrentSimulations = 8
		o.Logger = slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
		if mod != nil {
			mod(o)
		}
	})
}

// Request rate limiter: separate X-Real-IP values are separate buckets, a
// spoofed X-Forwarded-For never creates a new one, malformed values share the
// peer bucket, and IPv6 forms normalise to one bucket.
func TestClientIPRateLimiterBuckets(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := headerServer(t, func(o *Options) {
		o.RatePerSecond, o.RateBurst = 0.001, 1
		o.Now = func() time.Time { return now }
	}).Handler()
	get := func(peer string, hdr ...string) int { return serve(h, ipReq("GET", "/api/v1/health", peer, hdr...)) }

	if get("10.0.0.1:1", "X-Real-IP", "203.0.113.1") != 200 || get("10.0.0.1:1", "X-Real-IP", "203.0.113.2") != 200 {
		t.Error("distinct X-Real-IP values must have separate buckets")
	}
	if get("10.0.0.1:1", "X-Real-IP", "203.0.113.1", "X-Forwarded-For", "1.1.1.1") != 429 ||
		get("10.0.0.2:1", "X-Real-IP", "203.0.113.1", "X-Forwarded-For", "2.2.2.2") != 429 {
		t.Error("same X-Real-IP with varying X-Forwarded-For and peers must share one bucket")
	}
	// Malformed values use the peer bucket: they cannot pick arbitrary buckets.
	if get("10.0.0.3:1", "X-Real-IP", "1.1.1.1, 2.2.2.2") != 200 ||
		get("10.0.0.3:2", "X-Real-IP", "garbage") != 429 ||
		get("10.0.0.3:3") != 429 {
		t.Error("malformed or missing X-Real-IP must share the peer bucket")
	}
	// IPv6 and IPv4-mapped forms.
	if get("10.0.0.4:1", "X-Real-IP", "2001:db8::1") != 200 ||
		get("10.0.0.4:1", "X-Real-IP", "2001:0db8:0:0:0:0:0:1") != 429 {
		t.Error("IPv6 spellings must share a bucket")
	}
	if get("10.0.0.4:1", "X-Real-IP", "::ffff:198.51.100.9") != 200 ||
		get("10.0.0.4:1", "X-Real-IP", "198.51.100.9") != 429 {
		t.Error("IPv4-mapped IPv6 must share the IPv4 bucket")
	}
}

// Default mode is spoof-proof: X-Real-IP and X-Forwarded-For are both ignored.
func TestClientIPDefaultModeIgnoresHeaders(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := newInternalServer(t, func(o *Options) {
		o.RatePerSecond, o.RateBurst = 0.001, 1
		o.Now = func() time.Time { return now }
	}).Handler()
	if serve(h, ipReq("GET", "/api/v1/health", "10.0.0.1:1", "X-Real-IP", "1.1.1.1", "X-Forwarded-For", "2.2.2.2")) != 200 ||
		serve(h, ipReq("GET", "/api/v1/health", "10.0.0.1:2", "X-Real-IP", "3.3.3.3", "X-Forwarded-For", "4.4.4.4")) != 429 {
		t.Error("default mode must key on the peer only")
	}
}

// World creation limiter keys on the derived client IP.
func TestClientIPWorldCreateLimiter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := headerServer(t, func(o *Options) {
		o.WorldCreatePerHourPerIP = 1
		o.Now = func() time.Time { return now }
	}).Handler()
	post := func(peer string, hdr ...string) int { return serve(h, ipReq("POST", "/api/v1/demo", peer, hdr...)) }
	if post("10.0.0.1:1", "X-Real-IP", "203.0.113.1") != 201 {
		t.Fatal("first create")
	}
	if post("10.0.0.2:1", "X-Real-IP", "203.0.113.1", "X-Forwarded-For", "8.8.8.8") != 429 {
		t.Error("same X-Real-IP must share the world-creation allowance")
	}
	if post("10.0.0.1:1", "X-Real-IP", "203.0.113.2") != 201 {
		t.Error("another X-Real-IP has its own allowance")
	}
}

// Write-token failure limiter keys on the derived client IP.
func TestClientIPWriteTokenFailureLimiter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := headerServer(t, func(o *Options) {
		o.WriteTokenFailuresPerMinute = 2
		o.Now = func() time.Time { return now }
	}).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ipReq("POST", "/api/v1/demo", "10.0.0.9:1", "X-Real-IP", "203.0.113.50"))
	var created struct {
		RootBranch struct {
			ID string `json:"id"`
		} `json:"rootBranch"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil || created.RootBranch.ID == "" {
		t.Fatalf("demo: %d %v", rec.Code, err)
	}
	adv := func(peer string, hdr ...string) int {
		hdr = append(hdr, WriteTokenHeader, "wrong-token")
		return serve(h, ipReq("POST", "/api/v1/branches/"+created.RootBranch.ID+"/advance", peer, hdr...))
	}
	for range 2 {
		if c := adv("10.0.0.1:1", "X-Real-IP", "203.0.113.7"); c != 403 {
			t.Fatalf("bad token = %d, want 403", c)
		}
	}
	if adv("10.0.0.2:1", "X-Real-IP", "203.0.113.7", "X-Forwarded-For", "6.6.6.6") != 429 {
		t.Error("same X-Real-IP must share the failure budget")
	}
	if adv("10.0.0.1:1", "X-Real-IP", "203.0.113.8") != 403 {
		t.Error("another X-Real-IP has its own failure budget")
	}
}

// LLM duels per day: the call site passes s.clientIP(r) to the counter, so the
// counter's key is the derived address.
func TestClientIPLLMDailyLimiter(t *testing.T) {
	s := headerServer(t, func(o *Options) { o.LLMDuelsPerDayPerIP = 1 })
	key := func(real, xff string) string {
		return s.clientIP(ipReq("POST", "/x", "10.0.0.1:1", "X-Real-IP", real, "X-Forwarded-For", xff))
	}
	if ok, _ := s.llmDaily.take(key("203.0.113.1", "1.1.1.1"), s.llmPerDay); !ok {
		t.Fatal("first take")
	}
	if ok, _ := s.llmDaily.take(key("203.0.113.1", "2.2.2.2"), s.llmPerDay); ok {
		t.Error("same X-Real-IP must share the daily budget")
	}
	if ok, _ := s.llmDaily.take(key("203.0.113.2", "1.1.1.1"), s.llmPerDay); !ok {
		t.Error("another X-Real-IP has its own daily budget")
	}
}

// SSE connection cap keys on the derived client IP (real streams).
func TestClientIPSSECap(t *testing.T) {
	s := headerServer(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/demo", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		RootBranch struct {
			ID string `json:"id"`
		} `json:"rootBranch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	open := func(hdr ...string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/branches/"+created.RootBranch.ID+"/progress?fromTick=99", nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		got, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for i := range MaxSSEConnsPerIP {
		got := open("X-Real-IP", "203.0.113.1", "X-Forwarded-For", "9.9.9."+string(rune('1'+i)))
		defer got.Body.Close()
		if got.StatusCode != 200 {
			t.Fatalf("stream %d = %d", i, got.StatusCode)
		}
	}
	over := open("X-Real-IP", "203.0.113.1", "X-Forwarded-For", "7.7.7.7")
	defer over.Body.Close()
	if over.StatusCode != 429 {
		t.Errorf("stream over the cap = %d, want 429 (varying X-Forwarded-For must not help)", over.StatusCode)
	}
	other := open("X-Real-IP", "203.0.113.2")
	defer other.Body.Close()
	if other.StatusCode != 200 {
		t.Errorf("another X-Real-IP = %d, want 200", other.StatusCode)
	}
}
