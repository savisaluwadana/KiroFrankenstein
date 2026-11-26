package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// runCmd runs a Scenario under a Seed with no Commands to the horizon and
// prints the final State_Hash, Net_Profit, Service_Level and the Disclaimer.
func runCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("scenario", "", "path to the Scenario JSON file")
	seed := fs.Uint64("seed", 0, "world seed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--scenario is required")
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	sc, err := sim.ParseScenario(data)
	if err != nil {
		return err
	}
	if err := sim.ValidateScenario(sc); err != nil {
		return err
	}
	st, err := sim.Init(*sc, sim.Seed(*seed))
	if err != nil {
		return err
	}
	// Windows are not acted on: continue with no inputs.
	for int64(st.Tick()) < sc.HorizonTicks {
		step, err := sim.Advance(*sc, st, nil)
		if err != nil {
			return err
		}
		st = step.State
	}
	m := sim.ComputeMetrics(*sc, st)
	h := sim.Hash(st)
	fmt.Fprintf(out, "scenario: %s\nseed: %d\nengine_version: %d\n", sc.Name, *seed, sim.EngineVersion)
	fmt.Fprintf(out, "tick: %d\n", int64(st.Tick()))
	fmt.Fprintf(out, "state_hash: %x\n", h[:])
	fmt.Fprintf(out, "net_profit_cents: %d\n", int64(m.NetProfit))
	fmt.Fprintf(out, "service_level_bps: %d\n", int64(m.ServiceLevel))
	fmt.Fprintf(out, "%s\n", sim.Disclaimer)
	return nil
}
