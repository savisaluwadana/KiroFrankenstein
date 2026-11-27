package main

import (
	"flag"
	"io"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/calibration"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// calibrateCmd prints the calibration tables of docs/calibration.md for a
// Scenario: the no-Command run, the Rule_Based_Manager, the recorded demo plan
// and fixed human-style plans that isolate the inventory and staffing levers,
// over a Seed list. It reports observed results; it has no target and no
// winner. No network, no database, no LLM.
func calibrateCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("scenario", "", "path to the Scenario JSON file")
	seedsSpec := fs.String("seeds", "1..20", "seed list: 1..20 or 1,2,3 (1 to 50 seeds)")
	if err := fs.Parse(args); err != nil {
		return fail(exitUsageOrFileError, "%v", err)
	}
	if fs.NArg() > 0 {
		return fail(exitUsageOrFileError, "unexpected argument %q", fs.Arg(0))
	}
	seeds, err := ensemble.ParseSeeds(*seedsSpec)
	if err != nil {
		return fail(exitUsageOrFileError, "%v", err)
	}
	sc, _, err := loadScenario(*path)
	if err != nil {
		return err
	}
	if err := calibration.WriteReport(out, *sc, seeds); err != nil {
		return fail(exitUsageOrFileError, "%v", err)
	}
	_, err = io.WriteString(out, "\n"+sim.Disclaimer+"\n")
	return err
}
