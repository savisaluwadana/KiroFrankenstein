package ensemble_test

import (
	"context"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/rulebased"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
	"strconv"
)

const rrScenarioPath = "../../../scenarios/black-friday.json"

// rrPlay records a valid input log: at every open Decision_Window a random
// subset of the rule-based proposals (each passing the validator) is applied.
// It returns the log as an Inputs_File and the final State of the original run.
func rrPlay(t *rapid.T, sc *sim.Scenario, hash string, seed sim.Seed) (inputsfile.File, sim.State) {
	st, err := sim.Init(*sc, seed)
	if err != nil {
		t.Fatal(err)
	}
	mgr := rulebased.New(*sc)
	var ins []sim.Input
	var meta []inputsfile.Meta
	var seq uint64
	for int64(st.Tick()) < sc.HorizonTicks {
		eff := st.Tick() + 1
		var batch []sim.Input
		if open, _ := sim.DecisionWindowOpenState(*sc, st); open {
			obs := sim.Observe(*sc, st)
			budget := rapid.IntRange(0, int(sc.CommandBudget)).Draw(t, "budget")
			if budget > 0 {
				p, err := mgr.Propose(context.Background(), obs, budget)
				if err != nil {
					t.Fatal(err)
				}
				var picked []sim.Command
				for _, c := range p.Commands {
					if rapid.Bool().Draw(t, "pick") {
						picked = append(picked, c)
					}
				}
				for _, r := range sim.Validate(*sc, obs, picked, sc.CommandBudget) {
					if !r.OK {
						continue
					}
					seq++
					in := sim.Input{EffectiveTick: eff, Seq: seq, Kind: sim.InputCommand, Command: r.Command}
					batch = append(batch, in)
					ins = append(ins, in)
					meta = append(meta, inputsfile.Meta{Provenance: inputsfile.ProvHuman})
				}
			}
		}
		step, err := sim.Advance(*sc, st, batch)
		if err != nil {
			t.Fatal(err)
		}
		st = step.State
	}
	fin, err := inputsfile.FromInputs(ins, meta)
	if err != nil {
		t.Fatal(err)
	}
	return inputsfile.File{ScenarioHash: hash, Seed: seed, EngineVersion: strconv.Itoa(sim.EngineVersion), Inputs: fin}, st
}

// Feature: parallel-simulation-mvp, Property 28: Fixed-replay round trip under original seed
// Validates: Requirements 24.15 (INV-16)
func TestPropFixedReplayRoundTrip(t *testing.T) {
	sc, hash, err := replayfile.LoadScenario(rrScenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	rapid.Check(t, func(t *rapid.T) {
		seed := sim.Seed(rapid.Uint64Range(1, inputsfile.MaxSeed).Draw(t, "seed"))
		f, orig := rrPlay(t, sc, hash, seed)
		wantHash := sim.Hash(orig)
		wantMetrics := sim.ComputeMetrics(*sc, orig)

		res, err := ensemble.Run(context.Background(), ensemble.Config{
			Scenario: sc, ScenarioHash: hash, Seeds: []sim.Seed{seed},
			Arms: []ensemble.Arm{{Kind: ensemble.KindFixedReplay, File: &f}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Runs) != 1 {
			t.Fatalf("runs = %d", len(res.Runs))
		}
		r := res.Runs[0]
		if len(r.Skipped) != 0 {
			t.Fatalf("skipped commands under the original seed: %+v", r.Skipped)
		}
		if r.Hash != wantHash {
			t.Fatalf("ensemble hash %x != original %x", r.Hash, wantHash)
		}
		if !reflect.DeepEqual(r.Metrics, wantMetrics) {
			t.Fatalf("metrics differ:\n got %+v\nwant %+v", r.Metrics, wantMetrics)
		}
		st, err := replayfile.Simulate(sc, seed, &f, -1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if sim.Hash(st) != wantHash {
			t.Fatal("replayfile.Simulate hash differs from the original")
		}
	})
}
