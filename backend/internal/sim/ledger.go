package sim

import (
	"errors"
	"fmt"
	"math"
)

// The fixed chart of accounts (Req 10.1, 37.1). The numeric values are part of
// the canonical state encoding and must never change or be reordered.
const (
	AccountNone                 AccountID = 0 // invalid; never posted
	AccountCash                 AccountID = 1
	AccountInventory            AccountID = 2
	AccountOpeningEquity        AccountID = 3
	AccountRevenue              AccountID = 4
	AccountCostOfGoodsSold      AccountID = 5
	AccountLaborExpense         AccountID = 6
	AccountFreightExpense       AccountID = 7
	AccountMarketingExpense     AccountID = 8
	AccountOverheadExpense      AccountID = 9
	AccountServiceCreditExpense AccountID = 10

	// NumAccounts is the number of accounts in the chart.
	NumAccounts = 10
)

// Side is the normal-balance side of an account.
type Side uint8

// Normal-balance sides.
const (
	SideDebit Side = iota
	SideCredit
)

// Cause kinds for LedgerEntry.Cause. CauseRef carries the related identifier
// (order ID, purchase-order ID, SKU index, tick); see DescribeCause.
const (
	CauseOpening       uint8 = 1 // opening balances; CauseRef 0
	CauseShipment      uint8 = 2 // revenue and COGS at shipment; CauseRef = order ID
	CauseReceipt       uint8 = 3 // purchase receipt paid in cash; CauseRef = purchase-order ID
	CauseFreight       uint8 = 4 // freight on a purchase order; CauseRef = purchase-order ID
	CauseLabor         uint8 = 5 // wages for the tick; CauseRef = tick
	CauseMarketing     uint8 = 6 // marketing spend for the tick; CauseRef = tick
	CauseOverhead      uint8 = 7 // overhead for the tick; CauseRef = tick
	CauseServiceCredit uint8 = 8 // late-shipment service credit; CauseRef = order ID
)

// Ledger posting errors.
var (
	ErrEntryTooFewLines   = errors.New("sim: ledger entry needs at least two lines")
	ErrUnknownAccount     = errors.New("sim: unknown ledger account")
	ErrNegativeAmount     = errors.New("sim: negative ledger amount")
	ErrInvalidLine        = errors.New("sim: ledger line must have exactly one non-zero side")
	ErrUnbalancedEntry    = errors.New("sim: ledger entry debits do not equal credits")
	ErrAmountOverflow     = errors.New("sim: ledger amount overflow")
	ErrNoOpeningPosition  = errors.New("sim: opening entry has no non-zero balance")
	ErrNegativeOpeningVal = errors.New("sim: negative opening cash, units or unit cost")
)

var accountNames = [NumAccounts + 1]string{
	"",
	"Cash",
	"Inventory",
	"Opening_Equity",
	"Revenue",
	"Cost_Of_Goods_Sold",
	"Labor_Expense",
	"Freight_Expense",
	"Marketing_Expense",
	"Overhead_Expense",
	"Service_Credit_Expense",
}

// Accounts returns the chart of accounts in canonical order.
func Accounts() []AccountID {
	out := make([]AccountID, NumAccounts)
	for i := range out {
		out[i] = AccountID(i + 1)
	}
	return out
}

// Valid reports whether a is one of the 10 accounts.
func (a AccountID) Valid() bool { return a >= 1 && a <= NumAccounts }

// String returns the account name, for example "Cost_Of_Goods_Sold".
func (a AccountID) String() string {
	if !a.Valid() {
		return fmt.Sprintf("Account(%d)", uint8(a))
	}
	return accountNames[a]
}

// NormalSide returns the side on which the account normally carries a
// positive balance. Cash, Inventory, COGS and all expenses are debit-normal;
// Opening_Equity and Revenue are credit-normal.
func (a AccountID) NormalSide() Side {
	switch a {
	case AccountOpeningEquity, AccountRevenue:
		return SideCredit
	}
	return SideDebit
}

// Validate checks the posting rules without mutating anything: at least two
// lines, known accounts, non-negative amounts, exactly one non-zero side per
// line, and total debits equal total credits.
func (e LedgerEntry) Validate() error {
	if len(e.Lines) < 2 {
		return ErrEntryTooFewLines
	}
	var dr, cr uint64
	for i, l := range e.Lines {
		if !l.Account.Valid() {
			return fmt.Errorf("line %d: %w: %d", i, ErrUnknownAccount, uint8(l.Account))
		}
		if l.Debit < 0 || l.Credit < 0 {
			return fmt.Errorf("line %d: %w", i, ErrNegativeAmount)
		}
		if (l.Debit == 0) == (l.Credit == 0) {
			return fmt.Errorf("line %d: %w", i, ErrInvalidLine)
		}
		var ok bool
		if dr, ok = addU64(dr, uint64(l.Debit)); !ok {
			return ErrAmountOverflow
		}
		if cr, ok = addU64(cr, uint64(l.Credit)); !ok {
			return ErrAmountOverflow
		}
	}
	if dr > math.MaxInt64 || cr > math.MaxInt64 {
		return ErrAmountOverflow
	}
	if dr != cr {
		return fmt.Errorf("%w: debits %d, credits %d", ErrUnbalancedEntry, dr, cr)
	}
	return nil
}

func addU64(a, b uint64) (uint64, bool) {
	s := a + b
	return s, s >= a
}

// Post validates e and appends it to the ledger. It is the only package API
// that adds entries, so an invalid or unbalanced entry is never recorded; on
// error the ledger is unchanged. The lines are copied.
func (l *Ledger) Post(e LedgerEntry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	l.appendEntry(e)
	return nil
}

// appendEntry is the unexported append; callers must have validated e.
func (l *Ledger) appendEntry(e LedgerEntry) {
	e.Lines = append([]LedgerLine(nil), e.Lines...)
	l.Entries = append(l.Entries, e)
}

// Balances returns the running balance of every account computed from the
// posted entries, indexed by AccountID (index 0 is unused). Each balance is
// signed relative to the account's normal side, so a normal balance is
// positive.
func (l Ledger) Balances() [NumAccounts + 1]Cents {
	var net [NumAccounts + 1]Cents // debit minus credit
	for _, e := range l.Entries {
		for _, ln := range e.Lines {
			if ln.Account.Valid() {
				net[ln.Account] += ln.Debit - ln.Credit
			}
		}
	}
	for a := AccountID(1); a <= NumAccounts; a++ {
		if a.NormalSide() == SideCredit {
			net[a] = -net[a]
		}
	}
	return net
}

// Balance returns the balance of one account, signed relative to its normal
// side. It returns 0 for an unknown account.
func (l Ledger) Balance(a AccountID) Cents {
	if !a.Valid() {
		return 0
	}
	return l.Balances()[a]
}

// Totals returns the sum of all debits and all credits across the ledger.
// They are equal for any ledger built through Post (INV-6).
func (l Ledger) Totals() (debits, credits Cents) {
	for _, e := range l.Entries {
		for _, ln := range e.Lines {
			debits += ln.Debit
			credits += ln.Credit
		}
	}
	return debits, credits
}

// OpeningInventoryValue returns the sum over SKUs of opening units times
// opening unit cost, in cents.
func OpeningInventoryValue(skus []SKUSpec) (Cents, error) {
	var total int64
	for _, s := range skus {
		if s.OpeningUnits < 0 || s.OpeningUnitCostCents < 0 {
			return 0, ErrNegativeOpeningVal
		}
		if s.OpeningUnits != 0 && int64(s.OpeningUnitCostCents) > math.MaxInt64/s.OpeningUnits {
			return 0, ErrAmountOverflow
		}
		v := s.OpeningUnits * int64(s.OpeningUnitCostCents)
		if total > math.MaxInt64-v {
			return 0, ErrAmountOverflow
		}
		total += v
	}
	return Cents(total), nil
}

// OpeningEntry builds the Tick 0 entry: debit Cash by openingCash and
// Inventory by the opening inventory value, credit Opening_Equity by the sum.
// A zero-valued debit line is omitted.
func OpeningEntry(openingCash Cents, skus []SKUSpec) (LedgerEntry, error) {
	if openingCash < 0 {
		return LedgerEntry{}, ErrNegativeOpeningVal
	}
	inv, err := OpeningInventoryValue(skus)
	if err != nil {
		return LedgerEntry{}, err
	}
	if int64(openingCash) > math.MaxInt64-int64(inv) {
		return LedgerEntry{}, ErrAmountOverflow
	}
	total := openingCash + inv
	if total == 0 {
		return LedgerEntry{}, ErrNoOpeningPosition
	}
	var lines []LedgerLine
	if openingCash > 0 {
		lines = append(lines, LedgerLine{Account: AccountCash, Debit: openingCash})
	}
	if inv > 0 {
		lines = append(lines, LedgerLine{Account: AccountInventory, Debit: inv})
	}
	lines = append(lines, LedgerLine{Account: AccountOpeningEquity, Credit: total})
	return LedgerEntry{Tick: 0, At: 0, Cause: CauseOpening, CauseRef: 0, Lines: lines}, nil
}

// NewOpeningLedger returns a ledger containing only the opening entry, with
// OpeningCash and OpeningEquity set from it.
func NewOpeningLedger(openingCash Cents, skus []SKUSpec) (Ledger, error) {
	e, err := OpeningEntry(openingCash, skus)
	if err != nil {
		return Ledger{}, err
	}
	var l Ledger
	if err := l.Post(e); err != nil {
		return Ledger{}, err
	}
	l.OpeningCash = openingCash
	l.OpeningEquity = e.Lines[len(e.Lines)-1].Credit
	return l, nil
}

// DescribeCause renders a human-readable description for an entry from its
// cause kind and reference. Descriptions are derived, never stored or hashed.
func DescribeCause(cause uint8, ref int64) string {
	switch cause {
	case CauseOpening:
		return "Opening balances"
	case CauseShipment:
		return fmt.Sprintf("Shipment of order %d: revenue and cost of goods sold", ref)
	case CauseReceipt:
		return fmt.Sprintf("Receipt of purchase order %d", ref)
	case CauseFreight:
		return fmt.Sprintf("Freight for purchase order %d", ref)
	case CauseLabor:
		return fmt.Sprintf("Wages for tick %d", ref)
	case CauseMarketing:
		return fmt.Sprintf("Marketing spend for tick %d", ref)
	case CauseOverhead:
		return fmt.Sprintf("Overhead for tick %d", ref)
	case CauseServiceCredit:
		return fmt.Sprintf("Service credit for late order %d", ref)
	}
	return fmt.Sprintf("Unknown cause %d (ref %d)", cause, ref)
}

// Description is DescribeCause applied to the entry's own cause fields.
func (e LedgerEntry) Description() string { return DescribeCause(e.Cause, e.CauseRef) }
