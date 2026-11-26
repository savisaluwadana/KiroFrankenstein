package sim

import (
	"errors"
	"testing"
)

func TestLedgerOpeningEntry(t *testing.T) {
	skus := []SKUSpec{
		{ID: "a", OpeningUnits: 100, OpeningUnitCostCents: 250},
		{ID: "b", OpeningUnits: 10, OpeningUnitCostCents: 1000},
	}
	l, err := NewOpeningLedger(1000000, skus)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 1 {
		t.Fatalf("entries = %d", len(l.Entries))
	}
	b := l.Balances()
	if b[AccountCash] != 1000000 || b[AccountInventory] != 35000 || b[AccountOpeningEquity] != 1035000 {
		t.Fatalf("balances %v", b)
	}
	if l.OpeningCash != 1000000 || l.OpeningEquity != 1035000 {
		t.Fatalf("opening fields %d %d", l.OpeningCash, l.OpeningEquity)
	}
	d, c := l.Totals()
	if d != c {
		t.Fatalf("totals %d %d", d, c)
	}
}

func TestLedgerOpeningErrors(t *testing.T) {
	if _, err := NewOpeningLedger(0, nil); !errors.Is(err, ErrNoOpeningPosition) {
		t.Fatalf("got %v", err)
	}
	if _, err := NewOpeningLedger(-1, nil); !errors.Is(err, ErrNegativeOpeningVal) {
		t.Fatalf("got %v", err)
	}
	if _, err := OpeningInventoryValue([]SKUSpec{{OpeningUnits: 1 << 62, OpeningUnitCostCents: 4}}); !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("got %v", err)
	}
}

func TestLedgerPostRejects(t *testing.T) {
	cases := []struct {
		name  string
		lines []LedgerLine
		want  error
	}{
		{"no lines", nil, ErrEntryTooFewLines},
		{"one line", []LedgerLine{{Account: AccountCash, Debit: 5}}, ErrEntryTooFewLines},
		{"unbalanced", []LedgerLine{{Account: AccountCash, Debit: 5}, {Account: AccountRevenue, Credit: 4}}, ErrUnbalancedEntry},
		{"negative", []LedgerLine{{Account: AccountCash, Debit: -5}, {Account: AccountRevenue, Credit: 5}}, ErrNegativeAmount},
		{"unknown", []LedgerLine{{Account: 11, Debit: 5}, {Account: AccountRevenue, Credit: 5}}, ErrUnknownAccount},
		{"none account", []LedgerLine{{Account: AccountNone, Debit: 5}, {Account: AccountRevenue, Credit: 5}}, ErrUnknownAccount},
		{"both sides", []LedgerLine{{Account: AccountCash, Debit: 5, Credit: 5}, {Account: AccountRevenue, Credit: 5}}, ErrInvalidLine},
		{"zero line", []LedgerLine{{Account: AccountCash}, {Account: AccountRevenue, Credit: 0}}, ErrInvalidLine},
		{"overflow", []LedgerLine{
			{Account: AccountCash, Debit: 1 << 62}, {Account: AccountInventory, Debit: 1 << 62},
			{Account: AccountRevenue, Credit: 1 << 62}, {Account: AccountOpeningEquity, Credit: 1 << 62}}, ErrAmountOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var l Ledger
			err := l.Post(LedgerEntry{Cause: CauseShipment, Lines: tc.lines})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if len(l.Entries) != 0 {
				t.Fatal("entry appended despite error")
			}
		})
	}
}

func TestLedgerBalancesAndSides(t *testing.T) {
	l, err := NewOpeningLedger(1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := []LedgerLine{
		{Account: AccountCash, Debit: 300}, {Account: AccountRevenue, Credit: 300},
		{Account: AccountCostOfGoodsSold, Debit: 120}, {Account: AccountInventory, Credit: 120},
	}
	// Inventory goes negative here only because the test opens with none.
	if err := l.Post(LedgerEntry{Tick: 1, At: 60, Cause: CauseShipment, CauseRef: 7, Lines: lines}); err != nil {
		t.Fatal(err)
	}
	lines[0].Debit = 999 // posted entry must not alias the caller's slice
	b := l.Balances()
	if b[AccountCash] != 1300 || b[AccountRevenue] != 300 || b[AccountCostOfGoodsSold] != 120 || b[AccountInventory] != -120 {
		t.Fatalf("balances %v", b)
	}
	if l.Balance(AccountCash) != 1300 || l.Balance(0) != 0 || l.Balance(99) != 0 {
		t.Fatal("Balance accessor")
	}
	if AccountRevenue.NormalSide() != SideCredit || AccountOpeningEquity.NormalSide() != SideCredit ||
		AccountCash.NormalSide() != SideDebit || AccountServiceCreditExpense.NormalSide() != SideDebit {
		t.Fatal("normal sides")
	}
}

func TestLedgerChartOfAccounts(t *testing.T) {
	want := []string{"Cash", "Inventory", "Opening_Equity", "Revenue", "Cost_Of_Goods_Sold",
		"Labor_Expense", "Freight_Expense", "Marketing_Expense", "Overhead_Expense", "Service_Credit_Expense"}
	accts := Accounts()
	if len(accts) != len(want) {
		t.Fatalf("len %d", len(accts))
	}
	for i, a := range accts {
		if a.String() != want[i] || !a.Valid() || uint8(a) != uint8(i+1) {
			t.Fatalf("account %d = %v", i, a)
		}
	}
	if AccountNone.Valid() || AccountID(11).Valid() {
		t.Fatal("invalid accounts reported valid")
	}
}

func TestLedgerDescribeCause(t *testing.T) {
	cases := map[uint8]string{
		CauseOpening:       "Opening balances",
		CauseShipment:      "Shipment of order 9: revenue and cost of goods sold",
		CauseReceipt:       "Receipt of purchase order 9",
		CauseFreight:       "Freight for purchase order 9",
		CauseLabor:         "Wages for tick 9",
		CauseMarketing:     "Marketing spend for tick 9",
		CauseOverhead:      "Overhead for tick 9",
		CauseServiceCredit: "Service credit for late order 9",
		200:                "Unknown cause 200 (ref 9)",
	}
	for c, want := range cases {
		if got := DescribeCause(c, 9); got != want {
			t.Errorf("cause %d: %q, want %q", c, got, want)
		}
	}
	if (LedgerEntry{Cause: CauseLabor, CauseRef: 3}).Description() != "Wages for tick 3" {
		t.Fatal("Description")
	}
}
