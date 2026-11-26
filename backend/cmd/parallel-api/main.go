// Command parallel-api is the HTTP server entry point (wiring only).
//
// Configuration comes from the environment:
//
//	PORT              listen port (default 8080)
//	WEB_ORIGIN        the one allowed CORS origin (default http://localhost:3100)
//	RATE_LIMIT_RPS    per-IP requests per second (default 50)
//	RATE_LIMIT_BURST  per-IP burst size (default 100)
//
// The slice stores Worlds, Branches and Inputs in memory: state is lost when
// the process stops.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

const (
	defaultPort      = "8080"
	defaultWebOrigin = "http://localhost:3100"
)

type config struct {
	port      string
	webOrigin string
	rps       float64
	burst     int
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() (config, error) {
	c := config{port: envOr("PORT", defaultPort), webOrigin: envOr("WEB_ORIGIN", defaultWebOrigin)}
	if n, err := strconv.Atoi(c.port); err != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("PORT %q is not a port number", c.port)
	}
	u, err := url.Parse(c.webOrigin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return c, fmt.Errorf("WEB_ORIGIN %q must be one origin such as http://localhost:3100 (scheme, host, optional port, no path)", c.webOrigin)
	}
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return c, fmt.Errorf("RATE_LIMIT_RPS %q must be a positive number", v)
		}
		c.rps = f
	}
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("RATE_LIMIT_BURST %q must be a positive integer", v)
		}
		c.burst = n
	}
	return c, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("parallel-api stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	svc := app.NewBranchService(memory.New(), nil, nil)
	api, err := httpapi.New(httpapi.Options{
		Service: svc, FlagshipScenario: scenarios.BlackFriday, WebOrigin: cfg.webOrigin,
		RatePerSecond: cfg.rps, RateBurst: cfg.burst, Logger: log,
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("parallel-api listening", "addr", srv.Addr, "webOrigin", cfg.webOrigin, "base", "/api/v1",
		"store", "memory (in-process: all worlds are lost when the server stops)")

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}
	return nil
}
