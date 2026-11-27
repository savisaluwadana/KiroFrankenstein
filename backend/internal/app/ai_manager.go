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

// ErrDecisionStale: the AI's answer for a window arrived after the Branch
// changed (it was advanced, a Command was submitted, the window was ended, a
// Disruption was injected). The answer is discarded and nothing is recorded;
// retrying the call asks again. It is mapped to 409.
var ErrDecisionStale = errors.New("app: the ai decision arrived after the branch changed")

// externalDecider is an AIDecider whose answer needs a slow request (the LLM).
// The BranchService never calls Decide on it; it calls Prepare under its lock
// and runs the returned function with the lock released.
type externalDecider interface {
	AIDecider
	// Prepare returns either a finished Decision or a function that makes the
	// request. Called with the BranchService lock held; the reservation of the
	// per-World budget happens here.
	Prepare(ctx context.Context, b Branch, obs sim.Observation, budget int) (*Decision, func(context.Context) (Decision, error), error)
	// Forget drops anything cached for the window at tick (a stale answer).
	Forget(ctx context.Context, b Branch, tick sim.Tick)
}

// flightKey identifies one in-flight provider request: one per Branch window.
type flightKey struct {
	branch BranchID
	tick   sim.Tick
}

// flight is the single in-flight external decision of a window. Every caller
// that needs the window's decision waits on done; err is final once it is
// closed. The goroutine that runs the request also commits the result, so a
// caller that disconnects does not lose the decision.
type flight struct {
	done chan struct{}
	err  error

	// What the decision depends on, captured under the lock.
	key      flightKey
	version  uint64 // branch version at capture
	used     int64  // Commands already stored for the window
	engine   string // World engine version
	remained int64
}

// bump marks a state change of the Branch. An in-flight decision captured
// before it is stale. Caller holds s.mu.
func (s *BranchService) bump(id BranchID) { s.ver[id]++ }

// beginDecisionLocked runs the AI manager of c's Branch at the open
// Decision_Window at its head, when the answer needs no slow request, and ends
// the window. It is a no-op when the head is not an open window or the window
// already has a record, so a retry after a failure never decides twice.
// Proposals go through submitLocked, the same gate as a Human's
// SubmitCommands, so the AI cannot bypass validation, the budget or the cash
// rules (INV-14). A record is written even with zero Commands.
//
// When the decider is external (LLM) it returns a flight instead: the
// reservation is taken and the request is started with the lock released
// (goroutine), and the caller must wait for the flight without the lock and
// call again. A second caller for the same window joins the same flight.
// Caller holds s.mu.
func (s *BranchService) beginDecisionLocked(ctx context.Context, c branchCtx) (*flight, error) {
	st := c.head.state
	tick := st.Tick()
	if int64(tick) >= c.sc.HorizonTicks {
		return nil, nil
	}
	if open, _ := windowAt(c.sc, c.b, st); !open {
		return nil, nil
	}
	prior, err := s.ai.dec.DecisionsForBranch(ctx, c.b.ID)
	if err != nil {
		return nil, fmt.Errorf("decision records of branch %s: %w", c.b.ID, err)
	}
	for _, r := range prior {
		if r.WindowTick == tick {
			s.ended[c.b.ID] = tick
			return nil, nil
		}
	}
	key := flightKey{c.b.ID, tick}
	if fl := s.inflight[key]; fl != nil {
		return fl, nil // join: exactly one provider request per window
	}

	used, err := s.budgetUsed(ctx, c.b.ID, tick)
	if err != nil {
		return nil, err
	}
	remaining := max(c.sc.CommandBudget-used, 0)
	d, fellBack := s.decider(c)
	obs := observeWindow(c.sc, c.b, st)
	ext, isExt := d.(externalDecider)
	if !isExt || fellBack {
		dec, err := d.Decide(ctx, c.b, obs, int(remaining))
		if err != nil {
			return nil, fmt.Errorf("ai manager of branch %s at tick %d: %w", c.b.ID, tick, err)
		}
		return nil, s.commitDecisionLocked(ctx, c, dec, remaining, fellBack)
	}
	imm, run, err := ext.Prepare(ctx, c.b, obs, int(remaining))
	if err != nil {
		return nil, fmt.Errorf("ai manager of branch %s at tick %d: %w", c.b.ID, tick, err)
	}
	if imm != nil {
		return nil, s.commitDecisionLocked(ctx, c, s.withStaleUsage(key, *imm), remaining, false)
	}
	fl := &flight{
		done: make(chan struct{}), key: key, version: s.ver[c.b.ID], used: used,
		engine: c.w.EngineVersion, remained: remaining,
	}
	s.inflight[key] = fl
	// Detached from the caller: the answer is still committed if the caller
	// disconnects. The client applies the configured request timeout itself.
	rctx := context.WithoutCancel(ctx)
	go func() {
		dec, rerr := run(rctx)
		fl.err = s.finishFlight(rctx, fl, ext, c.b, dec, rerr)
		close(fl.done)
	}()
	return fl, nil
}

// finishFlight revalidates what the decision depended on and, when nothing
// changed, commits it through the same gate as a Human's Commands. A changed
// Branch discards the answer (ErrDecisionStale); the call it cost stays
// counted in the World's usage.
func (s *BranchService) finishFlight(ctx context.Context, fl *flight, ext externalDecider, b Branch, dec Decision, runErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, fl.key)
	if runErr != nil {
		return fmt.Errorf("ai manager of branch %s at tick %d: %w", fl.key.branch, fl.key.tick, runErr)
	}
	stale := func(why string) error {
		s.staleDecisions.Add(1)
		slog.Warn("ai-decision-stale", "branch", string(fl.key.branch), "tick", int64(fl.key.tick), "why", why)
		ext.Forget(ctx, b, fl.key.tick)
		// The discarded call stays charged: carry it into the retry's record.
		u := s.staleUsage[fl.key]
		u.calls += dec.LLMCallsCharged
		u.prompt += dec.PromptTokens
		u.completion += dec.CompletionTokens
		s.staleUsage[fl.key] = u
		return ErrDecisionStale
	}
	if s.ver[fl.key.branch] != fl.version {
		return stale("branch version changed")
	}
	c, err := s.loadMutable(ctx, fl.key.branch)
	if err != nil {
		return stale("branch not loadable: " + err.Error())
	}
	if c.head.state.Tick() != fl.key.tick {
		return stale("head moved")
	}
	if open, _ := windowAt(c.sc, c.b, c.head.state); !open || s.endedAt(fl.key.branch, fl.key.tick) {
		return stale("window closed")
	}
	if c.w.EngineVersion != fl.engine {
		return stale("engine version changed")
	}
	prior, err := s.ai.dec.DecisionsForBranch(ctx, fl.key.branch)
	if err != nil {
		return fmt.Errorf("decision records of branch %s: %w", fl.key.branch, err)
	}
	for _, r := range prior {
		if r.WindowTick == fl.key.tick {
			return stale("window already decided")
		}
	}
	used, err := s.budgetUsed(ctx, fl.key.branch, fl.key.tick)
	if err != nil {
		return err
	}
	if used != fl.used {
		return stale("command count changed")
	}
	return s.commitDecisionLocked(ctx, c, s.withStaleUsage(fl.key, dec), fl.remained, false)
}

// withStaleUsage adds the usage of discarded answers of the same window to dec.
func (s *BranchService) withStaleUsage(k flightKey, dec Decision) Decision {
	if u, ok := s.staleUsage[k]; ok {
		dec.LLMCallsCharged += u.calls
		dec.PromptTokens += u.prompt
		dec.CompletionTokens += u.completion
		delete(s.staleUsage, k)
	}
	return dec
}

// staleCharge is usage of discarded answers waiting to be recorded.
type staleCharge struct{ calls, prompt, completion int }

// commitDecisionLocked validates and stores dec for the window at c's head and
// writes the decision record, then ends the window. Caller holds s.mu.
func (s *BranchService) commitDecisionLocked(ctx context.Context, c branchCtx, dec Decision, remaining int64, fellBack bool) error {
	tick := c.head.state.Tick()
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
	s.bump(c.b.ID)
	return nil
}

// ensureDecided makes the AI Branch decide at an open window at its head and
// returns once the decision is recorded. Caller must NOT hold s.mu: it waits
// for a slow provider request without the lock. A stale answer is discarded
// and the request is made once more; a second stale answer is ErrDecisionStale.
func (s *BranchService) ensureDecided(ctx context.Context, id BranchID) error {
	for attempt := 0; attempt < 2; attempt++ {
		s.mu.Lock()
		c, err := s.loadMutable(ctx, id)
		var fl *flight
		if err == nil && isAIManager(c.b.ManagerKind) {
			fl, err = s.beginDecisionLocked(ctx, c)
		}
		s.mu.Unlock()
		if err != nil || fl == nil {
			return err
		}
		if err := waitFlight(ctx, fl); err == nil {
			return nil
		} else if !errors.Is(err, ErrDecisionStale) {
			return err
		}
	}
	return ErrDecisionStale
}

// waitFlight waits for fl or for the caller to give up. The flight itself
// continues and is committed either way.
func waitFlight(ctx context.Context, fl *flight) error {
	select {
	case <-fl.done:
		return fl.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// decideAtHead runs the AI manager at the head window of a freshly created
// Branch (World root or fork). A failure is logged and left for the lazy
// decision at the start of the next Advance. Caller must not hold s.mu.
func (s *BranchService) decideAtHead(ctx context.Context, id BranchID) {
	if err := s.ensureDecided(ctx, id); err != nil {
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
