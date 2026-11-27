// Package ensemble runs a Scenario under many Seeds for one or two Arms
// (Req 24, 25). It uses the sim package and the Inputs_File format directly
// and holds everything in memory: no database, no network, no LLM.
//
// Arms:
//   - Fixed_Replay: the Commands of an Inputs_File are re-applied at their
//     recorded effective Ticks under each Seed. Each Command is re-validated
//     against the Observation of that Seed's run at the preceding
//     Decision_Window. A Command that fails is skipped with a reason code and
//     never altered or replaced.
//   - Adaptive_Evaluation: the Rule_Based_Manager decides at every
//     Decision_Window at or after the Arm's start Tick; its proposals pass the
//     same validator and Command_Budget. An optional prefix Inputs_File is
//     applied as a Fixed_Replay for effective Ticks up to the start Tick.
//
// Every Arm runs under the same Seed list, so the exogenous draws are common
// to both Arms by construction (Req 25.1). Results are assembled by Arm and
// ascending Seed whatever the execution order.
package ensemble

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/rulebased"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

// MaxSeeds is the largest Seed list of one invocation (Req 24.2).
const MaxSeeds = 50

// Errors returned before any run is simulated.
var (
	ErrNoSeeds        = errors.New("ensemble: the seed list is empty")
	ErrTooManySeeds   = errors.New("ensemble: more than 50 seeds")
	ErrDuplicateSeeds = errors.New("ensemble: duplicate seeds")
	ErrUnsupportedArm = errors.New("ensemble: unsupported arm")
	ErrInvalidArm     = errors.New("ensemble: invalid arm")
	ErrNoArms         = errors.New("ensemble: at least one arm is required")
	ErrTooManyArms    = errors.New("ensemble: at most two arms are supported")
	ErrMismatch       = errors.New("ensemble: inputs file mismatch")
)

// Reason codes of Skipped_Commands that are not validator codes.
const (
	// ReasonNoDecisionWindow: no Decision_Window is open at the Tick preceding
	// the Command's effective Tick under this Seed.
	ReasonNoDecisionWindow = "no_decision_window"
)

// Kind is the kind of an Arm.
type Kind string

// Arm kinds.
const (
	KindFixedReplay        Kind = "fixed"
	KindAdaptiveEvaluation Kind = "adaptive"
	// KindLLM is recognised only to be rejected: ensembles never call an LLM.
	KindLLM Kind = "llm"
)

// Arm labels used in reports (Req 25.2).
const (
	LabelFixed    = "fixed plan (non-adaptive)"
	LabelAdaptive = "adaptive policy: rule-based"
)

// Arm is one evaluated strategy.
type Arm struct {
	Kind Kind
	// Name is a short user label, for example the Inputs_File path.
	Name string
	// File is the Fixed_Replay plan, or the optional prefix of an adaptive Arm.
	File *inputsfile.File
	// StartTick is the Arm's start Tick. The policy decides at Decision_Windows
	// whose Tick is >= StartTick; the prefix holds Inputs with effective Tick
	// <= StartTick. Unused by a Fixed_Replay.
	StartTick int64
}

// Label is the report label of the Arm with its start Tick (Req 25.2).
func (a Arm) Label() string {
	switch a.Kind {
	case KindFixedReplay:
		return LabelFixed
	case KindAdaptiveEvaluation:
		return fmt.Sprintf("%s (start tick %d)", LabelAdaptive, a.StartTick)
	}
	return string(a.Kind)
}

// Config is one ensemble invocation.
type Config struct {
	Scenario     *sim.Scenario
	ScenarioHash string
	Seeds        []sim.Seed
	Arms         []Arm // one or two
	// Concurrency is the size of the worker pool. 0 means GOMAXPROCS.
	Concurrency int

	// onInit, if not nil, is called once per run just before sim.Init. Tests
	// use it to prove that rejected invocations simulate nothing.
	onInit func()
}

// Skipped records a Fixed_Replay Command that failed validation (Req 24.6).
// Parameters are never altered: the Command is dropped.
type Skipped struct {
	Tick    int64  // effective Tick of the Command
	Kind    string // Command kind wire name
	Reason  string // sim.RejectCode value or ReasonNoDecisionWindow
	Message string
}

// RunResult is one Arm simulated under one Seed to the horizon.
type RunResult struct {
	Seed     sim.Seed
	ArmIndex int
	Arm      string // Arm label
	Hash     [32]byte
	Metrics  sim.Metrics
	Score    sim.Score
	// Skipped lists the skipped Fixed_Replay Commands (and any policy proposal
	// the validator rejected), in effective Tick order.
	Skipped []Skipped
	// Decisions is the number of Commands the policy proposed and the
	// validator accepted. It is 0 for a Fixed_Replay.
	Decisions int
	// Cancelled is the number of cancelled orders at the horizon.
	Cancelled int64
}

// Result is the outcome of an ensemble. Runs are ordered by Arm index, then
// ascending Seed. The canonical report is built from it by task 7.10.
type Result struct {
	ScenarioHash  string
	EngineVersion int
	HorizonTicks  int64
	Seeds         []sim.Seed // ascending
	Arms          []Arm
	Runs          []RunResult
}

// ParseSeeds parses "1..20" (inclusive range) or "1,2,3" (explicit list). The
// list is returned in the given order; Run validates count and duplicates.
func ParseSeeds(spec string) ([]sim.Seed, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, ErrNoSeeds
	}
	if lo, hi, ok := strings.Cut(spec, ".."); ok {
		a, err1 := strconv.ParseUint(strings.TrimSpace(lo), 10, 64)
		b, err2 := strconv.ParseUint(strings.TrimSpace(hi), 10, 64)
		if err1 != nil || err2 != nil || b < a {
			return nil, fmt.Errorf("ensemble: invalid seed range %q", spec)
		}
		if b-a+1 > MaxSeeds {
			return nil, fmt.Errorf("%w: range %q", ErrTooManySeeds, spec)
		}
		return SeedRange(sim.Seed(a), int(b-a+1))
	}
	var out []sim.Seed
	for _, p := range strings.Split(spec, ",") {
		v, err := strconv.ParseUint(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ensemble: invalid seed %q", p)
		}
		out = append(out, sim.Seed(v))
	}
	return out, nil
}

// SeedRange returns count consecutive Seeds from base.
func SeedRange(base sim.Seed, count int) ([]sim.Seed, error) {
	if count < 1 {
		return nil, ErrNoSeeds
	}
	if count > MaxSeeds {
		return nil, fmt.Errorf("%w: count %d", ErrTooManySeeds, count)
	}
	out := make([]sim.Seed, count)
	for i := range out {
		out[i] = base + sim.Seed(i)
	}
	return out, nil
}

// ArmSpec is a parsed --arm value; the caller loads the files.
type ArmSpec struct {
	Kind      Kind
	Path      string // fixed: required; adaptive: the prefix file; rulebased: ""
	StartTick int64
	// StartSet reports whether an explicit @startTick was given.
	StartSet bool
}

// ParseArmSpec parses "fixed:<file>", "rulebased[@start]" or
// "adaptive:<file>[@start]". Any name starting with "llm" is rejected with
// ErrUnsupportedArm.
func ParseArmSpec(spec string) (ArmSpec, error) {
	spec = strings.TrimSpace(spec)
	if spec == "llm" || strings.HasPrefix(spec, "llm:") || strings.HasPrefix(spec, "llm@") {
		return ArmSpec{}, fmt.Errorf("%w: %q (ensembles never call an LLM)", ErrUnsupportedArm, spec)
	}
	var as ArmSpec
	body := spec
	if i := strings.LastIndex(body, "@"); i >= 0 {
		n, err := strconv.ParseInt(body[i+1:], 10, 64)
		if err != nil || n < 0 {
			return as, fmt.Errorf("%w: bad start tick in %q", ErrInvalidArm, spec)
		}
		as.StartTick, as.StartSet = n, true
		body = body[:i]
	}
	switch {
	case body == "rulebased":
		as.Kind = KindAdaptiveEvaluation
	case strings.HasPrefix(body, "fixed:") && len(body) > len("fixed:"):
		if as.StartSet {
			return as, fmt.Errorf("%w: a fixed arm has no start tick: %q", ErrInvalidArm, spec)
		}
		as.Kind, as.Path = KindFixedReplay, body[len("fixed:"):]
	case strings.HasPrefix(body, "adaptive:") && len(body) > len("adaptive:"):
		as.Kind, as.Path = KindAdaptiveEvaluation, body[len("adaptive:"):]
	default:
		return as, fmt.Errorf("%w: %q (want fixed:<file>, rulebased[@tick] or adaptive:<file>[@tick])", ErrInvalidArm, spec)
	}
	return as, nil
}

// DefaultStart returns the start Tick of an adaptive Arm with a prefix and no
// explicit @start: the last effective Tick of the prefix, so the policy takes
// over where the recorded plan ends.
func DefaultStart(f *inputsfile.File) int64 {
	var m int64
	if f != nil {
		for _, in := range f.Inputs {
			if int64(in.EffectiveTick) > m {
				m = int64(in.EffectiveTick)
			}
		}
	}
	return m
}

// Run validates cfg (nothing is simulated if it is rejected), then simulates
// every Arm under every Seed.
func Run(ctx context.Context, cfg Config) (Result, error) {
	seeds, err := checkConfig(cfg)
	if err != nil {
		return Result{}, err
	}
	sc := cfg.Scenario
	res := Result{ScenarioHash: cfg.ScenarioHash, EngineVersion: sim.EngineVersion, HorizonTicks: sc.HorizonTicks, Seeds: seeds, Arms: cfg.Arms}

	type job struct{ arm, seed int }
	jobs := make([]job, 0, len(cfg.Arms)*len(seeds))
	for a := range cfg.Arms {
		for s := range seeds {
			jobs = append(jobs, job{a, s})
		}
	}
	out := make([]RunResult, len(jobs))
	errs := make([]error, len(jobs))

	workers := cfg.Concurrency
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > len(jobs) {
		workers = len(jobs)
	}
	ch := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				if ctx.Err() != nil {
					errs[i] = ctx.Err()
					continue
				}
				j := jobs[i]
				out[i], errs[i] = runOne(cfg, j.arm, seeds[j.seed])
			}
		}()
	}
	for i := range jobs {
		ch <- i
	}
	close(ch)
	wg.Wait()
	for i, e := range errs { // first error in job order: deterministic
		if e != nil {
			j := jobs[i]
			return Result{}, fmt.Errorf("arm %d seed %d: %w", j.arm, uint64(seeds[j.seed]), e)
		}
	}
	res.Runs = out
	return res, nil
}

// checkConfig rejects an invalid invocation before any simulation and returns
// the Seeds sorted ascending.
func checkConfig(cfg Config) ([]sim.Seed, error) {
	if cfg.Scenario == nil {
		return nil, errors.New("ensemble: scenario is required")
	}
	if len(cfg.Seeds) == 0 {
		return nil, ErrNoSeeds
	}
	if len(cfg.Seeds) > MaxSeeds {
		return nil, fmt.Errorf("%w: %d given", ErrTooManySeeds, len(cfg.Seeds))
	}
	seeds := append([]sim.Seed(nil), cfg.Seeds...)
	sort.Slice(seeds, func(i, j int) bool { return seeds[i] < seeds[j] })
	for i := 1; i < len(seeds); i++ {
		if seeds[i] == seeds[i-1] {
			return nil, fmt.Errorf("%w: seed %d", ErrDuplicateSeeds, uint64(seeds[i]))
		}
	}
	if len(cfg.Arms) == 0 {
		return nil, ErrNoArms
	}
	if len(cfg.Arms) > 2 {
		return nil, ErrTooManyArms
	}
	for i, a := range cfg.Arms {
		switch a.Kind {
		case KindFixedReplay:
			if a.File == nil {
				return nil, fmt.Errorf("%w: arm %d has no inputs file", ErrInvalidArm, i)
			}
		case KindAdaptiveEvaluation:
			if a.StartTick < 0 || a.StartTick >= cfg.Scenario.HorizonTicks {
				return nil, fmt.Errorf("%w: arm %d start tick %d is outside 0..%d", ErrInvalidArm, i, a.StartTick, cfg.Scenario.HorizonTicks-1)
			}
		default:
			return nil, fmt.Errorf("%w: arm %d kind %q", ErrUnsupportedArm, i, a.Kind)
		}
		if a.File != nil {
			if want := strconv.Itoa(sim.EngineVersion); a.File.EngineVersion != want {
				return nil, fmt.Errorf("%w: engineVersion: inputs file has %q, this binary is %q", ErrMismatch, a.File.EngineVersion, want)
			}
			if a.File.ScenarioHash != cfg.ScenarioHash {
				return nil, fmt.Errorf("%w: scenarioHash: inputs file has %s, scenario hashes to %s", ErrMismatch, a.File.ScenarioHash, cfg.ScenarioHash)
			}
		}
	}
	return seeds, nil
}

// runOne simulates one Arm under one Seed.
func runOne(cfg Config, armIdx int, seed sim.Seed) (RunResult, error) {
	sc := *cfg.Scenario
	arm := cfg.Arms[armIdx]
	if cfg.onInit != nil {
		cfg.onInit()
	}
	st, err := sim.Init(sc, seed)
	if err != nil {
		return RunResult{}, err
	}

	// File inputs in file order, filtered by Arm kind.
	var fixed []sim.Input
	if arm.File != nil {
		all, err := inputsfile.ToInputs(*arm.File)
		if err != nil {
			return RunResult{}, err
		}
		for _, in := range all {
			if int64(in.EffectiveTick) > sc.HorizonTicks {
				return RunResult{}, fmt.Errorf("input at effective tick %d is after the horizon %d", int64(in.EffectiveTick), sc.HorizonTicks)
			}
			if int64(in.EffectiveTick) < 1 {
				return RunResult{}, fmt.Errorf("input at effective tick %d is not after tick 0", int64(in.EffectiveTick))
			}
			if arm.Kind == KindAdaptiveEvaluation && int64(in.EffectiveTick) > arm.StartTick {
				continue // the policy owns everything after the start Tick
			}
			fixed = append(fixed, in)
		}
	}
	var mgr *rulebased.Manager
	if arm.Kind == KindAdaptiveEvaluation {
		mgr = rulebased.New(sc)
	}

	rr := RunResult{Seed: seed, ArmIndex: armIdx, Arm: arm.Label()}
	next := 0
	var seq uint64
	for int64(st.Tick()) < sc.HorizonTicks {
		eff := st.Tick() + 1
		var recorded []sim.Input
		for next < len(fixed) && fixed[next].EffectiveTick == eff {
			recorded = append(recorded, fixed[next])
			next++
		}
		if next < len(fixed) && fixed[next].EffectiveTick < eff {
			return RunResult{}, errors.New("inputs are not ordered by effective tick")
		}

		open, _ := sim.DecisionWindowOpenState(sc, st)
		var obs sim.Observation
		haveObs := false
		observe := func() sim.Observation {
			if !haveObs {
				obs, haveObs = sim.Observe(sc, st), true
			}
			return obs
		}

		// Re-validate the recorded Commands against this Seed's Observation.
		var cmdIdx []int
		var cmds []sim.Command
		for i, in := range recorded {
			switch in.Kind {
			case sim.InputDisruption:
				if err := sim.ValidateDisruptionInput(&sc, in.Disruption, in.EffectiveTick); err != nil {
					return RunResult{}, fmt.Errorf("disruption at effective tick %d rejected: %w", int64(in.EffectiveTick), err)
				}
			case sim.InputCommand:
				cmdIdx = append(cmdIdx, i)
				cmds = append(cmds, in.Command)
			}
		}
		keep := make([]bool, len(recorded))
		for i, in := range recorded {
			keep[i] = in.Kind == sim.InputDisruption
		}
		accepted := 0
		if len(cmds) > 0 {
			if !open {
				for _, c := range cmds {
					rr.Skipped = append(rr.Skipped, Skipped{Tick: int64(eff), Kind: c.Kind.Name(), Reason: ReasonNoDecisionWindow,
						Message: fmt.Sprintf("no decision window is open at tick %d", int64(st.Tick()))})
				}
			} else {
				for _, r := range sim.Validate(sc, observe(), cmds, sc.CommandBudget) {
					if r.OK {
						keep[cmdIdx[r.Index]] = true
						accepted++
						continue
					}
					rr.Skipped = append(rr.Skipped, Skipped{Tick: int64(eff), Kind: r.Command.Kind.Name(), Reason: string(r.Code), Message: r.Message})
				}
			}
		}
		var batch []sim.Input
		for i, in := range recorded {
			if keep[i] {
				seq++
				in.Seq = seq
				batch = append(batch, in)
			}
		}

		// The policy decides at windows at or after the start Tick.
		if mgr != nil && open && int64(st.Tick()) >= arm.StartTick {
			budget := int(sc.CommandBudget) - accepted
			if budget > 0 {
				p, err := mgr.Propose(context.Background(), observe(), budget)
				if err != nil {
					return RunResult{}, fmt.Errorf("rule-based manager: %w", err)
				}
				for _, r := range sim.Validate(sc, observe(), p.Commands, int64(budget)) {
					if !r.OK {
						rr.Skipped = append(rr.Skipped, Skipped{Tick: int64(eff), Kind: r.Command.Kind.Name(), Reason: string(r.Code), Message: r.Message})
						continue
					}
					seq++
					batch = append(batch, sim.Input{EffectiveTick: eff, Seq: seq, Kind: sim.InputCommand, Command: r.Command})
					rr.Decisions++
				}
			}
		}

		step, err := sim.Advance(sc, st, batch)
		if err != nil {
			return RunResult{}, fmt.Errorf("advance to tick %d: %w", int64(eff), err)
		}
		st = step.State
	}
	rr.Hash = sim.Hash(st)
	rr.Metrics = sim.ComputeMetrics(sc, st)
	rr.Score = sim.ComputeScore(sc, rr.Metrics)
	rr.Cancelled = st.Orders.Cancelled
	return rr, nil
}
