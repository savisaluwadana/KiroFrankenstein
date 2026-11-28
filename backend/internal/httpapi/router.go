package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
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

	// Production enables HSTS (behind an HTTPS terminator).
	Production bool
	// TrustedProxyHops is the number of trusted reverse proxies in front of
	// the server, 0 to MaxTrustedProxyHops. 0 (default) uses the peer address
	// and ignores X-Forwarded-For.
	TrustedProxyHops int
	// RequestTimeout bounds each non-stream request (default 30 s).
	RequestTimeout time.Duration
	// MaxConcurrentSimulations and SimQueueWait limit expensive simulation
	// requests (defaults 8 and 5 s).
	MaxConcurrentSimulations int
	SimQueueWait             time.Duration
	// WorldCreatePerHourPerIP caps world creations per client IP per hour
	// (default 60).
	WorldCreatePerHourPerIP int
	// MaxWorlds caps the worlds created through this server; 0 is unlimited.
	// Set it for the memory store only.
	MaxWorlds int
	// Ping and CheckSchema back /readyz. Both are optional; with neither the
	// server is ready whenever it is not shutting down.
	Ping        func(context.Context) error
	CheckSchema func(context.Context) error

	// WriteTokenFailuresPerMinute caps invalid write tokens (and LLM access
	// codes) per client IP per minute before 429 (default 20).
	WriteTokenFailuresPerMinute int
	// LLMAccessCode, when set, is required in X-Parallel-LLM-Code to start an
	// LLM-backed manager (ai_llm). Empty: no code. The rule-based AI never
	// needs it.
	LLMAccessCode string
	// LLMDuelsPerDayPerIP caps LLM-backed manager creations per client IP per
	// 24 hours (default 5, 1 to 1000).
	LLMDuelsPerDayPerIP int
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

	now           func() time.Time
	production    bool
	proxyHops     int
	reqTimeout    time.Duration
	sims          chan struct{}
	simWait       time.Duration
	createPerHour int
	createMu      sync.Mutex
	creates       map[string]*hourWindow
	createSweep   time.Time
	maxWorlds     int64
	worlds        atomic.Int64

	tokenFails   *windowCounter
	tokenFailMax int
	llmCode      string
	llmDaily     *windowCounter
	llmPerDay    int

	ping, schema func(context.Context) error
	readyMu      sync.Mutex
	readyAt      time.Time
	readyErr     error
	schemaOKAt   time.Time

	shuttingDown atomic.Bool
	shutdownOnce sync.Once
	done         chan struct{}
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
	if o.WebOrigin != "" {
		if err := ValidateWebOrigin(o.WebOrigin); err != nil {
			return nil, fmt.Errorf("httpapi: WebOrigin: %w", err)
		}
	}
	if o.TrustedProxyHops < 0 || o.TrustedProxyHops > MaxTrustedProxyHops {
		return nil, fmt.Errorf("httpapi: TrustedProxyHops must be 0 to %d", MaxTrustedProxyHops)
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = DefaultRequestTimeout
	}
	if o.MaxConcurrentSimulations <= 0 {
		o.MaxConcurrentSimulations = DefaultMaxConcurrentSimulation
	}
	if o.SimQueueWait <= 0 {
		o.SimQueueWait = DefaultSimQueueWait
	}
	if o.WorldCreatePerHourPerIP <= 0 {
		o.WorldCreatePerHourPerIP = DefaultWorldCreatePerHourPerIP
	}
	if o.WriteTokenFailuresPerMinute <= 0 {
		o.WriteTokenFailuresPerMinute = DefaultWriteTokenFailuresPerMinute
	}
	if o.LLMDuelsPerDayPerIP == 0 {
		o.LLMDuelsPerDayPerIP = DefaultLLMDuelsPerDayPerIP
	}
	if o.LLMDuelsPerDayPerIP < 1 || o.LLMDuelsPerDayPerIP > MaxLLMDuelsPerDayPerIP {
		return nil, fmt.Errorf("httpapi: LLMDuelsPerDayPerIP must be 1 to %d", MaxLLMDuelsPerDayPerIP)
	}
	return &Server{
		tokenFails: newWindowCounter(time.Minute, o.Now), tokenFailMax: o.WriteTokenFailuresPerMinute,
		llmCode: o.LLMAccessCode, llmDaily: newWindowCounter(24*time.Hour, o.Now), llmPerDay: o.LLMDuelsPerDayPerIP,
		now: o.Now, production: o.Production, proxyHops: o.TrustedProxyHops, reqTimeout: o.RequestTimeout,
		sims: make(chan struct{}, o.MaxConcurrentSimulations), simWait: o.SimQueueWait,
		createPerHour: o.WorldCreatePerHourPerIP, creates: map[string]*hourWindow{}, maxWorlds: int64(o.MaxWorlds),
		ping: o.Ping, schema: o.CheckSchema, done: make(chan struct{}),
		svc: o.Service, duels: o.Duels, flagship: o.FlagshipScenario, webOrigin: o.WebOrigin, log: o.Logger,
		limiter:      newLimiter(o.RatePerSecond, o.RateBurst, o.Now),
		sseHeartbeat: o.SSEHeartbeat, ssePoll: o.SSEPollInterval, sseConns: map[string]int{},
	}, nil
}

// Handler returns the router. The infrastructure probes /healthz and /readyz
// sit outside CORS and rate limiting; /api/v1 runs behind recovery, security
// headers, CORS and the per-IP rate limit. Every route except the progress
// stream has a request deadline, and the expensive ones take a simulation
// slot.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recoverer, s.securityHeaders, skipProbes(s.cors), skipProbes(s.rateLimit))
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeProblem(w, req, newProblem("not-found", "no such endpoint"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		p := badRequest("method not allowed for this endpoint")
		p.Status = http.StatusMethodNotAllowed
		p.Title = "Method not allowed"
		writeProblem(w, req, p)
	})
	r.Get("/healthz", s.healthz)
	r.Head("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Head("/readyz", s.readyz)
	r.Route("/api/v1", func(r chi.Router) {
		// The stream has no deadline: it ends with the client, the horizon or
		// BeginShutdown.
		r.Get("/branches/{branchId}/progress", s.progressStream)
		r.Group(func(r chi.Router) {
			r.Use(s.withTimeout)
			heavy := r.With(s.simSlot)
			create := r.With(s.guardWorldCreate)
			// Every mutation of an existing world passes the write-token check
			// first. Creation and POST /diffs (a read) need no token.
			bw := r.With(s.requireWrite("branch"))
			dw := r.With(s.requireWrite("duel"))
			bwHeavy := bw.With(s.simSlot)
			dwHeavy := dw.With(s.simSlot)
			r.Get("/health", s.getHealth)
			r.Get("/managers", s.listManagers)
			create.Post("/worlds", s.createWorld)
			r.Get("/worlds/{worldId}", s.getWorld)
			r.Get("/worlds/{worldId}/branches", s.listBranches)
			r.Get("/worlds/{worldId}/assumptions", s.getAssumptions)
			create.Post("/demo", s.createDemo)
			r.Get("/branches/{branchId}", s.getBranch)
			bwHeavy.Post("/branches/{branchId}/fork", s.forkBranch)
			bwHeavy.Post("/branches/{branchId}/advance", s.advanceBranch)
			bw.Post("/branches/{branchId}/commands", s.submitCommands)
			bw.Post("/branches/{branchId}/decision-window/end", s.endDecisionWindow)
			bw.Post("/branches/{branchId}/disruptions", s.injectDisruption)
			r.Get("/branches/{branchId}/tick-frames", s.listTickFrames)
			r.Get("/branches/{branchId}/state", s.getBranchState)
			r.Get("/branches/{branchId}/metrics", s.getBranchMetrics)
			r.Get("/branches/{branchId}/ledger", s.listLedgerEntries)
			heavy.Get("/branches/{branchId}/ledger/summary", s.getLedgerSummary)
			heavy.Get("/branches/{branchId}/journal", s.listJournal)
			r.Get("/branches/{branchId}/end-of-horizon", s.getEndOfHorizon)
			r.Get("/branches/{branchId}/export", s.exportBranch)
			r.Get("/branches/{branchId}/decisions", s.listDecisions)
			heavy.Post("/diffs", s.createDiff)
			create.With(s.simSlot).Post("/duels", s.createDuel)
			r.Get("/duels/{duelId}", s.getDuel)
			dwHeavy.Post("/duels/{duelId}/advance", s.advanceDuel)
			dw.Post("/duels/{duelId}/commands", s.submitDuelCommands)
			dw.Post("/duels/{duelId}/decision-window/end", s.endDuelWindow)
			dw.Post("/duels/{duelId}/disruptions", s.injectDuelDisruption)
			r.Get("/duels/{duelId}/score", s.getDuelScore)
			r.Get("/duels/{duelId}/decisions", s.listDuelDecisions)
		})
	})
	return r
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
