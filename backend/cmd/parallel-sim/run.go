package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

// Exit codes of parallel-sim.
const (
	exitOK               = 0
	exitMismatch         = 1 // verify: computed hash differs from --expect
	exitEngineVersion    = 2 // Inputs_File recorded under another engine version
	exitUsageOrFileError = 3 // usage, I/O, invalid file, rejected input, scenario mismatch
)

// exitError carries the exit code a failed command maps to.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func fail(code int, format string, a ...any) error {
	return &exitError{code: code, err: fmt.Errorf(format, a...)}
}

// wrap maps a replayfile error to its exit code: an engine version mismatch
// exits 2, everything else 3.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, replayfile.ErrEngineVersion) {
		return fail(exitEngineVersion, "%v", err)
	}
	return fail(exitUsageOrFileError, "%v", err)
}

// loadScenario reads, parses and validates a Scenario file and returns it
// with the SHA-256 of its canonical encoding (the world's Scenario hash).
func loadScenario(path string) (*sim.Scenario, string, error) {
	if path == "" {
		return nil, "", fail(exitUsageOrFileError, "--scenario is required")
	}
	sc, h, err := replayfile.LoadScenario(path)
	return sc, h, wrap(err)
}

// loadInputs reads an Inputs_File and checks it against the Scenario hash,
// the Seed and the engine version. A different engine version exits 2; any
// other disagreement exits 3.
func loadInputs(path, scenarioHash string, seed sim.Seed) (inputsfile.File, error) {
	f, err := replayfile.LoadInputs(path, scenarioHash, seed)
	return f, wrap(err)
}

// simulate replays through the shared validator-gated replay code.
func simulate(sc *sim.Scenario, seed sim.Seed, f *inputsfile.File, untilTick int64) (sim.State, error) {
	st, err := replayfile.Simulate(sc, seed, f, untilTick, nil)
	return st, wrap(err)
}

func printReport(out io.Writer, sc *sim.Scenario, seed sim.Seed, st sim.State) [32]byte {
	m := sim.ComputeMetrics(*sc, st)
	h := sim.Hash(st)
	fmt.Fprintf(out, "scenario: %s\nseed: %d\nengine_version: %d\n", sc.Name, uint64(seed), sim.EngineVersion)
	fmt.Fprintf(out, "tick: %d\n", int64(st.Tick()))
	fmt.Fprintf(out, "state_hash: %x\n", h[:])
	fmt.Fprintf(out, "net_profit_cents: %d\n", int64(m.NetProfit))
	fmt.Fprintf(out, "service_level_bps: %d\n", int64(m.ServiceLevel))
	fmt.Fprintf(out, "%s\n", sim.Disclaimer)
	return h
}

// runCmd runs a Scenario under a Seed to the horizon, optionally applying the
// Inputs of an Inputs_File, and prints the final State_Hash, Net_Profit,
// Service_Level and the Disclaimer.
func runCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("scenario", "", "path to the Scenario JSON file")
	seed := fs.Uint64("seed", 0, "world seed")
	inputs := fs.String("inputs", "", "path to an Inputs_File to apply")
	if err := fs.Parse(args); err != nil {
		return fail(exitUsageOrFileError, "%v", err)
	}
	sc, hash, err := loadScenario(*path)
	if err != nil {
		return err
	}
	var f *inputsfile.File
	if *inputs != "" {
		lf, err := loadInputs(*inputs, hash, sim.Seed(*seed))
		if err != nil {
			return err
		}
		f = &lf
	}
	st, err := simulate(sc, sim.Seed(*seed), f, -1)
	if err != nil {
		return asExit(err)
	}
	printReport(out, sc, sim.Seed(*seed), st)
	return nil
}

// verifyCmd replays a Scenario, Seed and Inputs_File and compares the State_Hash
// at --tick (default the horizon) with --expect. Errors exit 3 (see exit
// constants); a mismatch is reported as exit 1 by the caller through the
// returned code.
func verifyCmd(args []string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("scenario", "", "path to the Scenario JSON file")
	seed := fs.Uint64("seed", 0, "world seed")
	inputs := fs.String("inputs", "", "path to the Inputs_File")
	expect := fs.String("expect", "", "expected State_Hash (64 hex characters)")
	tick := fs.Int64("tick", -1, "Tick to verify (default: the horizon)")
	if err := fs.Parse(args); err != nil {
		return exitUsageOrFileError, fail(exitUsageOrFileError, "%v", err)
	}
	if *inputs == "" {
		return exitUsageOrFileError, fail(exitUsageOrFileError, "--inputs is required")
	}
	want, err := hex.DecodeString(*expect)
	if err != nil || len(want) != 32 {
		return exitUsageOrFileError, fail(exitUsageOrFileError, "--expect must be 64 hexadecimal characters")
	}
	sc, hash, err := loadScenario(*path)
	if err != nil {
		return codeOf(err), err
	}
	f, err := loadInputs(*inputs, hash, sim.Seed(*seed))
	if err != nil {
		return codeOf(err), err
	}
	if *tick > sc.HorizonTicks {
		return exitUsageOrFileError, fail(exitUsageOrFileError, "--tick %d is after the horizon %d", *tick, sc.HorizonTicks)
	}
	st, err := simulate(sc, sim.Seed(*seed), &f, *tick)
	if err != nil {
		return codeOf(err), err
	}
	h := printReport(out, sc, sim.Seed(*seed), st)
	if string(h[:]) == string(want) {
		fmt.Fprintln(out, "MATCH")
		return exitOK, nil
	}
	fmt.Fprintf(out, "expected_hash: %x\nMISMATCH\n", want)
	return exitMismatch, nil
}

func asExit(err error) error {
	var ee *exitError
	if errors.As(err, &ee) {
		return err
	}
	return fail(exitUsageOrFileError, "%v", err)
}

func codeOf(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return exitUsageOrFileError
}
