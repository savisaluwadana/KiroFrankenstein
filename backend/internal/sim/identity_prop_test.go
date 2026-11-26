package sim_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// biRaw holds account positions summed from raw ledger lines, independently
// of Ledger.Balance and the Metrics_Calculator. Debit-normal accounts are
// debit minus credit; credit-normal accounts are credit minus debit.
type biRaw struct {
	cash, inventory, openingEquity                           sim.Cents
	revenue, cogs, freight, labor, marketing, overhead, cred sim.Cents
}

func biSum(l sim.Ledger) biRaw {
	var r biRaw
	for _, e := range l.Entries {
		for _, ln := range e.Lines {
			d := ln.Debit - ln.Credit
			switch ln.Account {
			case sim.AccountCash:
				r.cash += d
			case sim.AccountInventory:
				r.inventory += d
			case sim.AccountOpeningEquity:
				r.openingEquity -= d
			case sim.AccountRevenue:
				r.revenue -= d
			case sim.AccountCostOfGoodsSold:
				r.cogs += d
			case sim.AccountFreightExpense:
				r.freight += d
			case sim.AccountLaborExpense:
				r.labor += d
			case sim.AccountMarketingExpense:
				r.marketing += d
			case sim.AccountOverheadExpense:
				r.overhead += d
			case sim.AccountServiceCreditExpense:
				r.cred += d
			}
		}
	}
	return r
}

// Feature: parallel-simulation-mvp, Property 20: Balance identity
// **Validates: Requirements 37.21**
func TestPropBalanceIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(simtest.MinHorizon).Draw(t, "case")
		run, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		for i, st := range run.States {
			r := biSum(st.Ledger)
			m := sim.ComputeMetrics(*c.Scenario, st)
			rawProfit := r.revenue - r.cogs - r.freight - r.labor - r.marketing - r.overhead - r.cred
			if m.NetProfit != rawProfit {
				t.Fatalf("tick %d: Metrics.NetProfit %d != raw profit %d", i, m.NetProfit, rawProfit)
			}
			if m.Cash != r.cash || m.InventoryValue != r.inventory || m.Reconciliation.OpeningEquity != r.openingEquity {
				t.Fatalf("tick %d: metrics cash/inventory/equity (%d,%d,%d) != raw (%d,%d,%d)", i,
					m.Cash, m.InventoryValue, m.Reconciliation.OpeningEquity, r.cash, r.inventory, r.openingEquity)
			}
			if lhs, rhs := r.cash+r.inventory, r.openingEquity+rawProfit; lhs != rhs {
				t.Fatalf("tick %d: raw Cash %d + Inventory %d = %d != Opening_Equity %d + Net_Profit %d = %d",
					i, r.cash, r.inventory, lhs, r.openingEquity, rawProfit, rhs)
			}
			if lhs, rhs := m.Cash+m.InventoryValue, m.Reconciliation.OpeningEquity+m.NetProfit; lhs != rhs {
				t.Fatalf("tick %d: metrics Cash+Inventory %d != Opening_Equity+Net_Profit %d", i, lhs, rhs)
			}
			if !m.Reconciliation.BalanceIdentityHolds {
				t.Fatalf("tick %d: Reconciliation.BalanceIdentityHolds is false", i)
			}
			if !m.Reconciliation.AccrualToCashHolds {
				t.Fatalf("tick %d: Reconciliation.AccrualToCashHolds is false", i)
			}
		}
		last := run.States[len(run.States)-1]
		eoh := sim.EndOfHorizonStatement(*c.Scenario, last)
		if !eoh.BalanceIdentityHolds {
			t.Fatalf("end of horizon (tick %d): BalanceIdentityHolds is false", eoh.Tick)
		}
	})
}
