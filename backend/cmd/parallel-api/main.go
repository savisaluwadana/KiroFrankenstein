// Command parallel-api is the HTTP server entry point (wiring only).
//
// Configuration comes from the environment (docs/deploy.md has the full
// table with bounds):
//
//	PORT                          listen port (default 8080)
//	DATABASE_URL                  PostgreSQL runtime-role URL; memory store when unset
//	MIGRATE_DATABASE_URL          owner-role URL used only by `migrate`
//	WEB_ORIGIN                    the one allowed CORS origin (default http://localhost:3100)
//	ENV / PARALLEL_ENV            "production" enables strict start-up validation and HSTS
//	TRUSTED_PROXY_HOPS            trusted reverse proxies in front, 0..5 (default 0)
//	CLIENT_IP_HEADER              X-Real-IP (edge-set client IP, e.g. Railway); exclusive with TRUSTED_PROXY_HOPS > 0
//	RATE_LIMIT_RPS, RATE_LIMIT_BURST  per-IP request rate (default 50, 100)
//	HTTP_REQUEST_TIMEOUT_SECONDS  per-request deadline, 1..120 (default 30)
//	SHUTDOWN_TIMEOUT_SECONDS      graceful drain, 1..120 (default 25)
//	MAX_CONCURRENT_SIMULATIONS    expensive requests at once, 1..64 (default 8)
//	WORLD_CREATE_PER_HOUR_PER_IP  world creations per IP per hour, 1..10000 (default 60)
//	MAX_WORLDS                    world cap of the memory store, 1..1000000 (default 5000)
//
// The API stores Worlds in PostgreSQL when DATABASE_URL (the runtime role) is
// set, and in memory otherwise (state is lost when the process stops).
// `parallel-api migrate up` applies the schema with MIGRATE_DATABASE_URL.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

// llmOptions reads the LLM_* variables. An invalid value aborts start-up with
// an error that names the variable. When base URL, key and model are all set
// the LLM opponent is enabled; only the model identifier is logged, never the
// key or the URL.
func llmOptions(log *slog.Logger, getenv func(string) string) ([]app.Option, error) {
	cfg, err := llm.LoadConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !cfg.Available() {
		log.Info("llm=disabled")
		return nil, nil
	}
	opt, err := app.WithLLM(cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("llm configuration: %w", err)
	}
	log.Info("llm=enabled", "model", cfg.Model)
	return []app.Option{opt}, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(context.Background(), os.Args[2:]); err != nil {
			log.Error("migrate failed", "err", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, os.Getenv); err != nil {
		log.Error("parallel-api stopped", "err", err)
		stop()
		os.Exit(1)
	}
}

// run serves until ctx is cancelled (SIGINT or SIGTERM), then shuts down:
// readiness turns false at once, open progress streams end, in-flight
// requests (including those waiting on an LLM call) drain for up to
// SHUTDOWN_TIMEOUT_SECONDS, then the remaining connections are closed, which
// cancels their contexts, and the database pool is closed.
func run(ctx context.Context, log *slog.Logger, getenv func(string) string) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	st, err := openStore(ctx, getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer st.close()
	opts, err := llmOptions(log, getenv)
	if err != nil {
		return err
	}
	svc := app.NewBranchService(st.store, nil, nil, opts...)
	o := httpapi.Options{
		Service: svc, Duels: app.NewDuelService(svc, nil), FlagshipScenario: scenarios.BlackFriday, WebOrigin: cfg.webOrigin,
		RatePerSecond: cfg.rps, RateBurst: cfg.burst, Logger: log, Production: cfg.production,
		TrustedProxyHops: cfg.trustedProxyHops, ClientIPHeader: cfg.clientIPHeader, RequestTimeout: cfg.requestTimeout,
		MaxConcurrentSimulations: cfg.maxSimulations, WorldCreatePerHourPerIP: cfg.worldCreatePerHour,
		LLMAccessCode: cfg.llmAccessCode, LLMDuelsPerDayPerIP: cfg.llmDuelsPerDay,
		WriteTokenFailuresPerMinute: cfg.tokenFailsPerMin,
		Ping:                        st.ping, CheckSchema: st.checkSchema,
	}
	if st.memory {
		o.MaxWorlds = cfg.maxWorlds
	}
	api, err := httpapi.New(o)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// Responses are written within the request deadline; the progress
		// stream clears its own write deadline.
		WriteTimeout:   cfg.requestTimeout + 10*time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen on port %s: %w", cfg.port, err)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("parallel-api listening", "addr", srv.Addr, "webOrigin", cfg.webOrigin, "base", "/api/v1",
		"store", st.desc, "production", cfg.production, "trustedProxyHops", cfg.trustedProxyHops, "clientIPHeader", cfg.clientIPHeader)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	log.Info("shutting down", "timeout", cfg.shutdownTimeout.String())
	api.BeginShutdown()
	sctx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		log.Warn("graceful shutdown timed out; closing remaining connections")
		_ = srv.Close()
	}
	return nil
}
