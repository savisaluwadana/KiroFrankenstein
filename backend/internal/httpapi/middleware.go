package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Hardening defaults and bounds (security steering; docs/deploy.md lists the
// environment variable behind each one).
const (
	DefaultRequestTimeout          = 30 * time.Second
	DefaultMaxConcurrentSimulation = 8
	DefaultSimQueueWait            = 5 * time.Second
	DefaultWorldCreatePerHourPerIP = 60
	MaxTrustedProxyHops            = 5

	readyPingTimeout = 2 * time.Second
	readyPingTTL     = time.Second
	readySchemaTTL   = 30 * time.Second
	hstsValue        = "max-age=15552000; includeSubDomains"
)

// ValidateWebOrigin checks the one allowed CORS origin: an http or https
// origin with host and optional port, and nothing else. "*" is rejected, so
// credentialed or wildcard CORS cannot be configured by accident.
func ValidateWebOrigin(origin string) error {
	if origin == "*" || strings.Contains(origin, "*") {
		return errors.New("a wildcard origin is not allowed; name the exact web origin")
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || strings.HasSuffix(origin, "/") {
		return errors.New("must be one http or https origin such as https://app.example.com (scheme, host, optional port, no path)")
	}
	return nil
}

// ---- security headers ----

// securityHeaders sets the API response headers. Responses are never cached;
// the progress stream replaces Cache-Control with its own value. HSTS is sent
// only in production mode behind an HTTPS terminator.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if s.production && (r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")) {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				// The panic value is logged server-side only; the response
				// carries no stack trace and no panic text.
				s.log.Error("handler panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(v))
				writeProblem(w, r, newProblem("internal", "the server could not complete the request"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- CORS ----

// cors allows exactly the configured web origin (exact string match). Other
// origins get no CORS headers, so browsers block them. Credentials are never
// allowed: Access-Control-Allow-Credentials is not sent.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin != "" && s.webOrigin != "" && origin == s.webOrigin
		h := w.Header()
		if origin != "" {
			h.Add("Vary", "Origin")
		}
		if allowed {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", "Retry-After")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			if allowed {
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, Last-Event-ID, "+WriteTokenHeader+", "+LLMCodeHeader)
				h.Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- client address and rate limit ----

// clientIP is the address used for per-client limits. By default it is the
// peer address and forwarding headers are ignored. With TrustedProxyHops = N
// > 0 the server runs behind N trusted proxies and takes the Nth entry from
// the right of X-Forwarded-For (the address the outermost trusted proxy saw).
// Entries further left are client-controlled and never used. When the header
// has fewer than N entries or the entry is not an IP address, the peer
// address is used.
func (s *Server) clientIP(r *http.Request) string {
	peer := peerAddr(r)
	if s.proxyHops == 0 {
		return peer
	}
	var parts []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for p := range strings.SplitSeq(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
	}
	if len(parts) < s.proxyHops {
		return peer
	}
	ip := net.ParseIP(parts[len(parts)-s.proxyHops])
	if ip == nil {
		return peer
	}
	return ip.String()
}

func peerAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := s.limiter.allow(s.clientIP(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeProblem(w, r, newProblem("rate-limited", "too many requests from this address"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// skipProbes applies mw to every path except the infrastructure probes, which
// stay outside CORS and rate limiting.
func skipProbes(mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		guarded := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}
			guarded.ServeHTTP(w, r)
		})
	}
}

type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is an in-memory per-key token bucket.
type limiter struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	sweep   time.Time
}

func newLimiter(rate float64, burst int, now func() time.Time) *limiter {
	return &limiter{rate: rate, burst: float64(burst), now: now, buckets: map[string]*bucket{}}
}

// allow takes one token for key. When none is left it returns the whole
// seconds to wait.
func (l *limiter) allow(key string) (bool, int) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.sweep) > time.Minute {
		full := time.Duration(l.burst / l.rate * float64(time.Second))
		for k, b := range l.buckets {
			if now.Sub(b.last) > full {
				delete(l.buckets, k)
			}
		}
		l.sweep = now
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens = min(l.burst, b.tokens+el*l.rate)
		b.last = now
	}
	if b.tokens < 1 {
		wait := int((1-b.tokens)/l.rate) + 1
		return false, wait
	}
	b.tokens--
	return true, 0
}

// ---- per-request timeout ----

// withTimeout bounds every non-stream request with a context deadline. Waits
// (store calls, the advance lock, LLM flights, the simulation queue) honour
// the context; a deadline error becomes a 503 `unavailable` problem.
func (s *Server) withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.reqTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- concurrency limit for expensive simulation work ----

// simSlot admits at most MaxConcurrentSimulations expensive requests at once
// (advance, fork, diff, journal, ledger summary, duel create/advance). An
// excess request waits up to SimQueueWait, then gets 503 with Retry-After.
func (s *Server) simSlot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.NewTimer(s.simWait)
		defer t.Stop()
		select {
		case s.sims <- struct{}{}:
			defer func() { <-s.sims }()
			next.ServeHTTP(w, r)
		case <-t.C:
			w.Header().Set("Retry-After", "2")
			writeProblem(w, r, newProblem("unavailable", "the server is busy with other simulations; retry shortly"))
		case <-r.Context().Done():
			w.Header().Set("Retry-After", "2")
			writeProblem(w, r, newProblem("unavailable", "the request ended while waiting for a free simulation slot"))
		}
	})
}

// ---- world creation caps ----

type hourWindow struct {
	start time.Time
	n     int
}

// allowWorldCreate counts one world creation for ip in a fixed one-hour
// window. It returns the seconds until the window ends when the cap is hit.
func (s *Server) allowWorldCreate(ip string) (bool, int) {
	now := s.now()
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if now.Sub(s.createSweep) > time.Hour {
		for k, w := range s.creates {
			if now.Sub(w.start) >= time.Hour {
				delete(s.creates, k)
			}
		}
		s.createSweep = now
	}
	w := s.creates[ip]
	if w == nil || now.Sub(w.start) >= time.Hour {
		w = &hourWindow{start: now}
		s.creates[ip] = w
	}
	if w.n >= s.createPerHour {
		return false, int(time.Hour.Seconds()-now.Sub(w.start).Seconds()) + 1
	}
	w.n++
	return true, 0
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// guardWorldCreate protects the endpoints that create a world: a per-IP
// hourly cap (429) and, when MaxWorlds is set (memory store), a total cap
// (503). A failed creation gives its slot back.
func (s *Server) guardWorldCreate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := s.allowWorldCreate(s.clientIP(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeProblem(w, r, newProblem("rate-limited", "too many worlds created from this address in the last hour"))
			return
		}
		if s.maxWorlds > 0 {
			if s.worlds.Add(1) > s.maxWorlds {
				s.worlds.Add(-1)
				w.Header().Set("Retry-After", "60")
				writeProblem(w, r, newProblem("unavailable", "the server holds the maximum number of worlds"))
				return
			}
			sw := &statusWriter{ResponseWriter: w}
			defer func() {
				if sw.status < 200 || sw.status >= 300 {
					s.worlds.Add(-1)
				}
			}()
			w = sw
		}
		next.ServeHTTP(w, r)
	})
}

// ---- health and readiness ----

type statusDTO struct {
	Status string `json:"status"`
}

// healthz is liveness: the process is up. It checks no dependency.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusDTO{Status: "ok"})
}

// readyz is readiness: not shutting down, the database answers a ping within
// 2 s and the schema is present (checked at most every 30 s). The response
// never says why.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.shuttingDown.Load() {
		writeProblem(w, r, newProblem("unavailable", "the service is not ready"))
		return
	}
	if err := s.checkReady(r.Context()); err != nil {
		s.log.Warn("readiness check failed", "err", err.Error())
		writeProblem(w, r, newProblem("unavailable", "the service is not ready"))
		return
	}
	writeJSON(w, http.StatusOK, statusDTO{Status: "ready"})
}

func (s *Server) checkReady(ctx context.Context) error {
	if s.ping == nil && s.schema == nil {
		return nil
	}
	s.readyMu.Lock()
	defer s.readyMu.Unlock()
	now := s.now()
	if !s.readyAt.IsZero() && now.Sub(s.readyAt) < readyPingTTL {
		return s.readyErr
	}
	cctx, cancel := context.WithTimeout(ctx, readyPingTimeout)
	defer cancel()
	var err error
	if s.ping != nil {
		err = s.ping(cctx)
	}
	if err == nil && s.schema != nil && (s.schemaOKAt.IsZero() || now.Sub(s.schemaOKAt) >= readySchemaTTL) {
		if err = s.schema(cctx); err == nil {
			s.schemaOKAt = now
		}
	}
	s.readyAt, s.readyErr = now, err
	return err
}

// BeginShutdown marks the server not ready and ends open progress streams.
// It is idempotent. Call it before http.Server.Shutdown.
func (s *Server) BeginShutdown() {
	s.shuttingDown.Store(true)
	s.shutdownOnce.Do(func() { close(s.done) })
}
