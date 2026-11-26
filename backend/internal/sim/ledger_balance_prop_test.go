package sim_test

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/simtest"
)

// lbViolation returns a description of the first ledger-balance violation in
// l, or "" when the ledger is consistent.
func lbViolation(l sim.Ledger) string {
	var dr, cr sim.Cents
	var net [sim.NumAccounts + 1]sim.Cents // debit minus credit, recomputed here
	for i, e := range l.Entries {
		if len(e.Lines) < 2 {
			return fmt.Sprintf("entry %d has %d lines", i, len(e.Lines))
		}
		if i > 0 && e.Tick < l.Entries[i-1].Tick {
			return fmt.Sprintf("entry %d tick %d precedes entry %d tick %d", i, e.Tick, i-1, l.Entries[i-1].Tick)
		}
		var edr, ecr sim.Cents
		for j, ln := range e.Lines {
			if !ln.Account.Valid() {
				return fmt.Sprintf("entry %d line %d: invalid account %d", i, j, uint8(ln.Account))
			}
			if ln.Debit < 0 || ln.Credit < 0 {
				return fmt.Sprintf("entry %d line %d: negative amount", i, j)
			}
			edr += ln.Debit
			ecr += ln.Credit
			net[ln.Account] += ln.Debit - ln.Credit
		}
		if edr != ecr {
			return fmt.Sprintf("entry %d unbalanced: debits %d, credits %d", i, edr, ecr)
		}
		dr += edr
		cr += ecr
	}
	if td, tc := l.Totals(); td != tc || td != dr || tc != cr {
		return fmt.Sprintf("Totals debits %d credits %d; recomputed %d/%d", td, tc, dr, cr)
	}
	// Account balances computed from entries are consistent: they match the
	// ledger's own balances and satisfy the accounting identity.
	bal := l.Balances()
	var debitNormal, creditNormal sim.Cents
	for _, a := range sim.Accounts() {
		want := net[a]
		if a.NormalSide() == sim.SideCredit {
			want = -want
			creditNormal += bal[a]
		} else {
			debitNormal += bal[a]
		}
		if bal[a] != want || l.Balance(a) != want {
			return fmt.Sprintf("account %v balance %d, want %d", a, bal[a], want)
		}
	}
	if debitNormal != creditNormal {
		return fmt.Sprintf("debit-normal balances %d != credit-normal balances %d", debitNormal, creditNormal)
	}
	return ""
}

// Feature: parallel-simulation-mvp, Property 15: Ledger balance
// **Validates: Requirements 10.14**
func TestPropLedgerBalance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := simtest.Cases(simtest.MinHorizon).Draw(t, "case")
		run, err := simtest.RunToHorizon(c.Scenario, c.Seed, c.Inputs)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		for i, st := range run.States {
			if msg := lbViolation(st.Ledger); msg != "" {
				t.Fatalf("tick %d: %s", i, msg)
			}
		}
	})
}
