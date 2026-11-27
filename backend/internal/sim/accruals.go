package sim

import "fmt"

// laborAccrual posts the Tick's wages, paid in Cash in the Tick they are
// earned (Req 10.8, 37.12 to 37.14, 37.20). Regular, overtime and temporary
// wages are separate Labor_Expense debit lines of one balanced entry; a
// component that is zero is omitted because a ledger line must be non-zero.
//
//	regular:  Regular workers * regular hourly wage
//	overtime: Regular workers * overtime hours per worker * overtime hourly wage
//	temp:     contracted temps (HiredAt <= at, including onboarding) * temp wage
func (e *stepper) laborAccrual(at SimMinute) error {
	// Overtime and temp wages are real since schedule_overtime and
	// hire_temp_workers exist (Req 37.13, 37.14); cohorts are paid from HiredAt,
	// including onboarding, while capacity starts at ReadyAt.
	w := e.st.Workforce
	wages := e.sc.Wages
	regular := Cents(w.Regular) * wages.RegularPerHourCents
	overtime := Cents(w.Regular*w.OvertimeHours) * wages.OvertimePerHourCents
	var temps int64
	for _, c := range w.Temps {
		if c.HiredAt <= at {
			temps += c.Count
		}
	}
	temp := Cents(temps) * wages.TempPerHourCents

	var lines []LedgerLine
	var total Cents
	for _, amt := range []Cents{regular, overtime, temp} {
		if amt > 0 {
			lines = append(lines, LedgerLine{Account: AccountLaborExpense, Debit: amt})
			total += amt
		}
	}
	return e.postAccrual(at, CauseLabor, lines, total)
}

// marketingAccrual posts the sum of State.Prices[i].MarketingCents as
// Marketing_Expense against Cash (Req 10.9).
func (e *stepper) marketingAccrual(at SimMinute) error {
	var total Cents
	for _, p := range e.st.Prices {
		total += p.MarketingCents
	}
	var lines []LedgerLine
	if total > 0 {
		lines = append(lines, LedgerLine{Account: AccountMarketingExpense, Debit: total})
	}
	return e.postAccrual(at, CauseMarketing, lines, total)
}

// overheadAccrual posts the Scenario's hourly overhead as Overhead_Expense
// against Cash (Req 10.10).
func (e *stepper) overheadAccrual(at SimMinute) error {
	total := e.sc.Costs.HourlyOverheadCents
	var lines []LedgerLine
	if total > 0 {
		lines = append(lines, LedgerLine{Account: AccountOverheadExpense, Debit: total})
	}
	return e.postAccrual(at, CauseOverhead, lines, total)
}

// postAccrual appends the Cash credit to the expense debit lines and posts the
// entry. Nothing is posted when total is zero. CauseRef is the Tick.
func (e *stepper) postAccrual(at SimMinute, cause uint8, lines []LedgerLine, total Cents) error {
	if total <= 0 {
		return nil
	}
	lines = append(lines, LedgerLine{Account: AccountCash, Credit: total})
	entry := LedgerEntry{Tick: TickOf(at), At: at, Cause: cause, CauseRef: int64(TickOf(at)), Lines: lines}
	if err := e.st.Ledger.Post(entry); err != nil {
		return fmt.Errorf("accrual posting at minute %d: %w", at, err)
	}
	return nil
}
