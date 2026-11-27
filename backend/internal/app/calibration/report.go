package calibration

import (
	"fmt"
	"io"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// DefaultSeed is the calibration default Seed (the demo Seed).
const DefaultSeed sim.Seed = demoplan.DefaultSeed

// WriteEvidence writes, for each Arm, the Ticks of the default-Seed run at
// which the Fulfilment_Constraint_Report lists a stock-bound or capacity-bound
// order (Req 9.7). Ticks are the Tick boundaries at which the report was built.
func WriteEvidence(w io.Writer, sc sim.Scenario, arms []Arm, seed sim.Seed) error {
	fmt.Fprintf(w, "| Arm (Seed %d) | Stock-bound Ticks (first) | Capacity-bound Ticks (first, peak units @ tick) | Max open-order queue |\n|---|---|---|---|\n", uint64(seed))
	for _, a := range arms {
		r, err := Run(sc, seed, a)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "| %s | %s (%d) | %s (%d, %d @ %d) | %d |\n", a.Name,
			Ranges(r.StockBoundTickList), r.Evidence.FirstStockBoundTick,
			Ranges(r.CapBoundTickList), r.Evidence.FirstCapBoundTick, r.Evidence.PeakCapBoundUnits, r.Evidence.PeakCapBoundTick,
			r.Evidence.MaxQueue)
	}
	return nil
}

// WriteReport writes the full calibration report body in Markdown for sc over
// seeds: the arm table, the evidence table, the lever sweeps and the per-Seed
// rows of the three arms the Calibration_Report must show (Req 36.7).
func WriteReport(w io.Writer, sc sim.Scenario, seeds []sim.Seed) error {
	plan, err := demoplan.Derive(sc, DefaultSeed)
	if err != nil {
		return fmt.Errorf("demo plan: %w", err)
	}
	arms := StandardArms(sc, plan)
	all, err := RunAll(sc, seeds, arms)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Crisis_Tick %d, Demo_Fork_Tick %d, Seeds %d..%d (%d seeds). Figures are means over the Seeds; min..max in brackets.\n\n", plan.CrisisTick, plan.ForkTick, uint64(seeds[0]), uint64(seeds[len(seeds)-1]), len(seeds))
	fmt.Fprintln(w, "### Arms")
	fmt.Fprintln(w)
	WriteTable(w, all)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "### Evidence at the default Seed")
	fmt.Fprintln(w)
	if err := WriteEvidence(w, sc, arms, DefaultSeed); err != nil {
		return err
	}
	fmt.Fprintln(w)
	sweeps := SweepArms(sc, plan)
	sall, err := RunAll(sc, seeds, sweeps)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "### Lever sweeps (restocked warehouse)")
	fmt.Fprintln(w)
	WriteTable(w, sall)
	fmt.Fprintln(w)
	for _, i := range []int{0, 1, 2} {
		fmt.Fprintf(w, "### Per-Seed results: %s\n\n", arms[i].Name)
		WriteSeedTable(w, all[i])
		fmt.Fprintln(w)
	}
	return nil
}
