package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
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
	// WriteTokenHash is the SHA-256 of the write token issued to the creator
	// (nil: none, a legacy-style world).
	WriteTokenHash []byte
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
// All methods are safe for concurrent use. State changes are serialised by one
// mutex that is never held across a request to the LLM provider: the provider
// call of an AI window runs unlocked, and its answer is committed only if the
// Branch is unchanged since the call was prepared (see ai_manager.go).
type BranchService struct {
	store Store
	newID func() string
	now   func() time.Time

	engineVersion string // running engine version, recorded with each World

	mu        sync.Mutex
	scenarios map[WorldID]sim.Scenario
	heads     map[BranchID]*replayed // head state and frames 0..Head_Tick
	ended     map[BranchID]sim.Tick  // tick of the window ended explicitly
	hooks     SnapshotHooks
	proj      ProjectionRepo // optional: set when the store implements it
	ai        aiState
	llm       *LLMProvider // nil unless WithLLM is applied
	subMu     sync.Mutex   // guards subs; never held while taking mu
	subs      map[BranchID]map[chan struct{}]struct{}

	// ver is a per-Branch counter bumped on every state change (advance,
	// accepted Commands, window end, injected Disruption, recorded decision).
	// An external AI answer is committed only if it is still the one it was
	// captured under. In-memory: a restart has no in-flight requests.
	ver            map[BranchID]uint64
	inflight       map[flightKey]*flight // one provider request per window
	staleUsage     map[flightKey]staleCharge
	staleDecisions atomic.Int64 // answers discarded because the Branch changed
	advGate        map[BranchID]chan struct{}
}

// StaleDecisions counts AI answers discarded because the Branch changed while
// the provider request was in flight (a metric; nothing is recorded for them
// except the usage they cost).
func (s *BranchService) StaleDecisions() int64 { return s.staleDecisions.Load() }

// acquireAdvance serialises Advance calls of one Branch without holding s.mu,
// so a second Advance waits for the first (and for its provider request)
// without blocking any other Branch, World or read. Lock order: gate, then mu.
func (s *BranchService) acquireAdvance(ctx context.Context, id BranchID) (func(), error) {
	s.mu.Lock()
	g := s.advGate[id]
	if g == nil {
		g = make(chan struct{}, 1)
		s.advGate[id] = g
	}
	s.mu.Unlock()
	select {
	case g <- struct{}{}:
		return func() { <-g }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// NewBranchService returns a service over store. newID and now may be nil
// (random UUIDv4 and time.Now).
func NewBranchService(store Store, newID func() string, now func() time.Time, opts ...Option) *BranchService {
	if newID == nil {
		newID = newUUIDv4
	}
	if now == nil {
		now = time.Now
	}
	s := &BranchService{
		store: store, newID: newID, now: now,
		engineVersion: strconv.Itoa(sim.EngineVersion),
		scenarios:     map[WorldID]sim.Scenario{},
		heads:         map[BranchID]*replayed{},
		ended:         map[BranchID]sim.Tick{},
		ver:           map[BranchID]uint64{},
		inflight:      map[flightKey]*flight{},
		staleUsage:    map[flightKey]staleCharge{},
		advGate:       map[BranchID]chan struct{}{},
		ai: aiState{
			factories: map[ManagerKind]AIDeciderFactory{ManagerAIRuleBased: RuleBasedDecider},
			deciders:  map[deciderKey]AIDecider{},
		},
	}
	for _, o := range opts {
		o(s)
	}
	if p, ok := store.(ProjectionRepo); ok {
		s.proj = p
	}
	if d, ok := store.(DecisionRepo); ok {
		s.ai.dec = d
	} else {
		s.ai.dec = &memDecisions{m: map[BranchID][]DecisionRecord{}}
	}
	return s
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
		EngineVersion: s.engineVersion, CreatedAt: s.now(), WriteTokenHash: req.WriteTokenHash,
	}
	b := Branch{
		ID: BranchID(s.newID()), WorldID: w.ID, Label: "main", HeadTick: 0,
		ManagerKind: mgr, CreatedAt: s.now(),
	}
	s.mu.Lock()
	err = func() error {
		if err := s.store.CreateWorld(ctx, w); err != nil {
			return fmt.Errorf("create world: %w", err)
		}
		if err := s.store.CreateBranch(ctx, b); err != nil {
			return fmt.Errorf("create root branch: %w", err)
		}
		s.scenarios[w.ID] = *sc
		return nil
	}()
	s.mu.Unlock()
	if err != nil {
		return World{}, Branch{}, err
	}
	if isAIManager(mgr) {
		s.decideAtHead(ctx, b.ID) // waits for a slow provider without s.mu
	}
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
		r, err := s.rebuild(ctx, b, w, sc, b.HeadTick, false)
		if err != nil {
			return branchCtx{}, err
		}
		h = &r
		s.heads[id] = h
		s.checkProjections(ctx, id, r)
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
//
// An AI Branch whose window needs a provider request is advanced in segments:
// the Ticks computed so far are stored (the Head_Tick is the window), the lock
// is released while the request runs, and the next segment continues from the
// stored head. Advance calls of one Branch are serialised, so they never
// interleave; everything else proceeds meanwhile.
func (s *BranchService) Advance(ctx context.Context, req AdvanceRequest) (AdvanceResult, error) {
	release, err := s.acquireAdvance(ctx, req.Branch)
	if err != nil {
		return AdvanceResult{}, err
	}
	defer release()
	p := &advanceRun{}
	stale := 0
	for {
		fl, done, err := s.advanceSegment(ctx, req, p)
		if err != nil {
			return AdvanceResult{}, err
		}
		if done {
			return p.res, nil
		}
		if err := waitFlight(ctx, fl); err != nil {
			if !errors.Is(err, ErrDecisionStale) {
				return AdvanceResult{}, err
			}
			if stale++; stale > 1 {
				return AdvanceResult{}, err
			}
		}
	}
}

// advanceRun carries an Advance across its segments.
type advanceRun struct {
	res      AdvanceResult
	started  bool
	expect   sim.Tick // head the next segment must find
	evalHead bool     // the next segment first judges the head it resumes at
}

// advanceSegment computes Ticks under s.mu until the Advance is done or an AI
// window needs a provider request; then it returns the flight to wait for.
func (s *BranchService) advanceSegment(ctx context.Context, req AdvanceRequest, p *advanceRun) (*flight, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.loadMutable(ctx, req.Branch)
	if err != nil {
		return nil, false, err
	}
	head := c.head.state.Tick()
	if !p.started {
		if err := checkExpected(req.ExpectedHeadTick, head); err != nil {
			return nil, false, err
		}
	} else if head != p.expect {
		// Something else moved the Branch while the request was in flight.
		return nil, false, fmt.Errorf("expected head tick %d, actual %d: %w", p.expect, head, ErrStaleHead)
	}
	horizon := sim.Tick(c.sc.HorizonTicks)
	if head >= horizon {
		return nil, false, ErrHorizonReached
	}
	target := horizon
	runTo := req.RunToTick != nil
	if runTo {
		if *req.RunToTick <= head && !p.started {
			return nil, false, fmt.Errorf("run-to tick %d is not above head %d: %w", *req.RunToTick, head, ErrInvalidTick)
		}
		if *req.RunToTick < target {
			target = *req.RunToTick
		}
	}
	p.started = true
	ai := isAIManager(c.b.ManagerKind)
	if ai && !p.evalHead {
		// The AI never waits for a human: decide at an open window at the head.
		fl, err := s.beginDecisionLocked(ctx, c)
		if err != nil {
			return nil, false, err
		}
		if fl != nil {
			p.expect = head
			return fl, false, nil
		}
	}
	inputs, err := s.engineInputs(ctx, req.Branch, horizon)
	if err != nil {
		return nil, false, err
	}

	st := c.head.state
	var seg []sim.TickFrame // frames computed by this segment
	for {
		if !p.evalHead {
			step, err := sim.Advance(c.sc, st, inputsAt(inputs, st.Tick()+1))
			if err != nil {
				if errors.Is(err, sim.ErrHorizonReached) {
					return nil, false, ErrHorizonReached
				}
				return nil, false, fmt.Errorf("advance to tick %d: %w", st.Tick()+1, err)
			}
			st = step.State
			seg = append(seg, step.Frame)
			p.res.Frames = append(p.res.Frames, step.Frame)
			s.saveSnapshot(ctx, c.sc, c.b, st)
		}
		p.evalHead = false
		tick := st.Tick()
		open, reasons := windowAt(c.sc, c.b, st)
		if ai && open && tick < horizon {
			if s.isExternal(c) {
				// The decision needs the Branch stored at this window, so
				// the provider request can run with the lock released.
				if len(seg) > 0 {
					if err := s.persistAdvance(ctx, c, st, seg, false); err != nil {
						return nil, false, err
					}
					seg = nil
				}
				if c, err = s.loadMutable(ctx, req.Branch); err != nil {
					return nil, false, err
				}
				fl, err := s.beginDecisionLocked(ctx, c)
				if err != nil {
					return nil, false, err
				}
				if fl != nil {
					p.expect, p.evalHead = tick, true
					return fl, false, nil
				}
			} else {
				bc := branchCtx{b: c.b, w: c.w, sc: c.sc, head: &replayed{state: st, base: tick}}
				if _, err := s.beginDecisionLocked(ctx, bc); err != nil {
					return nil, false, err
				}
			}
			if inputs, err = s.engineInputs(ctx, req.Branch, horizon); err != nil {
				return nil, false, err
			}
		}
		switch {
		case tick >= horizon:
			p.res.Paused = PauseHorizon
		case runTo && open && !ai && hasReason(reasons, sim.WindowDisruptionOnset):
			p.res.Paused, p.res.WindowReasons = PauseWindow, reasons
		case runTo && tick >= target:
			p.res.Paused = PauseRunTo
			if open {
				p.res.WindowReasons = reasons
			}
		case !runTo && open:
			p.res.Paused, p.res.WindowReasons = PauseWindow, reasons
		}
		if p.res.Paused != "" {
			break
		}
	}
	if err := s.persistAdvance(ctx, c, st, seg, ai); err != nil {
		return nil, false, err
	}
	p.res.HeadTick = st.Tick()
	p.res.Hash = sim.Hash(st)
	return nil, true, nil
}

// isExternal reports whether the AI decision of c's Branch needs a slow
// provider request. Caller holds s.mu.
func (s *BranchService) isExternal(c branchCtx) bool {
	d, fellBack := s.decider(c)
	_, ok := d.(externalDecider)
	return ok && !fellBack
}

// persistAdvance stores the head reached by an Advance segment: the head tick,
// the projections and the cached frames. decided says an AI window at st was
// already decided, so the window is closed to Commands. Caller holds s.mu.
func (s *BranchService) persistAdvance(ctx context.Context, c branchCtx, st sim.State, seg []sim.TickFrame, decided bool) error {
	id := c.b.ID
	if err := s.store.SetHeadTick(ctx, id, st.Tick()); err != nil {
		return fmt.Errorf("set head tick: %w", err)
	}
	s.writeProjections(ctx, id, seg)
	frames := append(append([]sim.TickFrame(nil), c.head.frames...), seg...)
	s.heads[id] = &replayed{state: st, base: c.head.base, frames: frames}
	delete(s.ended, id)
	if decided && st.Tick() < sim.Tick(c.sc.HorizonTicks) {
		if open, _ := windowAt(c.sc, c.b, st); open {
			s.ended[id] = st.Tick() // decided on arrival
		}
	}
	s.bump(id)
	s.notifyHead(id)
	return nil
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
	c, err := s.loadMutable(ctx, req.Branch)
	if err != nil {
		return SubmitResult{}, err
	}
	return s.submitLocked(ctx, c, req)
}

// submitLocked is the one gate every Command passes, Human or AI: window,
// budget and idempotency checks, then sim.Validate against the Observation at
// the head. Caller holds s.mu; c is the Branch context at its head.
func (s *BranchService) submitLocked(ctx context.Context, c branchCtx, req SubmitRequest) (SubmitResult, error) {
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
		s.bump(req.Branch)
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
	c, err := s.loadMutable(ctx, id)
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
	s.bump(id)
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
	if f, ok := c.head.frame(t); ok {
		return f, nil
	}
	r, err := s.rebuild(ctx, c.b, c.w, c.sc, t, true)
	if err != nil {
		return sim.TickFrame{}, err
	}
	f, ok := r.frame(t)
	if !ok {
		return sim.TickFrame{}, fmt.Errorf("frame %d not recomputed", t)
	}
	return f, nil
}

// FramesRange returns the Tick_Frames of ticks from..to inclusive in order.
// Frames the head cache holds are served from it; otherwise the range is
// recomputed with ONE replay from the best trusted snapshot at or before
// from-1 (else from Init), instead of one replay per tick. Frames are the
// engine's own output, so every frame equals the one FrameAt returns.
func (s *BranchService) FramesRange(ctx context.Context, id BranchID, from, to sim.Tick) ([]sim.TickFrame, error) {
	if from < 0 || to < from {
		return nil, fmt.Errorf("range %d..%d: %w", from, to, ErrInvalidTick)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.checkTick(to); err != nil {
		return nil, err
	}
	out := make([]sim.TickFrame, 0, int64(to-from)+1)
	cached := true
	for t := from; t <= to; t++ {
		f, ok := c.head.frame(t)
		if !ok {
			cached = false
			break
		}
		out = append(out, f)
	}
	if cached {
		return out, nil
	}
	out = out[:0]
	r, err := s.rebuildFrom(ctx, c.b, c.w, c.sc, from, to)
	if err != nil {
		return nil, err
	}
	for t := from; t <= to; t++ {
		f, ok := r.frame(t)
		if !ok {
			return nil, fmt.Errorf("frame %d not recomputed", t)
		}
		out = append(out, f)
	}
	return out, nil
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
	r, err := s.rebuild(ctx, c.b, c.w, c.sc, t, false)
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
	obs := observeWindow(c.sc, c.b, st)
	return obs, nil
}
