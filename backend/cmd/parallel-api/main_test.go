package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func prodEnv(over map[string]string) map[string]string {
	m := map[string]string{
		"ENV": "production", "WEB_ORIGIN": "https://parallel.example.com", "TRUSTED_PROXY_HOPS": "1",
		"DATABASE_URL": "postgres://app:s3cretpw@db.internal:5432/parallel?sslmode=verify-full",
	}
	for k, v := range over {
		if v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	return m
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := loadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.production || c.port != "8080" || c.webOrigin != defaultWebOrigin || c.trustedProxyHops != 0 ||
		c.requestTimeout != 30*time.Second || c.shutdownTimeout != 25*time.Second || c.maxSimulations != 8 ||
		c.worldCreatePerHour != 60 || c.maxWorlds != 5000 {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestLoadConfigBounds(t *testing.T) {
	cases := []struct {
		name, val string
		ok        bool
	}{
		{"TRUSTED_PROXY_HOPS", "5", true}, {"TRUSTED_PROXY_HOPS", "6", false}, {"TRUSTED_PROXY_HOPS", "-1", false},
		{"HTTP_REQUEST_TIMEOUT_SECONDS", "1", true}, {"HTTP_REQUEST_TIMEOUT_SECONDS", "0", false}, {"HTTP_REQUEST_TIMEOUT_SECONDS", "121", false},
		{"SHUTDOWN_TIMEOUT_SECONDS", "120", true}, {"SHUTDOWN_TIMEOUT_SECONDS", "0", false}, {"SHUTDOWN_TIMEOUT_SECONDS", "x", false},
		{"MAX_CONCURRENT_SIMULATIONS", "64", true}, {"MAX_CONCURRENT_SIMULATIONS", "65", false}, {"MAX_CONCURRENT_SIMULATIONS", "0", false},
		{"WORLD_CREATE_PER_HOUR_PER_IP", "10000", true}, {"WORLD_CREATE_PER_HOUR_PER_IP", "10001", false},
		{"MAX_WORLDS", "1", true}, {"MAX_WORLDS", "0", false},
		{"WEB_ORIGIN", "*", false}, {"WEB_ORIGIN", "ftp://x.test", false}, {"WEB_ORIGIN", "https://x.test/", false},
		{"PORT", "70000", false},
	}
	for _, tc := range cases {
		_, err := loadConfig(env(map[string]string{tc.name: tc.val}))
		if (err == nil) != tc.ok {
			t.Errorf("%s=%s: err = %v, want ok=%v", tc.name, tc.val, err, tc.ok)
		} else if err != nil && !strings.Contains(err.Error(), tc.name) {
			t.Errorf("%s=%s: error does not name the variable: %v", tc.name, tc.val, err)
		}
	}
}

func TestProductionConfig(t *testing.T) {
	if c, err := loadConfig(env(prodEnv(nil))); err != nil || !c.production {
		t.Fatalf("valid production config rejected: %v", err)
	}
	if c, err := loadConfig(env(prodEnv(map[string]string{"ENV": "", "PARALLEL_ENV": "production"}))); err != nil || !c.production {
		t.Fatalf("PARALLEL_ENV not honoured: %v", err)
	}
	cases := []struct {
		name string
		over map[string]string
		want string
	}{
		{"web origin unset", map[string]string{"WEB_ORIGIN": ""}, "WEB_ORIGIN"},
		{"http origin", map[string]string{"WEB_ORIGIN": "http://parallel.example.com"}, "WEB_ORIGIN"},
		{"localhost https", map[string]string{"WEB_ORIGIN": "https://localhost:3100"}, "WEB_ORIGIN"},
		{"loopback", map[string]string{"WEB_ORIGIN": "https://127.0.0.1"}, "WEB_ORIGIN"},
		{"no database", map[string]string{"DATABASE_URL": ""}, "DATABASE_URL"},
		{"sslmode disable", map[string]string{"DATABASE_URL": "postgres://u:pw0rd@h/db?sslmode=disable"}, "DATABASE_URL"},
		{"sslmode missing", map[string]string{"DATABASE_URL": "postgres://u:pw0rd@h/db"}, "DATABASE_URL"},
		{"sslmode prefer", map[string]string{"DATABASE_URL": "postgres://u:pw0rd@h/db?sslmode=prefer"}, "DATABASE_URL"},
		{"dsn without ssl", map[string]string{"DATABASE_URL": "host=h user=u password=pw0rd dbname=d"}, "DATABASE_URL"},
		{"hops unset", map[string]string{"TRUSTED_PROXY_HOPS": ""}, "TRUSTED_PROXY_HOPS"},
		{"key without model", map[string]string{"LLM_API_KEY": "sk-secretkey", "LLM_BASE_URL": "https://openrouter.ai/api/v1"}, "LLM_MODEL"},
		{"model without key", map[string]string{"LLM_MODEL": "m", "LLM_BASE_URL": "https://openrouter.ai/api/v1"}, "LLM_API_KEY"},
		{"http llm", map[string]string{"LLM_API_KEY": "sk-secretkey", "LLM_MODEL": "m", "LLM_BASE_URL": "http://llm.example.com/v1"}, "LLM_BASE_URL"},
	}
	for _, tc := range cases {
		_, err := loadConfig(env(prodEnv(tc.over)))
		if err == nil {
			t.Errorf("%s: accepted", tc.name)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: error does not name %s: %v", tc.name, tc.want, err)
		}
		for _, secret := range []string{"pw0rd", "s3cretpw", "sk-secretkey", "postgres://", "host=h"} {
			if strings.Contains(msg, secret) {
				t.Errorf("%s: error leaks %q: %v", tc.name, secret, err)
			}
		}
	}
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		u := "postgres://u:pw@h/db?sslmode=" + mode
		if _, err := loadConfig(env(prodEnv(map[string]string{"DATABASE_URL": u}))); err != nil {
			t.Errorf("sslmode=%s rejected: %v", mode, err)
		}
	}
	// The same values are accepted outside production.
	if _, err := loadConfig(env(map[string]string{"DATABASE_URL": "postgres://u:pw@h/db?sslmode=disable", "WEB_ORIGIN": "http://localhost:3100"})); err != nil {
		t.Errorf("development config rejected: %v", err)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

// Start-up self-check: the server starts with secrets in its environment,
// serves, shuts down cleanly (readiness false first), and nothing it logged
// contains DATABASE_URL, LLM_API_KEY or the LLM endpoint.
func TestStartupLogsContainNoSecrets(t *testing.T) {
	const (
		dbSecret  = "dbpassword-Zq81"
		keySecret = "sk-or-key-Zq81"
		llmURL    = "https://llm-endpoint-Zq81.example.com/v1"
	)
	port := freePort(t)
	e := map[string]string{
		"PORT": port, "SHUTDOWN_TIMEOUT_SECONDS": "5",
		"LLM_BASE_URL": llmURL, "LLM_API_KEY": keySecret, "LLM_MODEL": "test/model",
	}
	var buf syncBuf
	log := slog.New(slog.NewTextHandler(&buf, nil))

	// 1. A failing database connection reports no URL.
	bad := map[string]string{"DATABASE_URL": "postgres://app:" + dbSecret + "@127.0.0.1:1/parallel?sslmode=require&connect_timeout=1"}
	for k, v := range e {
		bad[k] = v
	}
	err := run(context.Background(), log, env(bad))
	if err == nil {
		t.Fatal("run succeeded against an unreachable database")
	}
	log.Error("parallel-api stopped", "err", err) // as main does
	if strings.Contains(err.Error(), dbSecret) {
		t.Errorf("start-up error leaks the database password: %v", err)
	}

	// 2. A normal run with the memory store and the LLM configured.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, log, env(e)) }()
	base := "http://127.0.0.1:" + port
	var ready bool
	for i := 0; i < 100 && !ready; i++ {
		resp, err := http.Get(base + "/readyz")
		if err == nil {
			ready = resp.StatusCode == 200
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if !ready {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !ready {
		cancel()
		t.Fatalf("server never became ready; log:\n%s", buf.String())
	}
	resp, err := http.Get(base + "/api/v1/managers")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, s := range []string{keySecret, llmURL} {
		if strings.Contains(string(body), s) {
			t.Errorf("API response leaks %q", s)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop within the shutdown timeout")
	}
	if _, err := http.Get(base + "/healthz"); err == nil {
		t.Error("server still accepts connections after shutdown")
	}
	out := buf.String()
	for _, s := range []string{dbSecret, keySecret, "llm-endpoint-Zq81", "postgres://"} {
		if strings.Contains(out, s) {
			t.Errorf("log contains %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "shutting down") || !strings.Contains(out, "llm=enabled") {
		t.Errorf("expected start-up and shutdown lines in the log:\n%s", out)
	}
}
