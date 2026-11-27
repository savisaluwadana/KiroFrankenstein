package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

// Exit codes of llm-check (in addition to those of run.go).
const (
	exitLLMPending = 4 // LIVE VERIFICATION PENDING: provider variables not all set
	exitLLMFailed  = 5 // a live request was made and the check failed
)

// maxLLMCheckCalls is the hard cap on provider requests per invocation: one
// free models listing and at most one paid completion.
const maxLLMCheckCalls = 2

// Step status labels of the report.
const (
	stPending      = "pending"
	stVerified     = "verified"
	stInconclusive = "inconclusive"
	stFailed       = "failed"
)

type llmStep struct {
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	HTTPStatus string `json:"httpStatus,omitempty"`
	LatencyMs  int64  `json:"latencyMs"`
}

type llmUsageReport struct {
	PromptTokens       int  `json:"promptTokens"`
	CompletionTokens   int  `json:"completionTokens"`
	TotalTokens        int  `json:"totalTokens"`
	ReportedByProvider bool `json:"reportedByProvider"`
	ReservedEstimate   int  `json:"reservedEstimate"`
}

type llmRejection struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Code  string `json:"code"`
}

type llmProposalStep struct {
	llmStep
	Usage             llmUsageReport `json:"usage"`
	CommandsProposed  int            `json:"commandsProposed"`
	CommandsAccepted  int            `json:"commandsAccepted"`
	CommandsRejected  []llmRejection `json:"commandsRejected"`
	Rationale         string         `json:"rationaleUntrusted,omitempty"`
	FallbackReason    string         `json:"fallbackReason,omitempty"`
	CommandBudget     int64          `json:"commandBudget"`
	ForkTick          int64          `json:"forkTick"`
	ScenarioName      string         `json:"scenario"`
	Seed              uint64         `json:"seed"`
	PromptBytes       int            `json:"promptBytes"`
	MaxTokensPerCall  int            `json:"maxTokensPerCall"`
	RequestTimeoutSec int            `json:"requestTimeoutSeconds"`
}

type llmReport struct {
	LiveVerified     bool            `json:"liveVerified"`
	Reason           string          `json:"reason"`
	TimestampUTC     string          `json:"timestampUtc"`
	ProviderHost     string          `json:"providerHost,omitempty"`
	Model            string          `json:"model,omitempty"`
	VariablesSet     []string        `json:"variablesSet"`
	VariablesMissing []string        `json:"variablesMissing"`
	Config           llmStep         `json:"configCheck"`
	Models           llmStep         `json:"modelAvailability"`
	Proposal         llmProposalStep `json:"structuredProposal"`
	FallbackProbe    llmStep         `json:"fallbackProbe"`
	ProviderCalls    int             `json:"providerCalls"`
	MaxProviderCalls int             `json:"maxProviderCalls"`
	Disclaimer       string          `json:"disclaimer"`
}

// recorder is the only transport of llm-check. It counts requests, refuses a
// third one and records the HTTP status of each path.
type recorder struct {
	base   http.RoundTripper
	mu     sync.Mutex
	calls  int
	status map[string]int
}

var errCallCap = errors.New("llm-check: provider call cap reached")

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	if r.calls >= maxLLMCheckCalls {
		r.mu.Unlock()
		return nil, errCallCap
	}
	r.calls++
	r.mu.Unlock()
	resp, err := r.base.RoundTrip(req)
	if err == nil {
		r.mu.Lock()
		r.status[req.URL.Path] = resp.StatusCode
		r.mu.Unlock()
	}
	return resp, err
}

func (r *recorder) statusOf(suffix string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for p, s := range r.status {
		if strings.HasSuffix(p, suffix) {
			return fmt.Sprint(s)
		}
	}
	return ""
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// scrubWriter replaces every occurrence of secret in the output. It is a
// last line of defence; no code path prints the key on purpose.
type scrubWriter struct {
	w      io.Writer
	secret string
}

func (s scrubWriter) Write(p []byte) (int, error) {
	if s.secret != "" {
		q := strings.ReplaceAll(string(p), s.secret, "[redacted]")
		if _, err := io.WriteString(s.w, q); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return s.w.Write(p)
}

// llmCheckCmd is the live verification procedure for the LLM AI manager. It
// reads the provider configuration from the environment only (getenv) and
// makes at most two provider requests: the free model listing and, only with
// --confirm-paid-call, one chat completion. Human text goes to stderr, the
// JSON report to --out (default stdout). It never prints the API key.
func llmCheckCmd(args []string, getenv func(string) string, now func() time.Time, base http.RoundTripper, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("llm-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	confirm := fs.Bool("confirm-paid-call", false, "allow the one paid completion request")
	out := fs.String("out", "", "write the JSON report to this file (default stdout)")
	scPath := fs.String("scenario", "", "scenario file (default: the embedded flagship)")
	seed := fs.Uint64("seed", 2026, "world seed of the observation")
	probe := fs.Bool("probe-fallback", false, "accepted; the fallback probe is never run live (stub tests cover it)")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "parallel-sim llm-check: usage: llm-check [--confirm-paid-call] [--out FILE] [--scenario FILE] [--seed N] [--probe-fallback]")
		return exitUsageOrFileError
	}
	cfg, err := llm.LoadConfig(getenv)
	secret := strings.TrimSpace(getenv(llm.EnvAPIKey))
	stderr = scrubWriter{stderr, secret}
	stdout = scrubWriter{stdout, secret}
	if err != nil {
		fmt.Fprintln(stderr, "parallel-sim llm-check:", err)
		return exitUsageOrFileError
	}

	rep := llmReport{
		TimestampUTC: now().UTC().Format(time.RFC3339), MaxProviderCalls: maxLLMCheckCalls,
		VariablesSet: []string{}, VariablesMissing: []string{}, Disclaimer: sim.Disclaimer,
		FallbackProbe: llmStep{Status: stPending, Detail: "never run live: a live provider cannot be forced to answer malformed; covered by stub tests"},
	}
	_ = *probe
	for _, v := range []struct{ name, val string }{{llm.EnvBaseURL, cfg.BaseURL}, {llm.EnvAPIKey, cfg.APIKey}, {llm.EnvModel, cfg.Model}} {
		if v.val != "" {
			rep.VariablesSet = append(rep.VariablesSet, v.name)
		} else {
			rep.VariablesMissing = append(rep.VariablesMissing, v.name)
		}
	}
	fmt.Fprintf(stderr, "variables set: %s\nvariables missing: %s\n", strings.Join(rep.VariablesSet, ", "), strings.Join(rep.VariablesMissing, ", "))
	rep.Models = llmStep{Status: stPending, Detail: "not run"}
	rep.Proposal.Status, rep.Proposal.Detail = stPending, "not run"
	rep.Proposal.CommandsRejected = []llmRejection{}

	if base == nil {
		base = http.DefaultTransport
	}
	rec := &recorder{base: base, status: map[string]int{}}
	finish := func(code int) int {
		rep.ProviderCalls = rec.count()
		b, _ := json.MarshalIndent(rep, "", "  ")
		b = append(b, '\n')
		if *out != "" {
			if err := os.WriteFile(*out, b, 0o600); err != nil {
				fmt.Fprintln(stderr, "parallel-sim llm-check: write report:", err)
				return exitUsageOrFileError
			}
			fmt.Fprintf(stderr, "report written to %s\n", *out)
		} else {
			_, _ = stdout.Write(b)
		}
		fmt.Fprintf(stderr, "liveVerified: %v (%s)\n", rep.LiveVerified, rep.Reason)
		return code
	}
	return runLLMCheck(&rep, cfg, *confirm, *scPath, *seed, rec, stderr, finish)
}

func runLLMCheck(rep *llmReport, cfg llm.Config, confirm bool, scPath string, seed uint64, rec *recorder, stderr io.Writer, finish func(int) int) int {
	if !cfg.Available() {
		rep.Config = llmStep{Status: stPending, Detail: "provider variables not all set"}
		rep.Reason = "no runtime API key configured"
		fmt.Fprintln(stderr, "LIVE VERIFICATION PENDING: no runtime API key configured")
		fmt.Fprintln(stderr, "Set LLM_BASE_URL, LLM_API_KEY and LLM_MODEL in the environment (see docs/live-llm-verification.md). No request was made.")
		return finish(exitLLMPending)
	}
	rep.Config = llmStep{Status: stVerified, Detail: "all provider variables set"}
	if u, err := url.Parse(cfg.BaseURL); err == nil {
		rep.ProviderHost = u.Hostname()
	}
	rep.Model = cfg.Model

	hc := &http.Client{Transport: rec, Timeout: cfg.RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// Step 2: model availability (free).
	rep.Models = checkModels(hc, cfg)
	rep.Models.HTTPStatus = rec.statusOf("/models")
	fmt.Fprintf(stderr, "model availability: %s (%s)\n", rep.Models.Status, rep.Models.Detail)

	if !confirm {
		rep.Proposal.Detail = "skipped: --confirm-paid-call not given; no completion request was made"
		rep.Reason = "completion call skipped (--confirm-paid-call not given)"
		fmt.Fprintln(stderr, "NOTICE: the completion step performs at most 1 paid completion request. It was SKIPPED; re-run with --confirm-paid-call to perform it.")
		return finish(exitOK)
	}
	if rep.Models.Status == stFailed {
		rep.Proposal.Detail = "skipped: model availability check failed, no paid request made"
		rep.Reason = "model availability check failed: " + rep.Models.Detail
		fmt.Fprintln(stderr, "completion call skipped because the availability check failed")
		return finish(exitLLMFailed)
	}
	fmt.Fprintln(stderr, "NOTICE: performing at most 1 paid completion request.")

	// Step 3: real observation, production Manager.Propose, validator.
	sc, obs, forkTick, err := flagshipObservation(scPath, seed)
	if err != nil {
		rep.Proposal = llmProposalStep{llmStep: llmStep{Status: stFailed, Detail: err.Error()}, CommandsRejected: []llmRejection{}}
		rep.Reason = "could not build the observation: " + err.Error()
		return finish(exitUsageOrFileError)
	}
	budget := int(sc.CommandBudget)
	system := llm.SystemPrompt(budget)
	user, err := llm.UserPrompt(obs, budget)
	if err != nil {
		rep.Reason = "prompt: " + err.Error()
		return finish(exitUsageOrFileError)
	}
	reserved := cfg.ReservationTokens(system, user)

	client, err := llm.NewClient(cfg, &http.Client{Transport: rec})
	if err != nil {
		rep.Reason = "client: " + err.Error()
		return finish(exitUsageOrFileError)
	}
	tracker := llm.NewTracker(cfg)
	mgr, err := llm.NewManager(client, cfg, tracker)
	if err != nil {
		rep.Reason = "manager: " + err.Error()
		return finish(exitUsageOrFileError)
	}
	p := &rep.Proposal
	p.CommandBudget, p.ForkTick, p.ScenarioName, p.Seed = sc.CommandBudget, forkTick, sc.Name, seed
	p.PromptBytes, p.MaxTokensPerCall, p.RequestTimeoutSec = len(system)+len(user), cfg.MaxTokensPerCall, int(cfg.RequestTimeout/time.Second)
	p.Usage.ReservedEstimate = reserved

	start := time.Now()
	prop, perr := mgr.Propose(context.Background(), obs, budget)
	p.LatencyMs = time.Since(start).Milliseconds()
	p.HTTPStatus = rec.statusOf("/chat/completions")
	u := tracker.Usage()
	p.Usage.PromptTokens, p.Usage.CompletionTokens, p.Usage.TotalTokens = u.PromptTokens, u.CompletionTokens, u.TotalTokens
	// An unreported usage is charged as the whole reservation (prompt estimate + max_tokens).
	p.Usage.ReportedByProvider = u.Calls > 0 && !(u.TotalTokens == reserved && u.PromptTokens == llm.EstimatePromptTokens(system, user))
	if perr != nil {
		p.Status = stFailed
		p.FallbackReason = llm.FallbackReason(perr)
		p.Detail = perr.Error() // typed llm errors never contain the key or URL
		rep.Reason = "completion call did not return a parsed structured proposal: " + p.FallbackReason
		fmt.Fprintf(stderr, "structured proposal: failed, fallback reason %s (%s)\n", p.FallbackReason, p.Detail)
		return finish(exitLLMFailed)
	}
	p.Status = stVerified
	p.CommandsProposed = len(prop.Commands)
	p.Rationale = prop.Rationale
	for _, r := range sim.Validate(sc, obs, prop.Commands, sc.CommandBudget) {
		if r.OK {
			p.CommandsAccepted++
		} else {
			p.CommandsRejected = append(p.CommandsRejected, llmRejection{Index: r.Index, Kind: r.Command.Kind.Name(), Code: string(r.Code)})
		}
	}
	rep.LiveVerified = true
	rep.Reason = "a real completion call returned a parsed structured proposal"
	fmt.Fprintf(stderr, "structured proposal: verified, %d proposed, %d accepted, %d rejected, status %s, %d ms, tokens %d/%d/%d\n",
		p.CommandsProposed, p.CommandsAccepted, len(p.CommandsRejected), p.HTTPStatus, p.LatencyMs,
		p.Usage.PromptTokens, p.Usage.CompletionTokens, p.Usage.TotalTokens)
	fmt.Fprintf(stderr, "AI rationale (not verified): %q\n", p.Rationale)
	return finish(exitOK)
}

// checkModels lists {base}/models and looks for the configured model id.
func checkModels(hc *http.Client, cfg llm.Config) llmStep {
	req, err := http.NewRequest(http.MethodGet, cfg.BaseURL+"/models", nil)
	if err != nil {
		return llmStep{Status: stFailed, Detail: "cannot build request"}
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	start := time.Now()
	resp, err := hc.Do(req)
	lat := time.Since(start).Milliseconds()
	if err != nil {
		// err text embeds the URL; report only the class.
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return llmStep{Status: stInconclusive, Detail: "models request timed out", LatencyMs: lat}
		}
		return llmStep{Status: stInconclusive, Detail: "models request failed (transport error)", LatencyMs: lat}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return llmStep{Status: stFailed, Detail: fmt.Sprintf("provider rejected the key (status %d)", resp.StatusCode), LatencyMs: lat}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented:
		return llmStep{Status: stInconclusive, Detail: "availability endpoint unsupported", LatencyMs: lat}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return llmStep{Status: stInconclusive, Detail: fmt.Sprintf("models request returned status %d", resp.StatusCode), LatencyMs: lat}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(raw, &list) != nil || list.Data == nil {
		return llmStep{Status: stInconclusive, Detail: "models response not in the OpenAI list format", LatencyMs: lat}
	}
	for _, m := range list.Data {
		if m.ID == cfg.Model {
			return llmStep{Status: stVerified, Detail: "configured model id is listed", LatencyMs: lat}
		}
	}
	return llmStep{Status: stFailed, Detail: "configured model id is not in the provider list", LatencyMs: lat}
}

// flagshipObservation runs the engine without commands to the demo fork tick
// of the flagship scenario and returns the Observation there.
func flagshipObservation(path string, seed uint64) (sim.Scenario, sim.Observation, int64, error) {
	data := scenarios.BlackFriday
	if path != "" {
		var err error
		if data, err = os.ReadFile(path); err != nil {
			return sim.Scenario{}, sim.Observation{}, 0, fmt.Errorf("read scenario: %w", err)
		}
	}
	sc, err := sim.ParseScenario(data)
	if err != nil {
		return sim.Scenario{}, sim.Observation{}, 0, fmt.Errorf("parse scenario: %w", err)
	}
	if err := sim.ValidateScenario(sc); err != nil {
		return sim.Scenario{}, sim.Observation{}, 0, fmt.Errorf("validate scenario: %w", err)
	}
	plan, err := demoplan.Derive(*sc, sim.Seed(seed))
	if err != nil {
		return sim.Scenario{}, sim.Observation{}, 0, fmt.Errorf("demo fork tick: %w", err)
	}
	st, err := sim.Init(*sc, sim.Seed(seed))
	if err != nil {
		return sim.Scenario{}, sim.Observation{}, 0, fmt.Errorf("init: %w", err)
	}
	for int64(st.Tick()) < plan.ForkTick {
		step, err := sim.Advance(*sc, st, nil)
		if err != nil {
			return sim.Scenario{}, sim.Observation{}, 0, fmt.Errorf("advance: %w", err)
		}
		st = step.State
	}
	return *sc, sim.Observe(*sc, st), plan.ForkTick, nil
}
