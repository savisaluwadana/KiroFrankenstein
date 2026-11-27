package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// maxRecordedTokens bounds a provider-reported token count before it is
// stored (the column is a 32-bit integer; a hostile or buggy provider must not
// overflow it or the World's counter).
const maxRecordedTokens = 1 << 24

// LLMStatus is the per-World LLM usage read model (Req 20.16). When the
// server has no LLM configuration only Available (false) is meaningful.
type LLMStatus struct {
	Available       bool
	Model           string // the model identifier; never the API key
	CallsMade       int
	CallsRemaining  int
	TokensUsed      int
	TokensRemaining int
}

// LLMProvider is the LLM side of a BranchService: the configuration, the HTTP
// client and one call and token Tracker per World. Counters are rebuilt from
// the stored decision records the first time a World is touched after a
// start, so they survive a restart. It is safe for concurrent use.
type LLMProvider struct {
	cfg    llm.Config
	client *llm.Client
	svc    *BranchService

	mu     sync.Mutex
	worlds map[WorldID]*llmWorld

	globalMu    sync.Mutex
	globalReady bool
	now         func() time.Time
}

// llmWorld is the live state of one World's LLM usage.
type llmWorld struct {
	tracker *llm.Tracker

	mu    sync.Mutex
	cache map[BranchID]cachedDecision
}

// cachedDecision keeps the last LLM outcome of a Branch so that a retry of a
// window whose record write failed makes no second request (Req 20.7).
type cachedDecision struct {
	tick sim.Tick
	dec  Decision
}

// WithLLM registers the LLM AI manager. cfg must be Available. httpClient may
// be nil (tests pass the httptest client). A Branch managed by ManagerAILLM
// then asks the provider once per Decision_Window and falls back to the
// rule-based proposals, recorded with the reason, when that fails.
func WithLLM(cfg llm.Config, httpClient *http.Client) (Option, error) {
	client, err := llm.NewClient(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	return func(s *BranchService) {
		p := &LLMProvider{cfg: cfg, client: client, svc: s, worlds: map[WorldID]*llmWorld{}, now: time.Now}
		s.llm = p
		s.ai.factories[ManagerAILLM] = p.factory
	}, nil
}

func (p *LLMProvider) factory(sc sim.Scenario) AIDecider {
	return llmDecider{p: p, rb: RuleBasedDecider(sc)}
}

// ensureGlobal rebuilds the server-wide daily usage from the decision records
// created since the start of the current UTC day, once per start. The
// Tracker itself resets when the UTC day changes while running.
func (p *LLMProvider) ensureGlobal(ctx context.Context) error {
	p.globalMu.Lock()
	defer p.globalMu.Unlock()
	if p.globalReady {
		return nil
	}
	n := p.now().UTC()
	sum, err := p.svc.ai.dec.LLMUsageSince(ctx, time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC))
	if err != nil {
		return fmt.Errorf("restore global llm usage: %w", err)
	}
	p.client.Global().Restore(llm.Usage{
		Calls: sum.Calls, PromptTokens: sum.PromptTokens, CompletionTokens: sum.CompletionTokens,
		TotalTokens: sum.PromptTokens + sum.CompletionTokens,
	})
	p.globalReady = true
	return nil
}

// world returns the live state of a World, rebuilding the counters from the
// decision records of all its Branches on first use.
func (p *LLMProvider) world(ctx context.Context, id WorldID) (*llmWorld, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if w := p.worlds[id]; w != nil {
		return w, nil
	}
	branches, err := p.svc.store.ListBranches(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("restore llm usage of world %s: %w", id, err)
	}
	var u llm.Usage
	for _, b := range branches {
		if !isAIManager(b.ManagerKind) {
			continue
		}
		recs, err := p.svc.ai.dec.DecisionsForBranch(ctx, b.ID)
		if err != nil {
			return nil, fmt.Errorf("restore llm usage of world %s: %w", id, err)
		}
		for _, r := range recs {
			u.Calls += r.LLMCallsCharged
			u.PromptTokens += r.PromptTokens
			u.CompletionTokens += r.CompletionTokens
		}
	}
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	t := llm.NewTracker(p.cfg)
	t.Restore(u)
	w := &llmWorld{tracker: t, cache: map[BranchID]cachedDecision{}}
	p.worlds[id] = w
	return w, nil
}

// LLMAvailable reports whether the server has an LLM configuration and the
// model identifier when it does. It never returns the key or the endpoint.
func (s *BranchService) LLMAvailable() (available bool, model string) {
	if s.llm == nil {
		return false, ""
	}
	return true, s.llm.cfg.Model
}

// LLMStatus reports the World's LLM availability and usage. It never includes
// the API key or the endpoint URL.
func (s *BranchService) LLMStatus(ctx context.Context, id WorldID) (LLMStatus, error) {
	if s.llm == nil {
		return LLMStatus{}, nil
	}
	w, err := s.llm.world(ctx, id)
	if err != nil {
		return LLMStatus{}, err
	}
	u := w.tracker.Usage()
	maxCalls, maxTok := w.tracker.Limits()
	return LLMStatus{
		Available: true, Model: s.llm.cfg.Model,
		CallsMade: u.Calls, CallsRemaining: max(maxCalls-u.Calls, 0),
		TokensUsed: u.TotalTokens, TokensRemaining: max(maxTok-u.TotalTokens, 0),
	}, nil
}

// LLMGlobalStatus reports the calls and tokens left in today's (UTC)
// server-wide LLM limits. ok is false when no LLM is configured. It never
// includes the API key or the endpoint URL.
func (s *BranchService) LLMGlobalStatus(ctx context.Context) (callsRemaining, tokensRemaining int, ok bool, err error) {
	if s.llm == nil {
		return 0, 0, false, nil
	}
	if err := s.llm.ensureGlobal(ctx); err != nil {
		return 0, 0, true, err
	}
	c, t := s.llm.client.Global().Remaining()
	return c, t, true, nil
}

// llmDecider is the AIDecider of ManagerAILLM Branches of one World.
type llmDecider struct {
	p  *LLMProvider
	rb AIDecider
}

// Decide makes at most one provider request for the window. Every provider
// failure, including an exhausted call or token budget, yields the
// rule-based proposals of the same window with provenance
// ai_rule_based_fallback and the reason. A well-formed proposal is returned
// as ai_llm even if the validator later rejects all of it. It returns an error
// only for failures of Parallel itself (store, rule-based manager).
//
// Decide is the synchronous form. The BranchService does not use it: it calls
// Prepare under its lock and runs the returned call without the lock.
func (d llmDecider) Decide(ctx context.Context, b Branch, obs sim.Observation, budget int) (Decision, error) {
	imm, run, err := d.Prepare(ctx, b, obs, budget)
	if err != nil {
		return Decision{}, err
	}
	if imm != nil {
		return *imm, nil
	}
	return run(ctx)
}

// Prepare is the part of Decide that needs the BranchService lock. It returns
// either a finished Decision (cache hit, spent Command_Budget, exhausted
// per-World budget) or a run function that makes the one provider request
// and needs no lock. The call and token reservation is taken here, so the
// per-World caps hold however many windows are in flight. The rule-based
// fallback and the prompt are computed here too, so run reads nothing shared.
func (d llmDecider) Prepare(ctx context.Context, b Branch, obs sim.Observation, budget int) (*Decision, func(context.Context) (Decision, error), error) {
	w, err := d.p.world(ctx, b.WorldID)
	if err != nil {
		return nil, nil, err
	}
	w.mu.Lock()
	c, ok := w.cache[b.ID]
	w.mu.Unlock()
	if ok && c.tick == obs.Tick {
		dec := c.dec
		return &dec, nil, nil
	}
	model := d.p.cfg.Model
	if budget <= 0 {
		return &Decision{Provenance: ProvAILLM, Model: model, Rationale: "No Command_Budget left in this window, so no LLM request was made."}, nil, nil
	}
	if err := d.p.ensureGlobal(ctx); err != nil {
		return nil, nil, err
	}
	system := llm.SystemPrompt(budget)
	user, err := llm.UserPrompt(obs, budget)
	if err != nil {
		dec, err := d.fallback(ctx, b, obs, budget, llm.FallbackReason(fmt.Errorf("%w: %w", llm.ErrHTTP, err)), 0, llm.Usage{})
		return &dec, nil, err
	}
	// Reserve estimated prompt tokens + max_tokens against the World and the
	// server-wide limits; an oversized prompt reserves nothing.
	res, err := d.p.client.Reserve(w.tracker, system, user)
	if err != nil {
		dec, err := d.fallback(ctx, b, obs, budget, llm.FallbackReason(err), 0, llm.Usage{})
		return &dec, nil, err
	}
	// The fallback is computed now, under the caller's lock; it is only used
	// when the request fails.
	probe, err := d.fallback(ctx, b, obs, budget, "", 1, llm.Usage{})
	if err != nil {
		res.Release()
		return nil, nil, err
	}
	run := func(ctx context.Context) (Decision, error) {
		content, raw, err := d.p.client.Complete(ctx, system, user)
		calls := 1
		var u llm.Usage
		if errors.Is(err, llm.ErrSaturated) {
			res.Release() // no request was made: the call is not charged
			calls = 0
		} else {
			if !raw.Unreported {
				raw.PromptTokens, raw.CompletionTokens = clampTokens(raw.PromptTokens), clampTokens(raw.CompletionTokens)
			}
			u = res.Settle(raw)
			if u.TotalTokens > res.Tokens {
				slog.Warn("llm provider reported more tokens than reserved; actual usage accounted",
					"reserved", res.Tokens, "actual", u.TotalTokens, "world", string(b.WorldID))
			}
		}
		withUsage := func(reason string) Decision {
			dec := probe
			dec.FallbackReason, dec.PromptTokens, dec.CompletionTokens, dec.LLMCallsCharged = reason, u.PromptTokens, u.CompletionTokens, calls
			return dec
		}
		var dec Decision
		if err != nil {
			dec = withUsage(llm.FallbackReason(err))
		} else if cmds, rationale, perr := llm.ParseProposal(content, budget); perr != nil {
			dec = withUsage(llm.FallbackReason(perr))
		} else {
			dec = Decision{
				Commands: cmds, Rationale: rationale, Provenance: ProvAILLM, Model: model,
				LLMCallsCharged: 1, PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
			}
		}
		w.mu.Lock()
		w.cache[b.ID] = cachedDecision{tick: obs.Tick, dec: dec}
		w.mu.Unlock()
		return dec, nil
	}
	return nil, run, nil
}

// Forget drops the cached outcome of the window at tick. The BranchService
// calls it when it discards a stale answer, so the retry asks again.
func (d llmDecider) Forget(ctx context.Context, b Branch, tick sim.Tick) {
	w, err := d.p.world(ctx, b.WorldID)
	if err != nil {
		return
	}
	w.mu.Lock()
	if c, ok := w.cache[b.ID]; ok && c.tick == tick {
		delete(w.cache, b.ID)
	}
	w.mu.Unlock()
}

func (d llmDecider) fallback(ctx context.Context, b Branch, obs sim.Observation, budget int, reason string, calls int, u llm.Usage) (Decision, error) {
	dec, err := d.rb.Decide(ctx, b, obs, budget)
	if err != nil {
		return Decision{}, fmt.Errorf("rule-based fallback (%s): %w", reason, err)
	}
	dec.Provenance, dec.FallbackReason, dec.Model = ProvAIRuleBasedFallback, reason, ""
	dec.LLMCallsCharged, dec.PromptTokens, dec.CompletionTokens = calls, u.PromptTokens, u.CompletionTokens
	return dec, nil
}

func clampTokens(n int) int { return min(max(n, 0), maxRecordedTokens) }
