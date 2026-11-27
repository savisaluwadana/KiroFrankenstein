package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
)

const (
	defaultPort      = "8080"
	defaultWebOrigin = "http://localhost:3100"
)

// config is the parsed process configuration. Error messages name the
// variable and never contain DATABASE_URL, LLM_API_KEY or any other secret.
type config struct {
	port       string
	webOrigin  string
	rps        float64
	burst      int
	production bool

	trustedProxyHops   int
	requestTimeout     time.Duration
	shutdownTimeout    time.Duration
	maxSimulations     int
	worldCreatePerHour int
	maxWorlds          int
	databaseConfigured bool
}

type getenvFunc func(string) string

func (g getenvFunc) or(key, def string) string {
	if v := g(key); v != "" {
		return v
	}
	return def
}

// intVar reads an integer variable within [lo, hi]; unset gives def.
func (g getenvFunc) intVar(key string, def, lo, hi int) (int, error) {
	v := g(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s %q must be an integer from %d to %d", key, v, lo, hi)
	}
	return n, nil
}

// productionEnv reports whether ENV (or PARALLEL_ENV when ENV is unset) is
// "production".
func productionEnv(get getenvFunc) bool {
	v := get("ENV")
	if v == "" {
		v = get("PARALLEL_ENV")
	}
	return strings.EqualFold(strings.TrimSpace(v), "production")
}

func loadConfig(getenv func(string) string) (config, error) {
	get := getenvFunc(getenv)
	c := config{port: get.or("PORT", defaultPort), webOrigin: get.or("WEB_ORIGIN", defaultWebOrigin), production: productionEnv(get)}
	if n, err := strconv.Atoi(c.port); err != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("PORT %q is not a port number", c.port)
	}
	if err := httpapi.ValidateWebOrigin(c.webOrigin); err != nil {
		return c, fmt.Errorf("WEB_ORIGIN %q: %w", c.webOrigin, err)
	}
	if v := get("RATE_LIMIT_RPS"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return c, fmt.Errorf("RATE_LIMIT_RPS %q must be a positive number", v)
		}
		c.rps = f
	}
	if v := get("RATE_LIMIT_BURST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("RATE_LIMIT_BURST %q must be a positive integer", v)
		}
		c.burst = n
	}
	var err error
	if c.trustedProxyHops, err = get.intVar("TRUSTED_PROXY_HOPS", 0, 0, httpapi.MaxTrustedProxyHops); err != nil {
		return c, err
	}
	secs, err := get.intVar("HTTP_REQUEST_TIMEOUT_SECONDS", 30, 1, 120)
	if err != nil {
		return c, err
	}
	c.requestTimeout = time.Duration(secs) * time.Second
	if secs, err = get.intVar("SHUTDOWN_TIMEOUT_SECONDS", 25, 1, 120); err != nil {
		return c, err
	}
	c.shutdownTimeout = time.Duration(secs) * time.Second
	if c.maxSimulations, err = get.intVar("MAX_CONCURRENT_SIMULATIONS", httpapi.DefaultMaxConcurrentSimulation, 1, 64); err != nil {
		return c, err
	}
	if c.worldCreatePerHour, err = get.intVar("WORLD_CREATE_PER_HOUR_PER_IP", httpapi.DefaultWorldCreatePerHourPerIP, 1, 10000); err != nil {
		return c, err
	}
	if c.maxWorlds, err = get.intVar("MAX_WORLDS", 5000, 1, 1_000_000); err != nil {
		return c, err
	}
	c.databaseConfigured = get("DATABASE_URL") != ""
	if c.production {
		if err := validateProduction(get, c); err != nil {
			return c, err
		}
	}
	return c, nil
}

// validateProduction applies the strict start-up rules of ENV=production.
func validateProduction(get getenvFunc, c config) error {
	if get("WEB_ORIGIN") == "" {
		return fmt.Errorf("WEB_ORIGIN must be set to the https origin of the web app in production")
	}
	u, _ := url.Parse(c.webOrigin) // already validated
	if u.Scheme != "https" {
		return fmt.Errorf("WEB_ORIGIN must be an https origin in production")
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return fmt.Errorf("WEB_ORIGIN must not be a localhost address in production")
	}
	if err := checkProductionDatabaseURL(get("DATABASE_URL")); err != nil {
		return err
	}
	if get("TRUSTED_PROXY_HOPS") == "" {
		return fmt.Errorf("TRUSTED_PROXY_HOPS must be set explicitly in production (0 when no reverse proxy is in front)")
	}
	llmCfg, err := llm.LoadConfig(get)
	if err != nil {
		return err
	}
	anyKeyOrModel := get(llm.EnvAPIKey) != "" || get(llm.EnvModel) != ""
	if anyKeyOrModel && !llmCfg.Available() {
		for _, name := range []string{llm.EnvBaseURL, llm.EnvAPIKey, llm.EnvModel} {
			if strings.TrimSpace(get(name)) == "" {
				return fmt.Errorf("%s must be set when another LLM variable is set", name)
			}
		}
	}
	if llmCfg.Available() && !strings.HasPrefix(strings.ToLower(llmCfg.BaseURL), "https://") {
		return fmt.Errorf("%s must be an https URL in production", llm.EnvBaseURL)
	}
	return nil
}

// checkProductionDatabaseURL requires DATABASE_URL and an encrypted
// connection. It never echoes the value or a parser error (which can quote
// the URL).
func checkProductionDatabaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("DATABASE_URL is required in production")
	}
	mode := ""
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		mode = u.Query().Get("sslmode")
	} else if strings.Contains(raw, "://") {
		return fmt.Errorf("DATABASE_URL is not a valid postgres:// URL")
	} else {
		for f := range strings.FieldsSeq(raw) { // key=value DSN
			if v, ok := strings.CutPrefix(f, "sslmode="); ok {
				mode = v
			}
		}
	}
	switch mode {
	case "require", "verify-ca", "verify-full":
		return nil
	case "":
		return fmt.Errorf("DATABASE_URL must set sslmode=require, verify-ca or verify-full in production")
	}
	return fmt.Errorf("DATABASE_URL sslmode must be require, verify-ca or verify-full in production")
}
