package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "sk-test-SECRET-key-123"

type stub struct {
	srv         *httptest.Server
	models      atomic.Int32
	completions atomic.Int32
	auth        atomic.Value
}

// newStub serves /models (modelsStatus, list) and /chat/completions (handler).
func newStub(t *testing.T, modelsStatus int, ids []string, chat http.HandlerFunc) *stub {
	t.Helper()
	s := &stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		s.models.Add(1)
		s.auth.Store(r.Header.Get("Authorization"))
		if modelsStatus != 200 {
			w.WriteHeader(modelsStatus)
			return
		}
		type m struct {
			ID string `json:"id"`
		}
		list := []m{}
		for _, id := range ids {
			list = append(list, m{id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": list})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		s.completions.Add(1)
		chat(w, r)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func okChat(content string, usage bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}}
		if usage {
			resp["usage"] = map[string]int{"prompt_tokens": 1234, "completion_tokens": 56, "total_tokens": 1290}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

const goodProposal = `{"commands":[{"kind":"hire_temp_workers","qty":2},{"kind":"hire_temp_workers","qty":99999}],"rationale":"add staff"}`

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func (s *stub) env(extra map[string]string) func(string) string {
	m := map[string]string{"LLM_BASE_URL": s.srv.URL + "/v1", "LLM_API_KEY": testKey, "LLM_MODEL": "vendor/model-x"}
	for k, v := range extra {
		m[k] = v
	}
	return envOf(m)
}

type result struct {
	code           int
	stdout, stderr string
	rep            llmReport
}

func runCheck(t *testing.T, getenv func(string) string, args ...string) result {
	t.Helper()
	var so, se bytes.Buffer
	code := llmCheckCmd(args, getenv, func() time.Time { return time.Date(2026, 11, 28, 1, 2, 3, 0, time.UTC) }, nil, &so, &se)
	r := result{code: code, stdout: so.String(), stderr: se.String()}
	if err := json.Unmarshal(so.Bytes(), &r.rep); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, so.String())
	}
	if strings.Contains(r.stdout+r.stderr, testKey) {
		t.Fatal("API key leaked to stdout or stderr")
	}
	if r.rep.ProviderCalls > maxLLMCheckCalls {
		t.Fatalf("provider calls %d > %d", r.rep.ProviderCalls, maxLLMCheckCalls)
	}
	return r
}

func TestLLMCheckPendingMakesNoRequest(t *testing.T) {
	s := newStub(t, 200, nil, okChat(goodProposal, true))
	// Base URL and model set, key missing.
	r := runCheck(t, envOf(map[string]string{"LLM_BASE_URL": s.srv.URL + "/v1", "LLM_MODEL": "m"}), "--confirm-paid-call")
	if r.code != exitLLMPending || r.rep.LiveVerified || r.rep.Config.Status != stPending {
		t.Fatalf("code %d rep %+v", r.code, r.rep)
	}
	if !strings.Contains(r.stderr, "LIVE VERIFICATION PENDING: no runtime API key configured") {
		t.Fatalf("stderr: %s", r.stderr)
	}
	if s.models.Load()+s.completions.Load() != 0 || r.rep.ProviderCalls != 0 {
		t.Fatal("requests were made while pending")
	}
	if len(r.rep.VariablesMissing) != 1 || r.rep.VariablesMissing[0] != "LLM_API_KEY" {
		t.Fatalf("missing: %v", r.rep.VariablesMissing)
	}
	// Nothing set at all.
	r = runCheck(t, envOf(nil))
	if r.code != exitLLMPending || r.rep.LiveVerified {
		t.Fatalf("code %d", r.code)
	}
}

func TestLLMCheckWithoutConfirmSkipsCompletion(t *testing.T) {
	s := newStub(t, 200, []string{"vendor/model-x"}, okChat(goodProposal, true))
	r := runCheck(t, s.env(nil))
	if r.code != exitOK || r.rep.LiveVerified || s.completions.Load() != 0 || s.models.Load() != 1 {
		t.Fatalf("code %d completions %d models %d", r.code, s.completions.Load(), s.models.Load())
	}
	if r.rep.Models.Status != stVerified || r.rep.Proposal.Status != stPending || r.rep.ProviderCalls != 1 {
		t.Fatalf("%+v", r.rep)
	}
	if s.auth.Load() != "Bearer "+testKey {
		t.Fatal("models request did not carry the bearer key")
	}
	if !strings.Contains(r.stderr, "SKIPPED") {
		t.Fatalf("stderr: %s", r.stderr)
	}
}

func TestLLMCheckModelsVariants(t *testing.T) {
	cases := []struct {
		name   string
		status int
		ids    []string
		want   string
		paid   int32 // completions expected with --confirm-paid-call
	}{
		{"present", 200, []string{"a", "vendor/model-x"}, stVerified, 1},
		{"missing model", 200, []string{"a", "b"}, stFailed, 0},
		{"unsupported 404", 404, nil, stInconclusive, 1},
		{"bad key 401", 401, nil, stFailed, 0},
		{"server error 500", 500, nil, stInconclusive, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub(t, tc.status, tc.ids, okChat(goodProposal, true))
			r := runCheck(t, s.env(nil), "--confirm-paid-call")
			if r.rep.Models.Status != tc.want {
				t.Fatalf("models status %q want %q (%s)", r.rep.Models.Status, tc.want, r.rep.Models.Detail)
			}
			if s.completions.Load() != tc.paid {
				t.Fatalf("completions %d want %d", s.completions.Load(), tc.paid)
			}
			if tc.paid == 0 && (r.rep.LiveVerified || r.code != exitLLMFailed) {
				t.Fatalf("code %d liveVerified %v", r.code, r.rep.LiveVerified)
			}
			if tc.want == stInconclusive && tc.status == 404 && !strings.Contains(r.rep.Models.Detail, "availability endpoint unsupported") {
				t.Fatalf("detail %q", r.rep.Models.Detail)
			}
		})
	}
}

func TestLLMCheckSuccessfulProposal(t *testing.T) {
	s := newStub(t, 200, []string{"vendor/model-x"}, okChat("```json\n"+goodProposal+"\n```", true))
	out := filepath.Join(t.TempDir(), "report.json")
	var so, se bytes.Buffer
	code := llmCheckCmd([]string{"--confirm-paid-call", "--out", out}, s.env(nil), time.Now, nil, &so, &se)
	if code != exitOK || so.Len() != 0 {
		t.Fatalf("code %d stdout %q stderr %s", code, so.String(), se.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b)+se.String(), testKey) || strings.Contains(string(b), "127.0.0.1:") || strings.Contains(string(b), "/v1") {
		t.Fatalf("report or stderr leaks key or URL parts:\n%s", b)
	}
	var rep llmReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	p := rep.Proposal
	if !rep.LiveVerified || p.Status != stVerified || p.HTTPStatus != "200" {
		t.Fatalf("%+v", rep)
	}
	if p.Usage.PromptTokens != 1234 || p.Usage.CompletionTokens != 56 || p.Usage.TotalTokens != 1290 || !p.Usage.ReportedByProvider || p.Usage.ReservedEstimate <= 1290 {
		t.Fatalf("usage %+v", p.Usage)
	}
	if p.CommandsProposed != 2 || p.CommandsAccepted != 1 || len(p.CommandsRejected) != 1 || p.CommandsRejected[0].Code != "too_many_temp_workers" {
		t.Fatalf("validator outcome %+v", p)
	}
	if rep.ProviderHost != "127.0.0.1" || rep.Model != "vendor/model-x" || rep.ProviderCalls != 2 || s.models.Load() != 1 || s.completions.Load() != 1 {
		t.Fatalf("host %q calls %d", rep.ProviderHost, rep.ProviderCalls)
	}
	if rep.TimestampUTC == "" || p.Rationale != "add staff" {
		t.Fatalf("%+v", rep)
	}
}

func TestLLMCheckUnreportedUsageIsFlagged(t *testing.T) {
	s := newStub(t, 200, []string{"vendor/model-x"}, okChat(goodProposal, false))
	r := runCheck(t, s.env(nil), "--confirm-paid-call")
	if !r.rep.LiveVerified || r.rep.Proposal.Usage.ReportedByProvider {
		t.Fatalf("%+v", r.rep.Proposal.Usage)
	}
}

func TestLLMCheckFailureClassification(t *testing.T) {
	cases := []struct {
		name   string
		chat   http.HandlerFunc
		extra  map[string]string
		reason string
	}{
		{"http 500", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }, nil, "http_error"},
		{"malformed content", okChat("not json at all", true), nil, "invalid_json"},
		{"schema violation", okChat(`{"commands":[{"kind":"launch_rocket"}],"rationale":"x"}`, true), nil, "schema_violation"},
		{"timeout", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		}, map[string]string{"LLM_REQUEST_TIMEOUT_SECONDS": "1"}, "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub(t, 200, []string{"vendor/model-x"}, tc.chat)
			r := runCheck(t, s.env(tc.extra), "--confirm-paid-call")
			if r.code != exitLLMFailed || r.rep.LiveVerified || r.rep.Proposal.FallbackReason != tc.reason || r.rep.Proposal.Status != stFailed {
				t.Fatalf("code %d rep %+v", r.code, r.rep.Proposal)
			}
			if s.completions.Load() != 1 || r.rep.ProviderCalls != 2 {
				t.Fatalf("completions %d calls %d", s.completions.Load(), r.rep.ProviderCalls)
			}
		})
	}
}

func TestLLMCheckRecorderCapsCalls(t *testing.T) {
	s := newStub(t, 200, nil, okChat(goodProposal, true))
	rec := &recorder{base: http.DefaultTransport, status: map[string]int{}}
	hc := &http.Client{Transport: rec}
	for i := 0; i < 5; i++ {
		resp, err := hc.Get(s.srv.URL + "/v1/models")
		if err == nil {
			resp.Body.Close()
		}
	}
	if rec.count() != maxLLMCheckCalls || s.models.Load() != maxLLMCheckCalls {
		t.Fatalf("recorded %d, server saw %d", rec.count(), s.models.Load())
	}
}

func TestLLMCheckBadFlagsAndConfig(t *testing.T) {
	var so, se bytes.Buffer
	if c := llmCheckCmd([]string{"--api-key", "x"}, envOf(nil), time.Now, nil, &so, &se); c != exitUsageOrFileError {
		t.Fatalf("code %d", c)
	}
	if c := llmCheckCmd(nil, envOf(map[string]string{"LLM_MAX_CALLS_PER_WORLD": "0"}), time.Now, nil, &so, &se); c != exitUsageOrFileError {
		t.Fatalf("code %d", c)
	}
}
