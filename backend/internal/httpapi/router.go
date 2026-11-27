package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Options configures the HTTP server.
type Options struct {
	// Service is the application service. Required.
	Service *app.BranchService
	// Duels runs Duels. Optional: when nil, one is built over Service.
	Duels *app.DuelService
	// FlagshipScenario is the scenario document used by POST /demo and by
	// POST /worlds when the request omits `scenario`. Required.
	FlagshipScenario []byte
	// WebOrigin is the single allowed CORS origin, for example
	// http://localhost:3100. Empty disables CORS (same-origin only).
	WebOrigin string
	// RatePerSecond and RateBurst configure the per-IP token bucket. Zero
	// values select DefaultRatePerSecond and DefaultRateBurst.
	RatePerSecond float64
	RateBurst     int
	// Now is the limiter clock (default time.Now).
	Now func() time.Time
	// Logger receives server-side error details (default slog.Default()).
	Logger *slog.Logger
	// SSEHeartbeat is the idle interval between heartbeat comments on the
	// progress stream (default 15 s). SSEPollInterval is the fallback head
	// poll interval of the stream (default 250 ms).
	SSEHeartbeat    time.Duration
	SSEPollInterval time.Duration
}

// Rate limit defaults: 50 requests per second per IP with a burst of 100.
const (
	DefaultRatePerSecond = 50
	DefaultRateBurst     = 100
)

// Server holds the handlers' dependencies.
type Server struct {
	svc       *app.BranchService
	duels     *app.DuelService
	flagship  []byte
	webOrigin string
	log       *slog.Logger
	limiter   *limiter

	planMu sync.Mutex
	plan   *demoplan.Plan

	sseHeartbeat time.Duration
	ssePoll      time.Duration
	sseMu        sync.Mutex
	sseConns     map[string]int // open progress streams per client IP
}

// New builds a Server. It validates the flagship scenario so a bad document
// fails at start-up and not on the first request.
func New(o Options) (*Server, error) {
	if o.Service == nil {
		return nil, errors.New("httpapi: Options.Service is required")
	}
	p, err := sim.ParseScenario(o.FlagshipScenario)
	if err != nil {
		return nil, fmt.Errorf("httpapi: flagship scenario: %w", err)
	}
	if err := sim.ValidateScenario(p); err != nil {
		return nil, fmt.Errorf("httpapi: flagship scenario: %w", err)
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RatePerSecond <= 0 {
		o.RatePerSecond = DefaultRatePerSecond
	}
	if o.RateBurst <= 0 {
		o.RateBurst = DefaultRateBurst
	}
	if o.SSEHeartbeat <= 0 {
		o.SSEHeartbeat = DefaultSSEHeartbeat
	}
	if o.SSEPollInterval <= 0 {
		o.SSEPollInterval = DefaultSSEPollInterval
	}
	if o.Duels == nil {
		o.Duels = app.NewDuelService(o.Service, nil)
	}
	return &Server{
		svc: o.Service, duels: o.Duels, flagship: o.FlagshipScenario, webOrigin: o.WebOrigin, log: o.Logger,
		limiter:      newLimiter(o.RatePerSecond, o.RateBurst, o.Now),
		sseHeartbeat: o.SSEHeartbeat, ssePoll: o.SSEPollInterval, sseConns: map[string]int{},
	}, nil
}

// Handler returns the router: /api/v1 behind recovery, security headers, CORS
// and the per-IP rate limit.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recoverer, securityHeaders, s.cors, s.rateLimit)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeProblem(w, req, newProblem("not-found", "no such endpoint"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		p := badRequest("method not allowed for this endpoint")
		p.Status = http.StatusMethodNotAllowed
		p.Title = "Method not allowed"
		writeProblem(w, req, p)
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.getHealth)
		r.Get("/managers", s.listManagers)
		r.Post("/worlds", s.createWorld)
		r.Get("/worlds/{worldId}", s.getWorld)
		r.Get("/worlds/{worldId}/branches", s.listBranches)
		r.Get("/worlds/{worldId}/assumptions", s.getAssumptions)
		r.Post("/demo", s.createDemo)
		r.Get("/branches/{branchId}", s.getBranch)
		r.Post("/branches/{branchId}/fork", s.forkBranch)
		r.Post("/branches/{branchId}/advance", s.advanceBranch)
		r.Post("/branches/{branchId}/commands", s.submitCommands)
		r.Post("/branches/{branchId}/decision-window/end", s.endDecisionWindow)
		r.Post("/branches/{branchId}/disruptions", s.injectDisruption)
		r.Get("/branches/{branchId}/tick-frames", s.listTickFrames)
		r.Get("/branches/{branchId}/state", s.getBranchState)
		r.Get("/branches/{branchId}/metrics", s.getBranchMetrics)
		r.Get("/branches/{branchId}/ledger", s.listLedgerEntries)
		r.Get("/branches/{branchId}/ledger/summary", s.getLedgerSummary)
		r.Get("/branches/{branchId}/journal", s.listJournal)
		r.Get("/branches/{branchId}/end-of-horizon", s.getEndOfHorizon)
		r.Get("/branches/{branchId}/export", s.exportBranch)
		r.Get("/branches/{branchId}/decisions", s.listDecisions)
		r.Get("/branches/{branchId}/progress", s.progressStream)
		r.Post("/diffs", s.createDiff)
		r.Post("/duels", s.createDuel)
		r.Get("/duels/{duelId}", s.getDuel)
		r.Post("/duels/{duelId}/advance", s.advanceDuel)
		r.Post("/duels/{duelId}/commands", s.submitDuelCommands)
		r.Post("/duels/{duelId}/decision-window/end", s.endDuelWindow)
		r.Post("/duels/{duelId}/disruptions", s.injectDuelDisruption)
		r.Get("/duels/{duelId}/score", s.getDuelScore)
		r.Get("/duels/{duelId}/decisions", s.listDuelDecisions)
	})
	return r
}

// ---- middleware ----

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
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

// cors allows exactly the configured web origin. Other origins get no CORS
// headers, so browsers block them.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin != "" && s.webOrigin != "" && origin == s.webOrigin
		if origin != "" {
			w.Header().Add("Vary", "Origin")
		}
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if allowed {
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, Last-Event-ID")
				h.Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := s.limiter.allow(clientIP(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeProblem(w, r, newProblem("rate-limited", "too many requests from this address"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the peer address. Forwarding headers are not trusted: behind a
// reverse proxy every client then shares one bucket, which is the safe
// failure.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- rate limiter ----

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

// ---- response helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail writes the problem for err. Unexpected errors are logged with their
// text server-side; the client sees only a generic 500.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	p := problemFor(err)
	if p.Status >= 500 {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err.Error())
	}
	writeProblem(w, r, p)
}

// pathBranch and pathWorld read identifiers. A value that is not a UUID cannot
// name anything, so it is a 404.
func pathID(r *http.Request, name string) (string, error) {
	id := chi.URLParam(r, name)
	if !isUUID(id) {
		return "", newProblem("not-found", "the world or branch does not exist")
	}
	return id, nil
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
		default:
			return false
		}
	}
	return true
}
