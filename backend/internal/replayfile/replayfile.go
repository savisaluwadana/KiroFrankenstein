// Package replayfile loads a Scenario file and an Inputs_File and replays them
// through the engine with the validator gate. It is shared by the parallel-sim
// CLI (run, verify) and the golden flagship hash test so both replay the same
// way. It may import sim and inputsfile; sim never imports it.
package replayfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

// ErrEngineVersion is wrapped by LoadInputs when the file was recorded under
// another engine version.
var ErrEngineVersion = errors.New("engine version mismatch")

// ScenarioHash returns the lowercase hex SHA-256 of the canonical Scenario.
func ScenarioHash(sc *sim.Scenario) string {
	h := sha256.Sum256(sim.PrintScenario(sc))
	return hex.EncodeToString(h[:])
}

// LoadScenario reads, parses and validates a Scenario file and returns it
// with its Scenario hash.
func LoadScenario(path string) (*sim.Scenario, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sc, err := sim.ParseScenario(data)
	if err != nil {
		return nil, "", err
	}
	if err := sim.ValidateScenario(sc); err != nil {
		return nil, "", err
	}
	return sc, ScenarioHash(sc), nil
}

// LoadInputs reads an Inputs_File and checks it against the Scenario hash,
// the Seed and the engine version. A different engine version wraps
// ErrEngineVersion.
func LoadInputs(path, scenarioHash string, seed sim.Seed) (inputsfile.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return inputsfile.File{}, err
	}
	f, err := inputsfile.Parse(data)
	if err != nil {
		return f, err
	}
	if want := strconv.Itoa(sim.EngineVersion); f.EngineVersion != want {
		return f, fmt.Errorf("%w: inputs file was recorded under engine version %s, this binary is engine version %s", ErrEngineVersion, f.EngineVersion, want)
	}
	if f.ScenarioHash != scenarioHash {
		return f, fmt.Errorf("scenario mismatch: inputs file scenarioHash %s, scenario file hashes to %s", f.ScenarioHash, scenarioHash)
	}
	if f.Seed != seed {
		return f, fmt.Errorf("seed mismatch: inputs file seed %d, --seed %d", uint64(f.Seed), uint64(seed))
	}
	return f, nil
}

// Simulate runs the Scenario from Init to untilTick (the horizon when
// untilTick is negative). Inputs are applied at their effective Ticks: each
// Command batch goes through sim.Validate against the Observation of its
// Decision_Window with the window's Command_Budget, each Disruption through
// sim.ValidateDisruptionInput. Any rejected Input is an error; nothing is
// altered or skipped. onTick, if not nil, is called with the State at Tick 0
// and after every Tick.
func Simulate(sc *sim.Scenario, seed sim.Seed, f *inputsfile.File, untilTick int64, onTick func(sim.State)) (sim.State, error) {
	st, err := sim.Init(*sc, seed)
	if err != nil {
		return st, err
	}
	if onTick != nil {
		onTick(st)
	}
	if untilTick < 0 || untilTick > sc.HorizonTicks {
		untilTick = sc.HorizonTicks
	}
	var ins []sim.Input
	if f != nil {
		if ins, err = inputsfile.ToInputs(*f); err != nil {
			return st, err
		}
		for i := range ins {
			ins[i].Seq = uint64(i) + 1 // as the app renumbers: engine Seq is the position
			if int64(ins[i].EffectiveTick) > sc.HorizonTicks {
				return st, fmt.Errorf("inputs[%d]: effective tick %d is after the horizon %d", i, int64(ins[i].EffectiveTick), sc.HorizonTicks)
			}
		}
	}
	next := 0 // first input not yet consumed (inputs are ordered by effective tick)
	for int64(st.Tick()) < untilTick {
		eff := st.Tick() + 1
		start := next
		for next < len(ins) && ins[next].EffectiveTick == eff {
			next++
		}
		if next < len(ins) && ins[next].EffectiveTick < eff {
			return st, fmt.Errorf("inputs[%d]: effective tick %d is not after tick 0", next, int64(ins[next].EffectiveTick))
		}
		batch := ins[start:next]
		if err := checkBatch(sc, st, batch, start); err != nil {
			return st, err
		}
		step, err := sim.Advance(*sc, st, batch)
		if err != nil {
			return st, err
		}
		st = step.State
		if onTick != nil {
			onTick(st)
		}
	}
	return st, nil
}

// checkBatch gates the Inputs taking effect at st.Tick()+1. base is the file
// index of batch[0], used in messages.
func checkBatch(sc *sim.Scenario, st sim.State, batch []sim.Input, base int) error {
	var cmds []sim.Command
	var idx []int
	for i, in := range batch {
		switch in.Kind {
		case sim.InputDisruption:
			if err := sim.ValidateDisruptionInput(sc, in.Disruption, in.EffectiveTick); err != nil {
				return fmt.Errorf("inputs[%d]: disruption rejected: %v", base+i, err)
			}
		case sim.InputCommand:
			cmds = append(cmds, in.Command)
			idx = append(idx, base+i)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	tick := st.Tick()
	if open, _ := sim.DecisionWindowOpenState(*sc, st); !open {
		return fmt.Errorf("inputs[%d]: command at effective tick %d but no decision window is open at tick %d", idx[0], int64(tick)+1, int64(tick))
	}
	obs := sim.Observe(*sc, st)
	for _, r := range sim.Validate(*sc, obs, cmds, sc.CommandBudget) {
		if !r.OK {
			return fmt.Errorf("inputs[%d]: command rejected at window tick %d: %s: %s", idx[r.Index], int64(tick), r.Code, r.Message)
		}
	}
	return nil
}
