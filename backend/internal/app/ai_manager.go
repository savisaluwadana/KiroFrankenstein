package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/rulebased"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// FallbackLLMUnavailable is the fallback reason recorded when a Branch is
// managed by the LLM AI but no LLM decider is registered, that is when the
// server has no LLM configuration (Req 20.8, 20.9). Once WithLLM registers the
// LLM decider, every fallback carries one of the five provider reasons
// (timeout, http_error, invalid_json, schema_violation, budget_exhausted).
const FallbackLLMUnavailable = "llm_unavailable"

// Decision is what an AIDecider returns for one Decision_Window: the proposed
// Commands in priority order (at most the budget), the untrusted rationale and
// how the proposal came about. It carries proposals only; the BranchService
// validates and records them.
type Decision struct {
	Commands       []sim.Command
	Rationale      string
	Provenance     Provenance // empty means ProvAIRuleBased
	Model          string     // LLM model name, empty otherwise
	FallbackReason string     // set with ProvAIRuleBasedFallback

	// LLM usage of the window; see DecisionRecord.
	LLMCallsCharged  int
	PromptTokens     int
	CompletionTokens int
}

// AIDecider is the hook through which an AI manager takes part in a
// Decision_Window. It sees only the Observation a Human would see and the
// remaining Command budget, and it never mutates state. The rule-based
// decider is built in; the LLM decider (with its rule-based fallback) plugs in
// through the same interface (task 6.5).
type AIDecider interface {
	Decide(ctx context.Context, b Branch, obs sim.Observation, budget int) (Decision, error)
}

// AIDeciderFactory builds the decider of one World from its Scenario.
type AIDeciderFactory func(sc sim.Scenario) AIDecider

// WithAIDecider registers (or replaces) the decider factory for a manager kind.
func WithAIDecider(kind ManagerKind, f AIDeciderFactory) Option {
	return func(s *BranchService) { s.ai.factories[kind] = f }
}

// PolicyDecider adapts a policy.Manager to AIDecider, stamping every decision
// with prov.
func PolicyDecider(m policy.Manager, prov Provenance) AIDecider {
	return policyDecider{m: m, prov: prov}
}

type policyDecider struct {
	m    policy.Manager
	prov Provenance
}

func (d policyDecider) Decide(ctx context.Context, _ Branch, obs sim.Observation, budget int) (Decision, error) {
	p, err := d.m.Propose(ctx, obs, budget)
	if err != nil {
		return Decision{}, fmt.Errorf("%s: %w", d.m.Name(), err)
	}
	return Decision{Commands: p.Commands, Rationale: p.Rationale, Provenance: d.prov}, nil
}

// RuleBasedDecider is the factory of the deterministic rule-based AI. The
// rule-based manager drops the Disruption_Script when it is built.
func RuleBasedDecider(sc sim.Scenario) AIDecider {
	return PolicyDecider(rulebased.New(sc), ProvAIRuleBased)
}

func isAIManager(k ManagerKind) bool { return k == ManagerAIRuleBased || k == ManagerAILLM }

type deciderKey struct {
	world WorldID
	kind  ManagerKind
}

// aiState is the AI part of a BranchService. Caller holds BranchService.mu
// for everything except construction.
type aiState struct {
	factories map[ManagerKind]AIDeciderFactory
	deciders  map[deciderKey]AIDecider
	dec       DecisionRepo
}

// memDecisions keeps records in memory for stores without a DecisionRepo.
type memDecisions struct {
	mu sync.Mutex
	m  map[BranchID][]DecisionRecord
}

func (d *memDecisions) AppendDecision(_ context.Context, r DecisionRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, e := range d.m[r.BranchID] {
		if e.WindowTick == r.WindowTick {
			return fmt.Errorf("decision of branch %s tick %d: %w", r.BranchID, r.WindowTick, ErrAlreadyExists)
		}
	}
	d.m[r.BranchID] = append(d.m[r.BranchID], r)
	sort.Slice(d.m[r.BranchID], func(i, j int) bool { return d.m[r.BranchID][i].WindowTick < d.m[r.BranchID][j].WindowTick })
	return nil
}

func (d *memDecisions) DecisionsForBranch(_ context.Context, id BranchID) ([]DecisionRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]DecisionRecord(nil), d.m[id]...), nil
}

// observeWindow is the Observation of a Branch at state st as a Human gets it:
// sim.Observe plus the Decision_Window flags, including the fork window.
func observeWindow(sc sim.Scenario, b Branch, st sim.State) sim.Observation {
	obs := sim.Observe(sc, st)
	open, reasons := windowAt(sc, b, st)
	obs.Window.Open, obs.Window.Reasons = open, reasons
	return obs
}

// decider returns the decider of c's World and manager kind. A Branch managed
// by the LLM AI with no registered LLM decider uses the rule-based decider,
// recorded as ai_rule_based_fallback. Caller holds s.mu.
func (s *BranchService) decider(c branchCtx) (d AIDecider, fallback bool) {
	kind := c.b.ManagerKind
	f, ok := s.ai.factories[kind]
	if !ok {
		kind, f, fallback = ManagerAIRuleBased, s.ai.factories[ManagerAIRuleBased], true
		if f == nil {
			f = RuleBasedDecider
		}
	}
	key := deciderKey{c.w.ID, kind}
	if d = s.ai.deciders[key]; d == nil {
		d = f(c.sc)
		s.ai.deciders[key] = d
	}
	return d, fallback
}

// decideLocked runs the AI manager of c's Branch at the open Decision_Window at
// its head and ends the window. It is a no-op when the head is not an open
// window or the window already has a record, so a retry after a failure never
// decides twice. Proposals go through submitLocked, the same gate as a Human's
// SubmitCommands, so the AI cannot bypass validation, the budget or the cash
// rules (INV-14). A record is written even with zero Commands. Caller holds s.mu.
func (s *BranchService) decideLocked(ctx context.Context, c branchCtx) error {
	st := c.head.state
	tick := st.Tick()
	if int64(tick) >= c.sc.HorizonTicks {
		return nil
	}
	if open, _ := windowAt(c.sc, c.b, st); !open {
		return nil
	}
	prior, err := s.ai.dec.DecisionsForBranch(ctx, c.b.ID)
	if err != nil {
		return fmt.Errorf("decision records of branch %s: %w", c.b.ID, err)
	}
	for _, r := range prior {
		if r.WindowTick == tick {
			s.ended[c.b.ID] = tick
			return nil
		}
	}

	used, err := s.budgetUsed(ctx, c.b.ID, tick)
	if err != nil {
		return err
	}
	remaining := max(c.sc.CommandBudget-used, 0)
	d, fellBack := s.decider(c)
	dec, err := d.Decide(ctx, c.b, observeWindow(c.sc, c.b, st), int(remaining))
	if err != nil {
		return fmt.Errorf("ai manager of branch %s at tick %d: %w", c.b.ID, tick, err)
	}
	if dec.Provenance == "" {
		dec.Provenance = ProvAIRuleBased
	}
	if fellBack {
		dec.Provenance, dec.FallbackReason = ProvAIRuleBasedFallback, FallbackLLMUnavailable
	}
	dec.Rationale = truncateRunes(dec.Rationale, MaxRationaleRunes)

	rec := DecisionRecord{
		BranchID: c.b.ID, WindowTick: tick, ManagerKind: c.b.ManagerKind,
		Provenance: dec.Provenance, Model: dec.Model, FallbackReason: dec.FallbackReason,
		Rationale: dec.Rationale, Proposed: append([]sim.Command(nil), dec.Commands...), CreatedAt: s.now(),
		LLMCallsCharged: dec.LLMCallsCharged, PromptTokens: dec.PromptTokens, CompletionTokens: dec.CompletionTokens,
	}
	switch {
	case len(dec.Commands) == 0:
	case remaining <= 0:
		for i, cmd := range dec.Commands {
			rec.Rejected = append(rec.Rejected, RejectedProposal{
				Index: i, Command: cmd, Code: string(sim.ReasonBudgetExhausted), Message: "the Command_Budget of this window is used up",
			})
		}
	default:
		res, err := s.submitLocked(ctx, c, SubmitRequest{
			Branch: c.b.ID, WindowTick: tick, Commands: dec.Commands,
			IdempotencyKey: fmt.Sprintf("ai-window-%d", tick),
			Metadata:       Metadata{Provenance: dec.Provenance, Rationale: dec.Rationale, ModelName: dec.Model},
		})
		if err != nil {
			return fmt.Errorf("submit ai commands of branch %s at tick %d: %w", c.b.ID, tick, err)
		}
		rec.AcceptedCount = len(res.Accepted)
		for _, v := range res.Rejected {
			rec.Rejected = append(rec.Rejected, RejectedProposal{Index: v.Index, Command: v.Command, Code: string(v.Code), Message: v.Message})
		}
	}
	if err := s.ai.dec.AppendDecision(ctx, rec); err != nil && !errors.Is(err, ErrAlreadyExists) {
		return fmt.Errorf("record decision of branch %s at tick %d: %w", c.b.ID, tick, err)
	}
	s.ended[c.b.ID] = tick
	return nil
}

// decideAtHead runs the AI manager at the head window of a freshly created
// Branch (World root or fork). A failure is logged and left for the lazy
// decision at the start of the next Advance. Caller holds s.mu.
func (s *BranchService) decideAtHead(ctx context.Context, id BranchID) {
	c, err := s.loadMutable(ctx, id)
	if err == nil {
		err = s.decideLocked(ctx, c)
	}
	if err != nil {
		slog.Warn("ai-window-decision", "branch", string(id), "err", err)
	}
}

// DecisionRecords returns the per-window records of an AI Branch in window
// order. A Human Branch has none.
func (s *BranchService) DecisionRecords(ctx context.Context, id BranchID) ([]DecisionRecord, error) {
	if _, err := s.store.GetBranch(ctx, id); err != nil {
		return nil, fmt.Errorf("branch %s: %w", id, err)
	}
	recs, err := s.ai.dec.DecisionsForBranch(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("decision records of branch %s: %w", id, err)
	}
	return recs, nil
}
