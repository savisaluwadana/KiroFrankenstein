package sim

import (
	"errors"
	"fmt"
)

// Workforce command errors returned when the engine applies a staffing Command
// that did not pass Validate.
var (
	ErrOvertimeLimit = errors.New("sim: overtime hours exceed the per-worker maximum")
	ErrTempLimit     = errors.New("sim: temporary workers exceed the maximum")
	ErrBadStaffing   = errors.New("sim: staffing quantity is invalid")
)

// ScheduleOvertime sets the overtime hours per regular worker for every Tick
// from the next one on (Req 9.6). It replaces the previous schedule, so hours
// of 0 cancels overtime. The hours must lie in 0..Workforce.MaxOvertimePerWorker.
// Overtime adds capacity (see fulfilmentCapacity) and is paid as Labor_Expense
// overtime wages in each Tick it is scheduled (laborAccrual).
func ScheduleOvertime(sc *Scenario, st *State, hours int64) error {
	if hours < 0 {
		return fmt.Errorf("%w: %d hours", ErrBadStaffing, hours)
	}
	if hours > sc.Workforce.MaxOvertimePerWorker {
		return fmt.Errorf("%w: %d > %d", ErrOvertimeLimit, hours, sc.Workforce.MaxOvertimePerWorker)
	}
	st.Workforce.OvertimeHours = hours
	return nil
}

// TempWorkerTotal returns every temporary worker contracted so far, including
// those still in onboarding.
func (w Workforce) TempWorkerTotal() int64 {
	var n int64
	for _, c := range w.Temps {
		n += c.Count
	}
	return n
}

// HireTempWorkers adds a cohort of count temporary workers hired at sim minute
// at (Req 9.5, 9.8, 9.9). The cohort is paid from HiredAt, including the
// onboarding delay, and adds fulfilment capacity only from
// ReadyAt = HiredAt + tempOnboardingTicks.
func HireTempWorkers(sc *Scenario, st *State, count int64, at SimMinute) error {
	if count < 1 {
		return fmt.Errorf("%w: %d workers", ErrBadStaffing, count)
	}
	if st.Workforce.TempWorkerTotal()+count > sc.Workforce.MaxTempWorkers {
		return fmt.Errorf("%w: %d + %d > %d", ErrTempLimit, st.Workforce.TempWorkerTotal(), count, sc.Workforce.MaxTempWorkers)
	}
	st.Workforce.Temps = append(st.Workforce.Temps, TempCohort{
		Count:   count,
		HiredAt: at,
		ReadyAt: at + SimMinute(sc.Workforce.TempOnboardingTicks*MinutesPerTick),
	})
	return nil
}

// overtimeFirstTickCents is the overtime wage bill of one Tick for hours per
// worker, used by the validator to check cash.
func overtimeFirstTickCents(sc Scenario, regular, hours int64) int64 {
	return regular * hours * int64(sc.Wages.OvertimePerHourCents)
}

// validateOvertime checks a schedule_overtime Command.
func validateOvertime(sc Scenario, obs Observation, c Command, available int64) (RejectCode, string, int64) {
	max := sc.Workforce.MaxOvertimePerWorker
	if c.Qty < 0 {
		return ReasonInvalidQuantity, fmt.Sprintf("overtime hours %d is below 0", c.Qty), 0
	}
	if c.Qty > max {
		return ReasonOvertimeExceedsLimit, fmt.Sprintf("overtime of %d hours per worker is above the limit of %d", c.Qty, max), 0
	}
	cost := overtimeFirstTickCents(sc, obs.Workforce.Regular, c.Qty)
	if cost > available {
		return ReasonInsufficientCash, fmt.Sprintf("first hour of overtime wages is %d cents but %d cents are available", cost, available), 0
	}
	return "", "", cost
}

// validateHireTemps checks a hire_temp_workers Command against the temporary
// workers already contracted (Observation) and those hired earlier in the batch
// (pendingTemps), and that the first Tick's wages are affordable.
func validateHireTemps(sc Scenario, obs Observation, c Command, available, pendingTemps int64) (RejectCode, string, int64) {
	max := sc.Workforce.MaxTempWorkers
	if c.Qty < 1 || c.Qty > max {
		if c.Qty > max {
			return ReasonTooManyTempWorkers, fmt.Sprintf("hiring %d temporary workers is above the limit of %d", c.Qty, max), 0
		}
		return ReasonInvalidQuantity, fmt.Sprintf("worker count %d is below 1", c.Qty), 0
	}
	have := obs.Workforce.TempsOnShift + obs.Workforce.TempsOnboarding + pendingTemps
	if have+c.Qty > max {
		return ReasonTooManyTempWorkers, fmt.Sprintf("%d temporary workers plus %d more is above the limit of %d", have, c.Qty, max), 0
	}
	cost := c.Qty * int64(sc.Wages.TempPerHourCents)
	if cost > available {
		return ReasonInsufficientCash, fmt.Sprintf("first hour of wages is %d cents but %d cents are available", cost, available), 0
	}
	return "", "", cost
}
