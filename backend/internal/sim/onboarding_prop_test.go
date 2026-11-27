package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// onbRun holds, per Tick (index = Tick), the total Labor_Expense debits and
// the fulfilment capacity of one run.
type onbRun struct {
	labor    map[sim.Tick]sim.Cents
	capacity map[sim.Tick]int64
}

func onbDrive(t *rapid.T, sc *sim.Scenario, seed sim.Seed, inputs []sim.Input) onbRun {
	st, err := sim.Init(*sc, seed)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	byTick := map[sim.Tick][]sim.Input{}
	for _, in := range inputs {
		byTick[in.EffectiveTick] = append(byTick[in.EffectiveTick], in)
	}
	r := onbRun{labor: map[sim.Tick]sim.Cents{}, capacity: map[sim.Tick]int64{}}
	for int64(st.Tick()) < sc.HorizonTicks {
		step, err := sim.Advance(*sc, st, byTick[st.Tick()+1])
		if err != nil {
			t.Fatalf("advance to %d: %v", st.Tick()+1, err)
		}
		st = step.State
		r.capacity[step.Report.Tick] = step.Report.Capacity
	}
	for _, e := range st.Ledger.Entries {
		for _, ln := range e.Lines {
			if ln.Account == sim.AccountLaborExpense {
				r.labor[e.Tick] += ln.Debit - ln.Credit
			}
		}
	}
	return r
}

// Feature: parallel-simulation-mvp, Property 40: Temp worker onboarding wages and zero capacity
// **Validates: Requirements 9.8, 37.14**
func TestPropTempOnboarding(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sc := simtest.Scenarios(simtest.ShortHorizon).Draw(t, "scenario")
		seed := simtest.Seeds().Draw(t, "seed")

		// Hire proposals at random decision windows, validator gated by
		// simtest.BuildInputLog (only accepted commands enter the log).
		props := simtest.Proposals{}
		maxHire := sc.Workforce.MaxTempWorkers
		if maxHire > 6 {
			maxHire = 6
		}
		for tick := sim.Tick(0); int64(tick) < sc.HorizonTicks; tick++ {
			if open, _ := sim.DecisionWindowOpen(*sc, tick); !open {
				continue
			}
			if !rapid.Bool().Draw(t, "hireHere") {
				continue
			}
			props[tick] = append(props[tick], sim.Command{
				Kind: sim.CmdHireTempWorkers,
				Qty:  rapid.Int64Range(1, maxHire).Draw(t, "count"),
			})
		}
		log, err := simtest.BuildInputLog(sc, seed, props)
		if err != nil {
			t.Fatalf("build input log: %v", err)
		}

		base := onbDrive(t, sc, seed, nil)
		with := onbDrive(t, sc, seed, log)

		wage := sc.Wages.TempPerHourCents
		perWorker := sc.Workforce.UnitsPerWorkerHour
		onb := sc.Workforce.TempOnboardingTicks

		for tick := sim.Tick(1); int64(tick) <= sc.HorizonTicks; tick++ {
			var wantWages sim.Cents
			var wantCap int64
			for _, in := range log {
				if in.Kind != sim.InputCommand || in.Command.Kind != sim.CmdHireTempWorkers {
					continue
				}
				n := in.Command.Qty
				// Paid from the effective Tick, including onboarding.
				if tick >= in.EffectiveTick {
					wantWages += sim.Cents(n) * wage
				}
				// Zero capacity before ReadyAt, n * unitsPerWorkerHour from it.
				if int64(tick) >= int64(in.EffectiveTick)+onb {
					wantCap += n * perWorker
				}
			}
			if got := with.labor[tick] - base.labor[tick]; got != wantWages {
				t.Fatalf("tick %d: extra Labor_Expense %d, want %d (log %+v)", tick, got, wantWages, log)
			}
			if got := with.capacity[tick] - base.capacity[tick]; got != wantCap {
				t.Fatalf("tick %d: extra capacity %d, want %d (onboarding %d ticks, log %+v)", tick, got, wantCap, onb, log)
			}
		}
	})
}
