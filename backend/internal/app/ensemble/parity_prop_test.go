package ensemble_test

import (
	"context"
	"encoding/hex"
	"reflect"
	"sync"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/rulebased"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

const (
	epScenarioPath = "../../../scenarios/black-friday.json"
	epPlanPath     = "../../../scenarios/demo-duel-human.json"
)

// epTick is what the engine exposes to a Manager and the exogenous draws of
// one Tick boundary.
type epTick struct {
	Open      bool
	Window    sim.ObsWindow
	Suppliers [][]sim.ObsSupplier
	Demand    [][2]int64 // (SKU, Potential_Arrivals) per OutcomeDemand
	Streams   []sim.NamedStream
}

type epRun struct {
	Ticks   []epTick
	Hash    [32]byte
	Skipped [][3]string // tick, kind, reason
}

// epSimulate independently re-simulates one arm under one seed through
// sim.Advance, mirroring the runner's rules (validation at the preceding
// window, budget, skip-never-alter).
func epSimulate(sc sim.Scenario, seed sim.Seed, plan []inputsfile.Input, adaptive bool) (epRun, error) {
	st, err := sim.Init(sc, seed)
	if err != nil {
		return epRun{}, err
	}
	var out epRun
	mgr := rulebased.New(sc)
	next := 0
	var seq uint64
	for int64(st.Tick()) < sc.HorizonTicks {
		eff := st.Tick() + 1
		open, _ := sim.DecisionWindowOpenState(sc, st)
		obs := sim.Observe(sc, st)

		var rec []sim.Command
		for next < len(plan) && plan[next].EffectiveTick == eff {
			rec = append(rec, *plan[next].Command)
			next++
		}
		var batch []sim.Input
		accepted := 0
		if len(rec) > 0 && !open {
			for _, c := range rec {
				out.Skipped = append(out.Skipped, [3]string{epItoa(int64(eff)), c.Kind.Name(), ensemble.ReasonNoDecisionWindow})
			}
		} else if len(rec) > 0 {
			for _, r := range sim.Validate(sc, obs, rec, sc.CommandBudget) {
				if !r.OK {
					out.Skipped = append(out.Skipped, [3]string{epItoa(int64(eff)), r.Command.Kind.Name(), string(r.Code)})
					continue
				}
				seq++
				accepted++
				batch = append(batch, sim.Input{EffectiveTick: eff, Seq: seq, Kind: sim.InputCommand, Command: r.Command})
			}
		}
		if adaptive && open {
			if budget := int(sc.CommandBudget) - accepted; budget > 0 {
				p, err := mgr.Propose(context.Background(), obs, budget)
				if err != nil {
					return epRun{}, err
				}
				for _, r := range sim.Validate(sc, obs, p.Commands, int64(budget)) {
					if !r.OK {
						out.Skipped = append(out.Skipped, [3]string{epItoa(int64(eff)), r.Command.Kind.Name(), string(r.Code)})
						continue
					}
					seq++
					batch = append(batch, sim.Input{EffectiveTick: eff, Seq: seq, Kind: sim.InputCommand, Command: r.Command})
				}
			}
		}

		step, err := sim.Advance(sc, st, batch)
		if err != nil {
			return epRun{}, err
		}
		rt := epTick{Open: open, Window: obs.Window}
		for _, s := range obs.SKUs {
			rt.Suppliers = append(rt.Suppliers, s.Suppliers)
		}
		for _, o := range step.Outcomes {
			if o.Kind == sim.OutcomeDemand {
				rt.Demand = append(rt.Demand, [2]int64{int64(o.SKU), o.Qty})
			}
		}
		for _, ns := range step.State.Streams {
			if ns.Name == sim.StreamDemand || ns.Name == sim.StreamDisruption {
				rt.Streams = append(rt.Streams, ns)
			}
		}
		out.Ticks = append(out.Ticks, rt)
		st = step.State
	}
	out.Hash = sim.Hash(st)
	return out, nil
}

func epItoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func epPlanFile(h string, ins []inputsfile.Input) *inputsfile.File {
	return &inputsfile.File{ScenarioHash: h, Seed: 1, EngineVersion: epItoa(int64(sim.EngineVersion)), Inputs: ins}
}

// Feature: parallel-simulation-mvp, Property 30: Ensemble parity
//
// Validates: Requirements 22.5 (INV-17)
//
// Each iteration runs a paired ensemble (random valid fixed plan drawn from
// the recorded demo plan, with injected invalid expedites, versus the
// rule-based arm) over 1..6 Seeds, and re-simulates every run through
// sim.Advance, comparing per-Tick decision windows, budget, horizon, lead
// times, Potential_Arrivals and demand/disruption stream states. The rapid
// default of 100 iterations is kept.
func TestPropEnsembleParity(t *testing.T) {
	sc, h, demo, err := epLoad()
	if err != nil {
		t.Fatal(err)
	}
	var cmdInputs []inputsfile.Input
	for _, in := range demo.Inputs {
		if in.Command != nil {
			cmdInputs = append(cmdInputs, in)
		}
	}
	if len(cmdInputs) == 0 {
		t.Fatal("demo plan has no commands")
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Random valid fixed plan: an ordered subset of the recorded commands,
		// plus optional invalid commands (unknown PO) at recorded ticks.
		var plan []inputsfile.Input
		for i, in := range cmdInputs {
			if rapid.Bool().Draw(rt, "keep") {
				plan = append(plan, in)
			}
			if rapid.IntRange(0, 3).Draw(rt, "ghost") == 0 {
				c := sim.Command{Kind: sim.CmdExpeditePurchaseOrder, PORef: uint64(900000 + i)}
				plan = append(plan, inputsfile.Input{EffectiveTick: in.EffectiveTick, Seq: 1, Command: &c, Provenance: inputsfile.ProvHuman})
			}
		}
		// Keep effective-tick order (stable): ghosts share their tick.
		plan = epSortStable(plan)

		n := rapid.IntRange(1, 6).Draw(rt, "nseeds")
		base := rapid.Uint64Range(1, 5000).Draw(rt, "base")
		seeds := make([]sim.Seed, n)
		cur := base
		for i := range seeds {
			seeds[i] = sim.Seed(cur)
			cur += uint64(rapid.IntRange(1, 7).Draw(rt, "gap"))
		}
		// Distinct by construction: gaps are >= 1.

		fixed := ensemble.Arm{Kind: ensemble.KindFixedReplay, File: epPlanFile(h, plan)}
		adaptive := ensemble.Arm{Kind: ensemble.KindAdaptiveEvaluation}
		res, err := ensemble.Run(context.Background(), ensemble.Config{Scenario: sc, ScenarioHash: h, Seeds: seeds, Arms: []ensemble.Arm{fixed, adaptive}})
		if err != nil {
			rt.Fatal(err)
		}
		// Independent re-simulation of every (seed, arm), in parallel to keep
		// 100 iterations affordable.
		sims := make([][2]epRun, n)
		errs := make([][2]error, n)
		var wg sync.WaitGroup
		for i, s := range seeds {
			for a := 0; a < 2; a++ {
				wg.Add(1)
				go func(i, a int, s sim.Seed) {
					defer wg.Done()
					if a == 0 {
						sims[i][a], errs[i][a] = epSimulate(*sc, s, plan, false)
					} else {
						sims[i][a], errs[i][a] = epSimulate(*sc, s, nil, true)
					}
				}(i, a, s)
			}
		}
		wg.Wait()
		for i := range errs {
			for a := range errs[i] {
				if errs[i][a] != nil {
					rt.Fatal(errs[i][a])
				}
			}
		}
		// seeds were generated ascending, which is the runner's order.

		// One horizon and one comparison tick for both arms.
		if res.HorizonTicks != sc.HorizonTicks || res.ScenarioHash != h || res.EngineVersion != sim.EngineVersion {
			rt.Fatalf("header: horizon %d hash %s version %d", res.HorizonTicks, res.ScenarioHash, res.EngineVersion)
		}
		if len(res.Runs) != 2*n || !reflect.DeepEqual(res.Seeds, seeds) {
			rt.Fatalf("runs = %d, want %d; seeds %v", len(res.Runs), 2*n, res.Seeds)
		}
		for i, r := range res.Runs {
			if r.ArmIndex != i/n || r.Seed != res.Seeds[i%n] {
				rt.Fatalf("run %d is arm %d seed %d", i, r.ArmIndex, uint64(r.Seed))
			}
		}

		for si, seed := range res.Seeds {
			fr, ar := res.Runs[si], res.Runs[n+si]
			fixedSim, adaptSim := sims[si][0], sims[si][1]
			if fixedSim.Hash != fr.Hash || adaptSim.Hash != ar.Hash {
				rt.Fatalf("seed %d: runner hash differs from independent re-simulation (%s vs %s)",
					uint64(seed), hex.EncodeToString(fr.Hash[:4]), hex.EncodeToString(fixedSim.Hash[:4]))
			}

			// Identical exogenous conditions and constraints, tick by tick.
			if len(fixedSim.Ticks) != int(sc.HorizonTicks) || len(adaptSim.Ticks) != int(sc.HorizonTicks) {
				rt.Fatalf("seed %d: comparison tick differs from horizon", uint64(seed))
			}
			for k := range fixedSim.Ticks {
				a, b := fixedSim.Ticks[k], adaptSim.Ticks[k]
				if a.Open != b.Open || !reflect.DeepEqual(a.Window, b.Window) {
					rt.Fatalf("seed %d tick %d: decision window, budget or cadence differ", uint64(seed), k)
				}
				if !reflect.DeepEqual(a.Suppliers, b.Suppliers) {
					rt.Fatalf("seed %d tick %d: lead times differ", uint64(seed), k)
				}
				if !reflect.DeepEqual(a.Demand, b.Demand) {
					rt.Fatalf("seed %d tick %d: potential arrivals differ: %v vs %v", uint64(seed), k, a.Demand, b.Demand)
				}
				if !reflect.DeepEqual(a.Streams, b.Streams) {
					rt.Fatalf("seed %d tick %d: demand/disruption stream state differs", uint64(seed), k)
				}
			}

			// Skipped commands: identical per rule, never altered. Every ghost
			// expedite is skipped as an unknown PO; the runner agrees with the
			// re-simulation exactly.
			got := make([][3]string, len(fr.Skipped))
			for i, s := range fr.Skipped {
				got[i] = [3]string{epItoa(s.Tick), s.Kind, s.Reason}
			}
			if !reflect.DeepEqual(got, fixedSim.Skipped) && !(len(got) == 0 && len(fixedSim.Skipped) == 0) {
				rt.Fatalf("seed %d: skipped %v, re-simulation %v", uint64(seed), got, fixedSim.Skipped)
			}
			ghosts := 0
			for _, in := range plan {
				if in.Command.Kind == sim.CmdExpeditePurchaseOrder && in.Command.PORef >= 900000 {
					ghosts++
				}
			}
			unknown := 0
			for _, s := range fr.Skipped {
				if s.Kind == "expedite_purchase_order" {
					unknown++
				}
			}
			if unknown < ghosts {
				rt.Fatalf("seed %d: %d invalid commands, only %d skipped as such", uint64(seed), ghosts, unknown)
			}
		}
	})
}

func epLoad() (*sim.Scenario, string, inputsfile.File, error) {
	sc, h, err := replayfile.LoadScenario(epScenarioPath)
	if err != nil {
		return nil, "", inputsfile.File{}, err
	}
	f, err := replayfile.LoadInputsAnySeed(epPlanPath, h)
	return sc, h, f, err
}

func epSortStable(in []inputsfile.Input) []inputsfile.Input {
	out := append([]inputsfile.Input(nil), in...)
	for i := 1; i < len(out); i++ { // insertion sort is stable
		for j := i; j > 0 && out[j].EffectiveTick < out[j-1].EffectiveTick; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
