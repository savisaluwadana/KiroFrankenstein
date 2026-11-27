package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

// armFlags collects repeated --arm values.
type armFlags []string

func (a *armFlags) String() string     { return strings.Join(*a, " ") }
func (a *armFlags) Set(v string) error { *a = append(*a, v); return nil }

// ensembleCmd runs one or two Arms under a Seed list (Req 24, 25) and prints a
// plain summary. The canonical JSON and CSV report (task 7.10) plugs in at
// writeReport.
func ensembleCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ensemble", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("scenario", "", "path to the Scenario JSON file")
	seedsSpec := fs.String("seeds", "", "seed list: 1..20 or 1,2,3 (1 to 50 seeds)")
	conc := fs.Int("concurrency", 0, "worker pool size (default GOMAXPROCS)")
	prefix := fs.String("prefix", "", "prefix Inputs_File applied to rulebased arms up to their start tick")
	outPrefix := fs.String("out", "", "output path prefix: writes <out>.json and <out>.csv (parent directory must exist)")
	tie := fs.Int64("tie-tolerance", -1, "Tie_Tolerance in hundredths of a point (default: the Scenario's)")
	var arms armFlags
	fs.Var(&arms, "arm", "fixed:<inputsfile> | rulebased[@startTick] | adaptive:<inputsfile>[@startTick] (repeat for two arms)")
	if err := fs.Parse(args); err != nil {
		return fail(exitUsageOrFileError, "%v", err)
	}
	if fs.NArg() > 0 {
		return fail(exitUsageOrFileError, "unexpected argument %q", fs.Arg(0))
	}
	if *conc < 0 {
		return fail(exitUsageOrFileError, "--concurrency must not be negative")
	}
	if *tie < -1 {
		return fail(exitUsageOrFileError, "--tie-tolerance must not be negative")
	}
	// Cheap checks first, so a bad invocation never reads big files.
	seeds, err := ensemble.ParseSeeds(*seedsSpec)
	if err != nil {
		return fail(exitUsageOrFileError, "%v", err)
	}
	specs := make([]ensemble.ArmSpec, 0, len(arms))
	for _, a := range arms {
		s, err := ensemble.ParseArmSpec(a)
		if err != nil {
			return fail(exitUsageOrFileError, "%v", err)
		}
		specs = append(specs, s)
	}
	sc, hash, err := loadScenario(*path)
	if err != nil {
		return err
	}
	var prefixFile *inputsfile.File
	if *prefix != "" {
		f, err := replayfile.LoadInputsAnySeed(*prefix, hash)
		if err != nil {
			return wrap(err)
		}
		prefixFile = &f
	}
	cfgArms := make([]ensemble.Arm, 0, len(specs))
	for i, s := range specs {
		a := ensemble.Arm{Kind: s.Kind, Name: arms[i], StartTick: s.StartTick}
		file := prefixFile
		if s.Path != "" {
			f, err := replayfile.LoadInputsAnySeed(s.Path, hash)
			if err != nil {
				return wrap(err)
			}
			file = &f
		}
		switch s.Kind {
		case ensemble.KindFixedReplay:
			a.File = file
		case ensemble.KindAdaptiveEvaluation:
			a.File = file
			if !s.StartSet {
				a.StartTick = ensemble.DefaultStart(file)
			}
		}
		cfgArms = append(cfgArms, a)
	}
	res, err := ensemble.Run(context.Background(), ensemble.Config{
		Scenario: sc, ScenarioHash: hash, Seeds: seeds, Arms: cfgArms, Concurrency: *conc,
	})
	if err != nil {
		if errors.Is(err, replayfile.ErrEngineVersion) {
			return fail(exitEngineVersion, "%v", err)
		}
		return fail(exitUsageOrFileError, "%v", err)
	}
	tolerance := sc.TieToleranceHundredths
	if *tie >= 0 {
		tolerance = *tie
	}
	return writeReport(out, sc, res, tolerance, *outPrefix)
}

// writeReport prints a plain per-run table to out and, when outPrefix is set,
// writes the canonical Ensemble_Report to <outPrefix>.json and <outPrefix>.csv
// (the parent directory must exist).
func writeReport(out io.Writer, sc *sim.Scenario, res ensemble.Result, tolerance int64, outPrefix string) error {
	fmt.Fprintf(out, "scenario: %s\nscenario_hash: %s\nengine_version: %d\nhorizon_ticks: %d\nseeds: %d\ntie_tolerance_hundredths: %d\n",
		sc.Name, res.ScenarioHash, res.EngineVersion, res.HorizonTicks, len(res.Seeds), tolerance)
	for i, a := range res.Arms {
		fmt.Fprintf(out, "arm %d: %s [%s]\n", i, a.Label(), a.Name)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "arm\tseed\tstate_hash\tnet_profit_cents\tservice_level_bps\tscore\tskipped\tdecisions")
	for _, r := range res.Runs {
		fmt.Fprintf(tw, "%d\t%d\t%x\t%d\t%d\t%d\t%d\t%d\n", r.ArmIndex, uint64(r.Seed), r.Hash[:8],
			int64(r.Metrics.NetProfit), int64(r.Metrics.ServiceLevel), r.Score.Composite, len(r.Skipped), r.Decisions)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, r := range res.Runs {
		for _, s := range r.Skipped {
			fmt.Fprintf(out, "skipped: arm %d seed %d tick %d %s: %s\n", r.ArmIndex, uint64(r.Seed), s.Tick, s.Kind, s.Reason)
		}
	}
	if len(res.Arms) == 2 {
		var w1, w2, ties int
		n := len(res.Seeds)
		for i := 0; i < n; i++ {
			a, b := res.Runs[i], res.Runs[n+i]
			switch sim.Compare(a.Score, b.Score, tolerance) {
			case sim.ScoreWin:
				w1++
			case sim.ScoreLoss:
				w2++
			default:
				ties++
			}
		}
		fmt.Fprintf(out, "paired: arm 0 wins %d, arm 1 wins %d, ties %d\n", w1, w2, ties)
	}
	fmt.Fprintln(out, sim.Disclaimer)
	if outPrefix == "" {
		return nil
	}
	rep := ensemble.BuildReport(res, tolerance)
	js, err := rep.JSON()
	if err != nil {
		return fail(exitUsageOrFileError, "report json: %v", err)
	}
	cs, err := rep.CSV()
	if err != nil {
		return fail(exitUsageOrFileError, "report csv: %v", err)
	}
	for _, f := range []struct {
		path string
		data []byte
	}{{outPrefix + ".json", js}, {outPrefix + ".csv", cs}} {
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			return fail(exitUsageOrFileError, "write report: %v", err)
		}
		fmt.Fprintf(out, "wrote %s\n", f.path)
	}
	return nil
}
