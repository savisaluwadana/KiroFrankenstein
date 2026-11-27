package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// MaxNameRunes is the longest allowed World or Branch name.
const MaxNameRunes = 64

// PauseReason says why Advance stopped.
type PauseReason string

// Pause reasons.
const (
	PauseWindow  PauseReason = "decision_window"
	PauseHorizon PauseReason = "horizon"
	PauseRunTo   PauseReason = "run_to_tick"
)

// CreateWorldRequest creates a World and its root Branch.
type CreateWorldRequest struct {
	Name         string
	ScenarioJSON []byte
	Seed         sim.Seed
	Overrides    map[string]int64 // Assumption path -> value
	Manager      ManagerKind      // default ManagerHuman
}

// AdvanceRequest advances a Branch. RunToTick nil means "to the next
// Decision_Window or the horizon". ExpectedHeadTick, when set, must equal the
// Branch's Head_Tick or the call fails with ErrStaleHead.
type AdvanceRequest struct {
	Branch           BranchID
	RunToTick        *sim.Tick
	ExpectedHeadTick *sim.Tick
}

// AdvanceResult reports where an Advance stopped. Frames are the Tick_Frames
// of the newly computed Ticks.
type AdvanceResult struct {
	HeadTick      sim.Tick
	Hash          [32]byte
	Paused        PauseReason
	WindowReasons []sim.WindowReason
	Frames        []sim.TickFrame
}

// SubmitRequest submits Commands in the Decision_Window at WindowTick (which
// must be the Head_Tick). Accepted Commands become Inputs with EffectiveTick
// WindowTick+1. IdempotencyKey, when set, makes a repeat call a no-op that
// returns the original result.
type SubmitRequest struct {
	Branch           BranchID
	WindowTick       sim.Tick
	Commands         []sim.Command
	IdempotencyKey   string
	Metadata         Metadata // Provenance defaults to ProvHuman
	ExpectedHeadTick *sim.Tick
}

// SubmitResult lists accepted Inputs and rejected Commands with reason codes.
type SubmitResult struct {
	Accepted        []InputRecord
	Rejected        []sim.ValidationResult
	BudgetRemaining int64
	Duplicate       bool
}

// BranchService is the application service for Worlds, advancing and
// rewind reads. It is the only code that turns stored Inputs into sim.Inputs.
// All methods are safe for concurrent use; they are serialised by one mutex.
type BranchService struct {
	store Store
	newID func() string
	now   func() time.Time

	mu        sync.Mutex
	scenarios map[WorldID]sim.Scenario
	heads     map[BranchID]*replayed // head state and frames 0..Head_Tick
	ended     map[BranchID]sim.Tick  // tick of the window ended explicitly
}

// NewBranchService returns a service over store. newID and now may be nil
// (random UUIDv4 and time.Now).
func NewBranchService(store Store, newID func() string, now func() time.Time) *BranchService {
	if newID == nil {
		newID = newUUIDv4
	}
	if now == nil {
		now = time.Now
	}
	return &BranchService{
		store: store, newID: newID, now: now,
		scenarios: map[WorldID]sim.Scenario{},
		heads:     map[BranchID]*replayed{},
		ended:     map[BranchID]sim.Tick{},
	}
}

func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("app: random source failed: %v", err))
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func validName(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > MaxNameRunes {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// CreateWorld validates and canonicalizes the Scenario (with overrides),
// stores the World and a root Branch at Head_Tick 0, whose Decision_Window is
// open at Tick 0.
func (s *BranchService) CreateWorld(ctx context.Context, req CreateWorldRequest) (World, Branch, error) {
	if !validName(req.Name) {
		return World{}, Branch{}, fmt.Errorf("world name: %w", ErrInvalidName)
	}
	mgr := req.Manager
	if mgr == "" {
		mgr = ManagerHuman
	}
	parsed, err := sim.ParseScenario(req.ScenarioJSON)
	if err != nil {
		return World{}, Branch{}, fmt.Errorf("%w: %w", ErrInvalidScenario, err)
	}
	sc, err := sim.ApplyOverridesAndValidate(parsed, req.Overrides)
	if err != nil {
		return World{}, Branch{}, fmt.Errorf("%w: %w", ErrInvalidScenario, err)
	}
	canon := sim.PrintScenario(sc)
	w := World{
		ID: WorldID(s.newID()), Name: req.Name, ScenarioHash: sha256.Sum256(canon),
		ScenarioJSON: canon, Seed: req.Seed,
		EngineVersion: strconv.Itoa(sim.EngineVersion), CreatedAt: s.now(),
	}
	b := Branch{
		ID: BranchID(s.newID()), WorldID: w.ID, Label: "main", HeadTick: 0,
		ManagerKind: mgr, CreatedAt: s.now(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.CreateWorld(ctx, w); err != nil {
		return World{}, Branch{}, fmt.Errorf("create world: %w", err)
	}
	if err := s.store.CreateBranch(ctx, b); err != nil {
		return World{}, Branch{}, fmt.Errorf("create root branch: %w", err)
	}
	s.scenarios[w.ID] = *sc
	return w, b, nil
}

// branchCtx is the resolved context of one call.
type branchCtx struct {
	b    Branch
	w    World
	sc   sim.Scenario
	head *replayed
}

// load resolves a Branch, its Scenario and its head state. On a cache miss it
// rebuilds the head by replaying the stored inputs (the Input_Log is the
// truth, the cache is derived). Caller holds s.mu.
func (s *BranchService) load(ctx context.Context, id BranchID) (branchCtx, error) {
	b, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return branchCtx{}, fmt.Errorf("branch %s: %w", id, err)
	}
	w, err := s.store.GetWorld(ctx, b.WorldID)
	if err != nil {
		return branchCtx{}, fmt.Errorf("world %s: %w", b.WorldID, err)
	}
	sc, ok := s.scenarios[w.ID]
	if !ok {
		p, err := sim.ParseScenario(w.ScenarioJSON)
		if err != nil {
			return branchCtx{}, fmt.Errorf("stored scenario of %s: %w", w.ID, err)
		}
		sc = *p
		s.scenarios[w.ID] = sc
	}
	h := s.heads[id]
	if h == nil || h.state.Tick() != b.HeadTick {
		inputs, err := s.engineInputs(ctx, id, b.HeadTick)
		if err != nil {
			return branchCtx{}, err
		}
		r, err := replay(sc, w.Seed, inputs, b.HeadTick)
		if err != nil {
			return branchCtx{}, err
		}
		h = &r
		s.heads[id] = h
	}
	return branchCtx{b: b, w: w, sc: sc, head: h}, nil
}

// engineInputs resolves the branch's input prefix (ancestor walk) up to tick
// upTo and renumbers it.
func (s *BranchService) engineInputs(ctx context.Context, id BranchID, upTo sim.Tick) ([]sim.Input, error) {
	recs, err := s.store.InputsForBranch(ctx, id, upTo)
	if err != nil {
		return nil, fmt.Errorf("inputs of branch %s: %w", id, err)
	}
	return buildEngineInputs(recs)
}

// windowAt reports whether a Decision_Window is open at tick and why: the
// Scenario's windows, the onsets of injected Disruptions (read from the state
// at tick, which lists an injected Disruption in its onset Tick) and the fork
// window of a child Branch.
func windowAt(sc sim.Scenario, b Branch, st sim.State) (bool, []sim.WindowReason) {
	tick := st.Tick()
	open, reasons := sim.DecisionWindowOpenState(sc, st)
	if b.ParentBranchID != "" && b.ForkTick == tick && int64(tick) < sc.HorizonTicks {
		open, reasons = true, append(reasons, sim.WindowFork)
	}
	return open, reasons
}

func checkExpected(exp *sim.Tick, head sim.Tick) error {
	if exp != nil && *exp != head {
		return fmt.Errorf("expected head tick %d, actual %d: %w", *exp, head, ErrStaleHead)
	}
	return nil
}

func hasReason(rs []sim.WindowReason, r sim.WindowReason) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// Advance computes Ticks from the Head_Tick. Without RunToTick it stops at the
// next Decision_Window or the horizon. With RunToTick it ends intervening
// windows with no Commands and stops at RunToTick, the horizon, or the next
// Disruption-onset window, whichever comes first. Advancing from an open
// window ends that window.
func (s *BranchService) Advance(ctx context.Context, req AdvanceRequest) (AdvanceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, req.Branch)
	if err != nil {
		return AdvanceResult{}, err
	}
	head := c.head.state.Tick()
	if err := checkExpected(req.ExpectedHeadTick, head); err != nil {
		return AdvanceResult{}, err
	}
	horizon := sim.Tick(c.sc.HorizonTicks)
	if head >= horizon {
		return AdvanceResult{}, ErrHorizonReached
	}
	target := horizon
	runTo := req.RunToTick != nil
	if runTo {
		if *req.RunToTick <= head {
			return AdvanceResult{}, fmt.Errorf("run-to tick %d is not above head %d: %w", *req.RunToTick, head, ErrInvalidTick)
		}
		if *req.RunToTick < target {
			target = *req.RunToTick
		}
	}
	inputs, err := s.engineInputs(ctx, req.Branch, horizon)
	if err != nil {
		return AdvanceResult{}, err
	}

	st := c.head.state
	res := AdvanceResult{}
	for {
		step, err := sim.Advance(c.sc, st, inputsAt(inputs, st.Tick()+1))
		if err != nil {
			if errors.Is(err, sim.ErrHorizonReached) {
				return AdvanceResult{}, ErrHorizonReached
			}
			return AdvanceResult{}, fmt.Errorf("advance to tick %d: %w", st.Tick()+1, err)
		}
		st = step.State
		res.Frames = append(res.Frames, step.Frame)
		tick := st.Tick()
		open, reasons := windowAt(c.sc, c.b, st)
		switch {
		case tick >= horizon:
			res.Paused = PauseHorizon
		case runTo && open && hasReason(reasons, sim.WindowDisruptionOnset):
			res.Paused, res.WindowReasons = PauseWindow, reasons
		case runTo && tick >= target:
			res.Paused = PauseRunTo
			if open {
				res.WindowReasons = reasons
			}
		case !runTo && open:
			res.Paused, res.WindowReasons = PauseWindow, reasons
		}
		if res.Paused != "" {
			break
		}
	}

	if err := s.store.SetHeadTick(ctx, req.Branch, st.Tick()); err != nil {
		return AdvanceResult{}, fmt.Errorf("set head tick: %w", err)
	}
	frames := append(append([]sim.TickFrame(nil), c.head.frames...), res.Frames...)
	s.heads[req.Branch] = &replayed{state: st, frames: frames}
	delete(s.ended, req.Branch)
	res.HeadTick = st.Tick()
	res.Hash = sim.Hash(st)
	return res, nil
}

// budgetUsed counts command inputs already stored for the window at tick
// (effective tick+1) on this branch's prefix.
func (s *BranchService) budgetUsed(ctx context.Context, id BranchID, window sim.Tick) (int64, error) {
	recs, err := s.store.InputsForBranch(ctx, id, window+1)
	if err != nil {
		return 0, fmt.Errorf("inputs of branch %s: %w", id, err)
	}
	var n int64
	for _, r := range recs {
		if r.EffectiveTick == window+1 && r.Kind == sim.InputCommand {
			n++
		}
	}
	return n, nil
}

// SubmitCommands validates Commands in order with sim.Validate against the
// Observation at the head and stores the accepted ones. Rejected Commands are
// returned with reason codes and stored nowhere.
func (s *BranchService) SubmitCommands(ctx context.Context, req SubmitRequest) (SubmitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, req.Branch)
	if err != nil {
		return SubmitResult{}, err
	}
	head := c.head.state.Tick()
	if err := checkExpected(req.ExpectedHeadTick, head); err != nil {
		return SubmitResult{}, err
	}
	if req.WindowTick > head {
		return SubmitResult{}, fmt.Errorf("window tick %d, head %d: %w", req.WindowTick, head, ErrNotYetSimulated)
	}
	if req.WindowTick+1 <= head { // effective tick already computed
		return SubmitResult{}, fmt.Errorf("effective tick %d <= head %d: %w", req.WindowTick+1, head, ErrHistoryImmutable)
	}
	if req.IdempotencyKey != "" {
		if _, ok, err := s.store.FindByIdempotencyKey(ctx, req.Branch, idemKey(req.IdempotencyKey, 0)); err != nil {
			return SubmitResult{}, fmt.Errorf("idempotency lookup: %w", err)
		} else if ok {
			return s.previousSubmission(ctx, req, c)
		}
	}
	open, _ := windowAt(c.sc, c.b, c.head.state)
	if !open || s.endedAt(req.Branch, head) {
		return SubmitResult{}, fmt.Errorf("tick %d: %w", head, ErrWindowNotOpen)
	}
	used, err := s.budgetUsed(ctx, req.Branch, head)
	if err != nil {
		return SubmitResult{}, err
	}
	remaining := c.sc.CommandBudget - used
	if remaining <= 0 && len(req.Commands) > 0 {
		return SubmitResult{}, fmt.Errorf("budget %d used %d: %w", c.sc.CommandBudget, used, ErrBudgetExceeded)
	}

	obs := sim.Observe(c.sc, c.head.state)
	results := sim.Validate(c.sc, obs, req.Commands, remaining)
	out := SubmitResult{BudgetRemaining: remaining}
	md := req.Metadata
	if md.Provenance == "" {
		md.Provenance = ProvHuman
	}
	n := 0
	for _, r := range results {
		if !r.OK {
			out.Rejected = append(out.Rejected, r)
			continue
		}
		in := md
		if req.IdempotencyKey != "" {
			in.IdempotencyKey = idemKey(req.IdempotencyKey, n)
		}
		rec, err := s.store.AppendInput(ctx, req.Branch, NewCommandInput(head+1, r.Command, in))
		if err != nil {
			return SubmitResult{}, fmt.Errorf("append input: %w", err)
		}
		out.Accepted = append(out.Accepted, rec)
		out.BudgetRemaining--
		n++
	}
	return out, nil
}

func idemKey(key string, i int) string { return key + "#" + strconv.Itoa(i) }

func (s *BranchService) endedAt(id BranchID, head sim.Tick) bool {
	t, ok := s.ended[id]
	return ok && t == head
}

// previousSubmission rebuilds the original result of a repeated idempotency
// key from the stored inputs; nothing is appended.
func (s *BranchService) previousSubmission(ctx context.Context, req SubmitRequest, c branchCtx) (SubmitResult, error) {
	out := SubmitResult{Duplicate: true}
	used, err := s.budgetUsed(ctx, req.Branch, c.head.state.Tick())
	if err != nil {
		return SubmitResult{}, err
	}
	out.BudgetRemaining = max(c.sc.CommandBudget-used, 0)
	for i := 0; ; i++ {
		rec, ok, err := s.store.FindByIdempotencyKey(ctx, req.Branch, idemKey(req.IdempotencyKey, i))
		if err != nil {
			return SubmitResult{}, fmt.Errorf("idempotency lookup: %w", err)
		}
		if !ok {
			break
		}
		out.Accepted = append(out.Accepted, rec)
	}
	return out, nil
}

// EndDecisionWindow closes the open window at the head Tick without further
// Commands. Commands already submitted stay recorded.
func (s *BranchService) EndDecisionWindow(ctx context.Context, id BranchID, windowTick sim.Tick, expected *sim.Tick) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	head := c.head.state.Tick()
	if err := checkExpected(expected, head); err != nil {
		return err
	}
	if windowTick > head {
		return fmt.Errorf("window tick %d, head %d: %w", windowTick, head, ErrNotYetSimulated)
	}
	if windowTick < head {
		return fmt.Errorf("window tick %d < head %d: %w", windowTick, head, ErrHistoryImmutable)
	}
	if open, _ := windowAt(c.sc, c.b, c.head.state); !open || s.endedAt(id, head) {
		return fmt.Errorf("tick %d: %w", head, ErrWindowNotOpen)
	}
	s.ended[id] = head
	return nil
}

// ---- Rewind reads: pure, never change Head_Tick or the Input_Log ----

// HeadTick returns the Branch's Head_Tick.
func (s *BranchService) HeadTick(ctx context.Context, id BranchID) (sim.Tick, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return 0, err
	}
	return c.head.state.Tick(), nil
}

func (c branchCtx) checkTick(t sim.Tick) error {
	if t < 0 {
		return fmt.Errorf("tick %d: %w", t, ErrInvalidTick)
	}
	if head := c.head.state.Tick(); t > head {
		return fmt.Errorf("tick %d > head %d: %w", t, head, ErrNotYetSimulated)
	}
	return nil
}

// FrameAt returns the Tick_Frame of tick (cached for every tick <= head).
func (s *BranchService) FrameAt(ctx context.Context, id BranchID, t sim.Tick) (sim.TickFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return sim.TickFrame{}, err
	}
	if err := c.checkTick(t); err != nil {
		return sim.TickFrame{}, err
	}
	return c.head.frames[t], nil
}

// HashAt returns the State_Hash at tick.
func (s *BranchService) HashAt(ctx context.Context, id BranchID, t sim.Tick) ([32]byte, error) {
	f, err := s.FrameAt(ctx, id, t)
	return f.Hash, err
}

// MetricsAt returns the Metrics_Calculator output at tick.
func (s *BranchService) MetricsAt(ctx context.Context, id BranchID, t sim.Tick) (sim.Metrics, error) {
	f, err := s.FrameAt(ctx, id, t)
	return f.Metrics, err
}

// StateAt returns the full State at tick. The head comes from the cache; an
// earlier tick is rebuilt by replay from Init over the branch's input prefix.
func (s *BranchService) StateAt(ctx context.Context, id BranchID, t sim.Tick) (sim.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return sim.State{}, err
	}
	return s.stateAt(ctx, c, t)
}

func (s *BranchService) stateAt(ctx context.Context, c branchCtx, t sim.Tick) (sim.State, error) {
	if err := c.checkTick(t); err != nil {
		return sim.State{}, err
	}
	if t == c.head.state.Tick() {
		return c.head.state, nil
	}
	inputs, err := s.engineInputs(ctx, c.b.ID, t)
	if err != nil {
		return sim.State{}, err
	}
	r, err := replay(c.sc, c.w.Seed, inputs, t)
	if err != nil {
		return sim.State{}, err
	}
	return r.state, nil
}

// ObservationAt returns the Observation at tick, with the fork window added
// when tick is the fork Tick of a child Branch.
func (s *BranchService) ObservationAt(ctx context.Context, id BranchID, t sim.Tick) (sim.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return sim.Observation{}, err
	}
	st, err := s.stateAt(ctx, c, t)
	if err != nil {
		return sim.Observation{}, err
	}
	obs := sim.Observe(c.sc, st)
	open, reasons := windowAt(c.sc, c.b, st)
	obs.Window.Open, obs.Window.Reasons = open, reasons
	return obs, nil
}
