package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// DuelID identifies a Duel.
type DuelID string

// ComparisonPolicyMinHead is the one Comparison_Tick policy of the MVP: the
// lower of the two Branches' Head_Ticks (Req 22.1).
const ComparisonPolicyMinHead = "min_head_tick"

// Labels of the two Duel Branches.
const (
	DuelHumanLabel = "You"
	DuelAILabel    = "Rule-based AI"
	DuelLLMLabel   = "LLM AI"
)

// Duel errors.
var (
	// ErrDuelWindowOpen: the Duel is at a Decision_Window the Human Manager
	// has not ended yet; neither Branch advances (Req 21.3).
	ErrDuelWindowOpen = errors.New("app: duel is waiting for the human manager to end the decision window")
	// ErrDuelDiverged: the Human Branch is behind the AI Branch, which only
	// happens when the AI Branch was advanced outside the Duel.
	ErrDuelDiverged = errors.New("app: duel branches have diverged in time")
	// ErrInvalidAIKind: a Duel's AI Manager must be rule-based or LLM.
	ErrInvalidAIKind = errors.New("app: duel ai manager kind must be ai_rule_based or ai_llm")
)

// Duel is the stored record of a head-to-head: two sibling Branches forked
// from one parent at ForkTick. It is metadata; the engine never reads it.
type Duel struct {
	ID               DuelID
	WorldID          WorldID
	ParentBranchID   BranchID
	HumanBranchID    BranchID
	AIBranchID       BranchID
	AIKind           ManagerKind
	ForkTick         sim.Tick
	ComparisonPolicy string
	CreatedAt        time.Time
}

// DuelRepo stores Duels. It has no update or delete method: a Duel never
// changes after creation.
type DuelRepo interface {
	// CreateDuel stores d; a duplicate ID wraps ErrAlreadyExists and an unknown
	// World or Branch wraps ErrNotFound.
	CreateDuel(ctx context.Context, d Duel) error
	// GetDuel wraps ErrNotFound for an unknown ID.
	GetDuel(ctx context.Context, id DuelID) (Duel, error)
}

// DuelBranchLookup is an optional DuelRepo capability: find the Duel that owns
// a Branch as its Human or AI side. It reports ok=false for any other Branch.
type DuelBranchLookup interface {
	DuelForBranch(ctx context.Context, id BranchID) (Duel, bool, error)
}

type memDuels struct {
	mu sync.Mutex
	m  map[DuelID]Duel
}

func (r *memDuels) DuelForBranch(_ context.Context, id BranchID) (Duel, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.m {
		if d.HumanBranchID == id || d.AIBranchID == id {
			return d, true, nil
		}
	}
	return Duel{}, false, nil
}

func (r *memDuels) CreateDuel(_ context.Context, d Duel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[d.ID]; ok {
		return fmt.Errorf("duel %s: %w", d.ID, ErrAlreadyExists)
	}
	r.m[d.ID] = d
	return nil
}

func (r *memDuels) GetDuel(_ context.Context, id DuelID) (Duel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.m[id]
	if !ok {
		return Duel{}, fmt.Errorf("duel %s: %w", id, ErrNotFound)
	}
	return d, nil
}

// DuelService runs Duels over a BranchService. Duel operations that move time
// or append to both Branches are serialised by one mutex; each Branch call
// below it takes the BranchService mutex on its own, so nothing here holds
// the BranchService lock across calls.
type DuelService struct {
	bs    *BranchService
	repo  DuelRepo
	newID func() string
	now   func() time.Time
	mu    sync.Mutex
}

// NewDuelService returns a DuelService. A nil repo uses the BranchService's
// store when it implements DuelRepo, else an in-memory repository.
func NewDuelService(bs *BranchService, repo DuelRepo) *DuelService {
	if repo == nil {
		if r, ok := bs.store.(DuelRepo); ok {
			repo = r
		} else {
			repo = &memDuels{m: map[DuelID]Duel{}}
		}
	}
	return &DuelService{bs: bs, repo: repo, newID: bs.newID, now: bs.now}
}

// CreateDuelRequest names the parent Branch, the fork Tick and the AI kind.
// An empty AIKind means the rule-based AI. An LLM AI with no LLM decider
// registered plays as the rule-based fallback, recorded with provenance
// ai_rule_based_fallback (Req 20.8).
type CreateDuelRequest struct {
	Parent   BranchID
	ForkTick sim.Tick
	AIKind   ManagerKind
}

// CreateDuel forks two sibling Branches of Parent at ForkTick: the Human one
// (label "You") and the AI one. Both share the parent's history up to ForkTick
// and therefore the same seed, Scenario and exogenous streams (Req 21.1).
func (d *DuelService) CreateDuel(ctx context.Context, req CreateDuelRequest) (Duel, error) {
	kind := req.AIKind
	if kind == "" {
		kind = ManagerAIRuleBased
	}
	if kind != ManagerAIRuleBased && kind != ManagerAILLM {
		return Duel{}, fmt.Errorf("manager %q: %w", kind, ErrInvalidAIKind)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	parent, err := d.bs.GetBranch(ctx, req.Parent)
	if err != nil {
		return Duel{}, err
	}
	human, err := d.bs.ForkBranch(ctx, req.Parent, req.ForkTick, DuelHumanLabel, ManagerHuman)
	if err != nil {
		return Duel{}, fmt.Errorf("fork human branch: %w", err)
	}
	label := DuelAILabel
	if kind == ManagerAILLM {
		label = DuelLLMLabel
	}
	ai, err := d.bs.ForkBranch(ctx, req.Parent, req.ForkTick, label, kind)
	if err != nil {
		return Duel{}, fmt.Errorf("fork ai branch: %w", err)
	}
	du := Duel{
		ID: DuelID(d.newID()), WorldID: parent.WorldID, ParentBranchID: req.Parent,
		HumanBranchID: human.ID, AIBranchID: ai.ID, AIKind: kind, ForkTick: req.ForkTick,
		ComparisonPolicy: ComparisonPolicyMinHead, CreatedAt: d.now(),
	}
	if err := d.repo.CreateDuel(ctx, du); err != nil {
		return Duel{}, fmt.Errorf("store duel: %w", err)
	}
	return du, nil
}

// DuelOfBranch returns the Duel that owns the Branch as its Human or AI side.
// ok is false for every other Branch, and when the repository cannot look up
// by Branch.
func (d *DuelService) DuelOfBranch(ctx context.Context, id BranchID) (Duel, bool, error) {
	l, ok := d.repo.(DuelBranchLookup)
	if !ok {
		return Duel{}, false, nil
	}
	return l.DuelForBranch(ctx, id)
}

// GetDuel returns the stored Duel.
func (d *DuelService) GetDuel(ctx context.Context, id DuelID) (Duel, error) {
	du, err := d.repo.GetDuel(ctx, id)
	if err != nil {
		return Duel{}, fmt.Errorf("duel %s: %w", id, err)
	}
	return du, nil
}

// branchWindow is the window state of one Branch at its head.
type branchWindow struct {
	head    sim.Tick
	horizon sim.Tick
	open    bool
	reasons []sim.WindowReason
	ended   bool
}

func (s *BranchService) windowStatus(ctx context.Context, id BranchID) (branchWindow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return branchWindow{}, err
	}
	head := c.head.state.Tick()
	w := branchWindow{head: head, horizon: sim.Tick(c.sc.HorizonTicks)}
	if head < w.horizon {
		w.open, w.reasons = windowAt(c.sc, c.b, c.head.state)
		w.ended = s.endedAt(id, head)
	}
	return w, nil
}

// ensureDecided lets an AI Branch decide at an open window at its head. It is
// a no-op when the window already has a record.
func (s *BranchService) ensureDecided(ctx context.Context, id BranchID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.loadMutable(ctx, id)
	if err != nil {
		return err
	}
	if !isAIManager(c.b.ManagerKind) {
		return nil
	}
	return s.decideLocked(ctx, c)
}

// DuelStatus is the lockstep state of a Duel.
type DuelStatus struct {
	Duel           Duel
	HumanHead      sim.Tick
	AIHead         sim.Tick
	ComparisonTick sim.Tick
	Horizon        sim.Tick
	// WindowOpen and WindowReasons describe the Human Branch's window at its
	// head. WaitingOnHuman is true while that window is open and not ended:
	// neither Branch can advance.
	WindowOpen     bool
	WindowReasons  []sim.WindowReason
	WaitingOnHuman bool
	Warnings       []Warning
}

// DuelStatus reports where both Branches are and whether the Duel waits for
// the Human Manager.
func (d *DuelService) DuelStatus(ctx context.Context, id DuelID) (DuelStatus, error) {
	du, err := d.GetDuel(ctx, id)
	if err != nil {
		return DuelStatus{}, err
	}
	hw, err := d.bs.windowStatus(ctx, du.HumanBranchID)
	if err != nil {
		return DuelStatus{}, err
	}
	aw, err := d.bs.windowStatus(ctx, du.AIBranchID)
	if err != nil {
		return DuelStatus{}, err
	}
	warn, err := d.bs.WorldEngineWarnings(ctx, du.WorldID)
	if err != nil {
		return DuelStatus{}, err
	}
	return DuelStatus{
		Duel: du, HumanHead: hw.head, AIHead: aw.head, ComparisonTick: min(hw.head, aw.head), Horizon: hw.horizon,
		WindowOpen: hw.open, WindowReasons: hw.reasons, WaitingOnHuman: hw.open && !hw.ended, Warnings: warn,
	}, nil
}

// SubmitHuman submits the Human Manager's Commands in its window. Branch in
// req is replaced by the Duel's Human Branch.
func (d *DuelService) SubmitHuman(ctx context.Context, id DuelID, req SubmitRequest) (SubmitResult, error) {
	du, err := d.GetDuel(ctx, id)
	if err != nil {
		return SubmitResult{}, err
	}
	req.Branch = du.HumanBranchID
	return d.bs.SubmitCommands(ctx, req)
}

// EndHumanWindow ends the Human Manager's window at windowTick. The AI
// Manager has already decided at that window, so the Duel can advance.
func (d *DuelService) EndHumanWindow(ctx context.Context, id DuelID, windowTick sim.Tick, expected *sim.Tick) error {
	du, err := d.GetDuel(ctx, id)
	if err != nil {
		return err
	}
	return d.bs.EndDecisionWindow(ctx, du.HumanBranchID, windowTick, expected)
}

// AdvanceDuelResult reports one lockstep advance.
type AdvanceDuelResult struct {
	HeadTick      sim.Tick
	Paused        PauseReason
	WindowReasons []sim.WindowReason
	Human         AdvanceResult
	AI            AdvanceResult
}

// AdvanceDuel moves both Branches together. Without runTo they stop at the
// next Decision_Window or the horizon. With runTo the Human Branch's
// intervening windows end with no Commands, the AI decides at every window it
// passes, and both stop at runTo, the horizon or the next Disruption onset
// (Req 21.2, 21.3).
//
// The Duel refuses to advance (ErrDuelWindowOpen) while the Human Manager's
// window at the head is open and not ended. The AI Manager decides at its
// window on arrival, so only the Human can hold the Duel. If an earlier call
// advanced the Human Branch but failed on the AI Branch, the next call only
// brings the AI Branch up to the Human Branch's tick.
func (d *DuelService) AdvanceDuel(ctx context.Context, id DuelID, runTo *sim.Tick) (AdvanceDuelResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	du, err := d.GetDuel(ctx, id)
	if err != nil {
		return AdvanceDuelResult{}, err
	}
	hw, err := d.bs.windowStatus(ctx, du.HumanBranchID)
	if err != nil {
		return AdvanceDuelResult{}, err
	}
	aw, err := d.bs.windowStatus(ctx, du.AIBranchID)
	if err != nil {
		return AdvanceDuelResult{}, err
	}
	switch {
	case aw.head > hw.head:
		return AdvanceDuelResult{}, fmt.Errorf("human at tick %d, ai at tick %d: %w", hw.head, aw.head, ErrDuelDiverged)
	case aw.head < hw.head:
		ares, err := d.bs.Advance(ctx, AdvanceRequest{Branch: du.AIBranchID, RunToTick: &hw.head})
		if err != nil {
			return AdvanceDuelResult{}, fmt.Errorf("catch up ai branch: %w", err)
		}
		return AdvanceDuelResult{HeadTick: ares.HeadTick, Paused: ares.Paused, WindowReasons: ares.WindowReasons, AI: ares}, nil
	}
	if hw.head >= hw.horizon {
		return AdvanceDuelResult{}, ErrHorizonReached
	}
	if runTo != nil && *runTo <= hw.head {
		return AdvanceDuelResult{}, fmt.Errorf("run-to tick %d is not above head %d: %w", *runTo, hw.head, ErrInvalidTick)
	}
	if err := d.bs.ensureDecided(ctx, du.AIBranchID); err != nil {
		return AdvanceDuelResult{}, err
	}
	if hw.open && !hw.ended {
		return AdvanceDuelResult{}, fmt.Errorf("tick %d: %w", hw.head, ErrDuelWindowOpen)
	}
	hres, err := d.bs.Advance(ctx, AdvanceRequest{Branch: du.HumanBranchID, RunToTick: runTo})
	if err != nil {
		return AdvanceDuelResult{}, fmt.Errorf("advance human branch: %w", err)
	}
	target := hres.HeadTick
	ares, err := d.bs.Advance(ctx, AdvanceRequest{Branch: du.AIBranchID, RunToTick: &target})
	if err != nil {
		return AdvanceDuelResult{}, fmt.Errorf("advance ai branch: %w", err)
	}
	return AdvanceDuelResult{
		HeadTick: hres.HeadTick, Paused: hres.Paused, WindowReasons: hres.WindowReasons, Human: hres, AI: ares,
	}, nil
}

// DuelInjectRequest injects one Disruption into both Branches (Req 21.4).
type DuelInjectRequest struct {
	Kind             uint8
	StartTick        sim.Tick
	DurationTicks    int64
	MagnitudeBps     sim.Bps
	Target           int64
	Role             uint8
	ExpectedHeadTick *sim.Tick
	IdempotencyKey   string
}

// DuelInjectResult holds the stored Input of each Branch.
type DuelInjectResult struct {
	Human InjectResult
	AI    InjectResult
}

// InjectDisruption appends the same Disruption with the same start Tick to
// both Branches. Both Branches are at the same head, so both accept or both
// reject. A retry with the same IdempotencyKey completes a half-applied call.
func (d *DuelService) InjectDisruption(ctx context.Context, id DuelID, req DuelInjectRequest) (DuelInjectResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	du, err := d.GetDuel(ctx, id)
	if err != nil {
		return DuelInjectResult{}, err
	}
	one := func(b BranchID) (InjectResult, error) {
		return d.bs.InjectDisruption(ctx, InjectRequest{
			Branch: b, Kind: req.Kind, StartTick: req.StartTick, DurationTicks: req.DurationTicks,
			MagnitudeBps: req.MagnitudeBps, Target: req.Target, Role: req.Role,
			ExpectedHeadTick: req.ExpectedHeadTick, IdempotencyKey: req.IdempotencyKey,
		})
	}
	hr, err := one(du.HumanBranchID)
	if err != nil {
		return DuelInjectResult{}, fmt.Errorf("inject into human branch: %w", err)
	}
	ar, err := one(du.AIBranchID)
	if err != nil {
		return DuelInjectResult{}, fmt.Errorf("inject into ai branch: %w", err)
	}
	return DuelInjectResult{Human: hr, AI: ar}, nil
}

// DuelOutcome is the standing of a Duel.
type DuelOutcome string

// Duel outcomes.
const (
	DuelHumanWins DuelOutcome = "human_wins"
	DuelAIWins    DuelOutcome = "ai_wins"
	DuelDraw      DuelOutcome = "draw"
)

// WeightedComponents are the weight-scaled parts of a Composite_Score in
// hundredths of a point: RoundHalfEven(weightBps * componentBps, 10000) each.
// They are rounded one by one, so their sum can differ from the Composite by
// at most one hundredth; the Composite is rounded once.
type WeightedComponents struct {
	Financial  int64
	Fulfilment int64
	OnTime     int64
}

// DuelBranchScore is one Branch's score next to its raw metrics (Req 21.5,
// 21.6). It depends only on that Branch and the Scenario (Req 21.12).
type DuelBranchScore struct {
	Branch    BranchID
	Manager   ManagerKind
	Label     string
	Metrics   sim.Metrics // raw, unaltered
	Cancelled int64       // cancelled orders at the Comparison_Tick
	Score     sim.Score   // components in bps, Composite in hundredths
	Weighted  WeightedComponents
}

// DuelScore is the score of both Branches at one Comparison_Tick.
type DuelScore struct {
	Duel           Duel
	ComparisonTick sim.Tick
	Horizon        sim.Tick
	// Provisional is true while either Branch's Head_Tick is below the
	// horizon. Outcome is then the standing at ComparisonTick, not a declared
	// winner (Req 22.2).
	Provisional bool
	Human       DuelBranchScore
	AI          DuelBranchScore
	Outcome     DuelOutcome

	Weights                sim.ScoreWeights
	ProfitFloorCents       sim.Cents
	ProfitTargetCents      sim.Cents
	TieToleranceHundredths int64
	Warnings               []Warning
}

// DuelScore computes both scores at Comparison_Tick = min(head ticks), from
// rewind reads, and decides winner or draw with sim.Compare and the
// Scenario's Tie_Tolerance (Req 21.5 to 21.9, 21.15, 22.1, 22.2).
func (d *DuelService) DuelScore(ctx context.Context, id DuelID) (DuelScore, error) {
	du, err := d.GetDuel(ctx, id)
	if err != nil {
		return DuelScore{}, err
	}
	hh, err := d.bs.HeadTick(ctx, du.HumanBranchID)
	if err != nil {
		return DuelScore{}, err
	}
	ah, err := d.bs.HeadTick(ctx, du.AIBranchID)
	if err != nil {
		return DuelScore{}, err
	}
	ct := min(hh, ah)
	sc, err := d.bs.scenarioOf(ctx, du.WorldID)
	if err != nil {
		return DuelScore{}, err
	}
	horizon := sim.Tick(sc.HorizonTicks)
	score := func(b BranchID, kind ManagerKind, label string) (DuelBranchScore, error) {
		m, err := d.bs.MetricsAt(ctx, b, ct)
		if err != nil {
			return DuelBranchScore{}, err
		}
		st, err := d.bs.StateAt(ctx, b, ct)
		if err != nil {
			return DuelBranchScore{}, err
		}
		sco := sim.ComputeScore(sc, m)
		w := sc.ScoreWeights
		return DuelBranchScore{
			Branch: b, Manager: kind, Label: label, Metrics: m, Cancelled: st.Orders.Cancelled, Score: sco,
			Weighted: WeightedComponents{
				Financial:  sim.RoundHalfEven(int64(w.FinancialWeightBps)*int64(sco.Financial), sim.BpsScale),
				Fulfilment: sim.RoundHalfEven(int64(w.FulfilmentWeightBps)*int64(sco.FulfilmentRate), sim.BpsScale),
				OnTime:     sim.RoundHalfEven(int64(w.OnTimeWeightBps)*int64(sco.OnTime), sim.BpsScale),
			},
		}, nil
	}
	hb, err := d.bs.GetBranch(ctx, du.HumanBranchID)
	if err != nil {
		return DuelScore{}, err
	}
	ab, err := d.bs.GetBranch(ctx, du.AIBranchID)
	if err != nil {
		return DuelScore{}, err
	}
	hs, err := score(hb.ID, hb.ManagerKind, hb.Label)
	if err != nil {
		return DuelScore{}, err
	}
	as, err := score(ab.ID, ab.ManagerKind, ab.Label)
	if err != nil {
		return DuelScore{}, err
	}
	var out DuelOutcome
	switch sim.Compare(hs.Score, as.Score, sc.TieToleranceHundredths) {
	case sim.ScoreWin:
		out = DuelHumanWins
	case sim.ScoreLoss:
		out = DuelAIWins
	default:
		out = DuelDraw
	}
	warn, err := d.bs.WorldEngineWarnings(ctx, du.WorldID)
	if err != nil {
		return DuelScore{}, err
	}
	return DuelScore{
		Duel: du, ComparisonTick: ct, Horizon: horizon, Provisional: hh < horizon || ah < horizon,
		Human: hs, AI: as, Outcome: out, Weights: sc.ScoreWeights,
		ProfitFloorCents: sc.ProfitFloorCents, ProfitTargetCents: sc.ProfitTargetCents,
		TieToleranceHundredths: sc.TieToleranceHundredths, Warnings: warn,
	}, nil
}

// scenarioOf returns the parsed Scenario of a World.
func (s *BranchService) scenarioOf(ctx context.Context, id WorldID) (sim.Scenario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc, ok := s.scenarios[id]; ok {
		return sc, nil
	}
	w, err := s.store.GetWorld(ctx, id)
	if err != nil {
		return sim.Scenario{}, fmt.Errorf("world %s: %w", id, err)
	}
	p, err := sim.ParseScenario(w.ScenarioJSON)
	if err != nil {
		return sim.Scenario{}, fmt.Errorf("stored scenario of %s: %w", id, err)
	}
	s.scenarios[id] = *p
	return *p, nil
}
