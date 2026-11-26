package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// crSums walks the raw entries once and returns, independently of the
// Metrics_Calculator, the sum of Cash lines (debit minus credit) over every
// entry and over every entry except the opening entry.
func crSums(l sim.Ledger) (all, excludingOpening sim.Cents) {
	for _, e := range l.Entries {
		for _, ln := range e.Lines {
			if ln.Account != sim.AccountCash {
				continue
			}
			d := ln.Debit - ln.Credit
			all += d
			if e.Cause != sim.CauseOpening {
				excludingOpening += d
			}
		}
	}
	return all, excludingOpening
}

// Feature: parallel-simulation-mvp, Property 16: Cash reconciliation
// **Validates: Requirements 10.15**
func TestPropCashReconciliation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(simtest.MinHorizon).Draw(t, "case")
		run, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		for i, st := range run.States {
			all, flow := crSums(st.Ledger)
			m := sim.ComputeMetrics(*c.Scenario, st)
			if want := st.Ledger.OpeningCash + flow; all != want {
				t.Fatalf("tick %d: sum of Cash lines %d != OpeningCash %d + flow %d", i, all, st.Ledger.OpeningCash, flow)
			}
			if got := st.Ledger.Balance(sim.AccountCash); got != all {
				t.Fatalf("tick %d: Balance(Cash) %d != raw Cash sum %d", i, got, all)
			}
			if m.Cash != all {
				t.Fatalf("tick %d: Metrics.Cash %d != raw Cash sum %d", i, m.Cash, all)
			}
			if m.NetCashFlow != flow {
				t.Fatalf("tick %d: Metrics.NetCashFlow %d != raw flow %d", i, m.NetCashFlow, flow)
			}
			if m.Cash != st.Ledger.OpeningCash+m.NetCashFlow {
				t.Fatalf("tick %d: Cash %d != OpeningCash %d + NetCashFlow %d", i, m.Cash, st.Ledger.OpeningCash, m.NetCashFlow)
			}
		}
	})
}
