// Package simtest holds shared test helpers for the simulation: a loader for
// the flagship Scenario, pgregory.net/rapid generators (Scenario overrides,
// Seeds, validator-accepted Input logs) and helpers that drive sim.Init and
// sim.Advance to a Tick or to the horizon.
//
// It lives outside internal/sim so it may import rapid and the os package; it
// is imported only from tests.
package simtest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Horizon bounds used by generators. The validator's minimum horizon is 24.
const (
	MinHorizon   = 24
	ShortHorizon = 48 // default upper bound for generated scenarios
)

var (
	flagshipOnce sync.Once
	flagship     *sim.Scenario
	flagshipErr  error
)

// FlagshipPath returns the absolute path of backend/scenarios/black-friday.json,
// located relative to this source file.
func FlagshipPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "scenarios", "black-friday.json")
}

// LoadFlagship parses and validates the flagship Scenario. Each call returns an
// independent copy.
func LoadFlagship() (*sim.Scenario, error) {
	flagshipOnce.Do(func() {
		data, err := os.ReadFile(FlagshipPath())
		if err != nil {
			flagshipErr = err
			return
		}
		sc, err := sim.ParseScenario(data)
		if err != nil {
			flagshipErr = err
			return
		}
		if err := sim.ValidateScenario(sc); err != nil {
			flagshipErr = err
			return
		}
		flagship = sc
	})
	if flagshipErr != nil {
		return nil, flagshipErr
	}
	return sim.ApplyOverrides(flagship, nil) // deep copy
}

// MustFlagship is LoadFlagship that panics on error.
func MustFlagship() *sim.Scenario {
	sc, err := LoadFlagship()
	if err != nil {
		panic(fmt.Sprintf("simtest: load flagship: %v", err))
	}
	return sc
}

// WithHorizon returns a validated copy of base with horizonTicks overridden
// (minimum MinHorizon), for fast property tests.
func WithHorizon(base *sim.Scenario, ticks int64) (*sim.Scenario, error) {
	return sim.ApplyOverridesAndValidate(base, map[string]int64{"horizonTicks": ticks})
}

// overrideSpec is one curated Assumption override range (inside its declared
// bounds in the flagship's assumptionMeta).
type overrideSpec struct {
	path     string
	min, max int64
}

var curatedOverrides = []overrideSpec{
	{"openingCashCents", 2_000_000, 20_000_000},
	{"demand.baseArrivalsPerTick", 20, 200},
	{"demand.baseConversionBps", 1000, 4000},
	{"demand.priceElasticityBps", 0, 5000},
	{"workforce.regularWorkers", 8, 30},
	{"commandBudget", 1, 5},
	{"skus.headphones.openingUnits", 0, 2000},
	{"suppliers.headphones.primary.leadTimeTicks", 4, 36},
}

// Overrides generates a map of curated Assumption overrides, each within
// bounds, plus horizonTicks in [MinHorizon, maxHorizon].
func Overrides(maxHorizon int64) *rapid.Generator[map[string]int64] {
	if maxHorizon < MinHorizon {
		maxHorizon = MinHorizon
	}
	return rapid.Custom(func(t *rapid.T) map[string]int64 {
		m := map[string]int64{
			"horizonTicks": rapid.Int64Range(MinHorizon, maxHorizon).Draw(t, "horizonTicks"),
		}
		for _, o := range curatedOverrides {
			if rapid.Bool().Draw(t, "use "+o.path) {
				m[o.path] = rapid.Int64Range(o.min, o.max).Draw(t, o.path)
			}
		}
		return m
	})
}

// Scenarios generates validated Scenarios derived from the flagship with
// Overrides applied. Horizons are in [MinHorizon, maxHorizon].
func Scenarios(maxHorizon int64) *rapid.Generator[*sim.Scenario] {
	return rapid.Custom(func(t *rapid.T) *sim.Scenario {
		base := MustFlagship()
		sc, err := sim.ApplyOverridesAndValidate(base, Overrides(maxHorizon).Draw(t, "overrides"))
		if err != nil {
			t.Fatalf("generated overrides rejected by validator: %v", err)
		}
		return sc
	})
}

// Seeds generates world Seeds.
func Seeds() *rapid.Generator[sim.Seed] {
	return rapid.Custom(func(t *rapid.T) sim.Seed {
		return sim.Seed(rapid.Uint64().Draw(t, "seed"))
	})
}

// Proposals maps a Decision_Window Tick to the raw (not yet validated)
// Commands a manager proposes there.
type Proposals map[sim.Tick][]sim.Command

// GenProposals generates raw proposals for sc: only at Ticks where
// sim.DecisionWindowOpen is true, with up to sc.CommandBudget+2 commands per
// window so that the budget and cash checks are exercised. Kinds are limited
// to the implemented ones (set_price, place_purchase_order).
func GenProposals(sc *sim.Scenario) *rapid.Generator[Proposals] {
	return rapid.Custom(func(t *rapid.T) Proposals {
		p := Proposals{}
		nSKU := int64(len(sc.SKUs))
		for tick := sim.Tick(0); int64(tick) < sc.HorizonTicks; tick++ {
			if open, _ := sim.DecisionWindowOpen(*sc, tick); !open {
				continue
			}
			n := rapid.IntRange(0, int(sc.CommandBudget)+2).Draw(t, "ncmds")
			for i := 0; i < n; i++ {
				sku := rapid.Int64Range(0, nSKU-1).Draw(t, "sku")
				if rapid.Bool().Draw(t, "isPrice") {
					lo, hi := sim.PriceBounds(*sc, sc.SKUs[sku].ReferencePriceCents)
					p[tick] = append(p[tick], sim.Command{
						Kind: sim.CmdSetPrice, SKU: sku,
						AmountCents: sim.Cents(rapid.Int64Range(int64(lo), int64(hi)).Draw(t, "price")),
					})
				} else {
					p[tick] = append(p[tick], sim.Command{
						Kind: sim.CmdPlacePurchaseOrder, SKU: sku,
						Role: uint8(rapid.IntRange(0, 1).Draw(t, "role")),
						Qty:  rapid.Int64Range(1, 400).Draw(t, "qty"),
					})
				}
			}
		}
		return p
	})
}

// BuildInputLog drives the engine from Init, and at every Decision_Window Tick
// validates the proposals against the Observation at that Tick with
// sim.Validate, keeping only accepted Commands (at most sc.CommandBudget). Each
// kept Command becomes an Input with EffectiveTick = window Tick + 1 and a
// strictly increasing Seq (starting at 1). The result passes the validator by
// construction.
func BuildInputLog(sc *sim.Scenario, seed sim.Seed, props Proposals) ([]sim.Input, error) {
	st, err := sim.Init(*sc, seed)
	if err != nil {
		return nil, err
	}
	var log []sim.Input
	var seq uint64
	for int64(st.Tick()) < sc.HorizonTicks {
		tick := st.Tick()
		var step []sim.Input
		if open, _ := sim.DecisionWindowOpen(*sc, tick); open {
			if cmds := props[tick]; len(cmds) > 0 {
				obs := sim.Observe(*sc, st)
				for _, r := range sim.Validate(*sc, obs, cmds, sc.CommandBudget) {
					if !r.OK {
						continue
					}
					seq++
					step = append(step, sim.Input{
						EffectiveTick: tick + 1, Seq: seq,
						Kind: sim.InputCommand, Command: r.Command,
					})
				}
			}
		}
		log = append(log, step...)
		adv, err := sim.Advance(*sc, st, step)
		if err != nil {
			return nil, err
		}
		st = adv.State
	}
	return log, nil
}

// Case is a generated test case: a Scenario, a Seed and a validator-accepted
// Input log.
type Case struct {
	Scenario *sim.Scenario
	Seed     sim.Seed
	Inputs   []sim.Input
}

// Cases generates Cases with horizons in [MinHorizon, maxHorizon].
func Cases(maxHorizon int64) *rapid.Generator[Case] {
	return rapid.Custom(func(t *rapid.T) Case {
		sc := Scenarios(maxHorizon).Draw(t, "scenario")
		seed := Seeds().Draw(t, "seed")
		props := GenProposals(sc).Draw(t, "proposals")
		log, err := BuildInputLog(sc, seed, props)
		if err != nil {
			t.Fatalf("build input log: %v", err)
		}
		return Case{Scenario: sc, Seed: seed, Inputs: log}
	})
}

// Run is the result of RunToTick or RunToHorizon. Index i of Hashes and States
// is the State at Tick i (index 0 is the initial State).
type Run struct {
	Hashes [][32]byte
	States []sim.State
}

// Final returns the last State of the run.
func (r Run) Final() sim.State { return r.States[len(r.States)-1] }

// RunToTick runs sim.Init and sim.Advance up to and including tick (clamped to
// the horizon), applying at each step the inputs whose EffectiveTick equals the
// Tick being computed.
func RunToTick(sc *sim.Scenario, seed sim.Seed, inputs []sim.Input, tick sim.Tick) (Run, error) {
	st, err := sim.Init(*sc, seed)
	if err != nil {
		return Run{}, err
	}
	run := Run{Hashes: [][32]byte{sim.Hash(st)}, States: []sim.State{st}}
	byTick := map[sim.Tick][]sim.Input{}
	for _, in := range inputs {
		byTick[in.EffectiveTick] = append(byTick[in.EffectiveTick], in)
	}
	for st.Tick() < tick && int64(st.Tick()) < sc.HorizonTicks {
		adv, err := sim.Advance(*sc, st, byTick[st.Tick()+1])
		if err != nil {
			return run, fmt.Errorf("advance to tick %d: %w", st.Tick()+1, err)
		}
		st = adv.State
		run.Hashes = append(run.Hashes, sim.Hash(st))
		run.States = append(run.States, st)
	}
	return run, nil
}

// RunToHorizon is RunToTick to sc.HorizonTicks.
func RunToHorizon(sc *sim.Scenario, seed sim.Seed, inputs []sim.Input) (Run, error) {
	return RunToTick(sc, seed, inputs, sim.Tick(sc.HorizonTicks))
}
