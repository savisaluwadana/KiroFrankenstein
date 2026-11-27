package sim

import "testing"

func workforceStep(t *testing.T, sc Scenario, st State, cmds ...Command) State {
	t.Helper()
	var ins []Input
	for i, c := range cmds {
		ins = append(ins, Input{EffectiveTick: st.Tick() + 1, Seq: uint64(i + 1), Kind: InputCommand, Command: c})
	}
	next, _, _, err := StepTick(sc, st, ins)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// laborDebits returns the labor lines of the Tick's labor entry in order.
func laborDebits(st State, tick Tick) []Cents {
	var out []Cents
	for _, en := range st.Ledger.Entries {
		if en.Cause == CauseLabor && en.Tick == tick {
			for _, l := range en.Lines {
				if l.Account == AccountLaborExpense {
					out = append(out, l.Debit)
				}
			}
		}
	}
	return out
}

func TestWorkforceOvertimeCappedPerWorker(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	obs := Observe(sc, st)
	max := sc.Workforce.MaxOvertimePerWorker

	if r := Validate(sc, obs, []Command{{Kind: CmdScheduleOvertime, Qty: max}}, 5); !r[0].OK {
		t.Errorf("at limit rejected: %+v", r[0])
	}
	r := Validate(sc, obs, []Command{{Kind: CmdScheduleOvertime, Qty: max + 1}, {Kind: CmdScheduleOvertime, Qty: -1}}, 5)
	if r[0].Code != ReasonOvertimeExceedsLimit || r[0].Message == "" || r[1].Code != ReasonInvalidQuantity {
		t.Errorf("rejections = %+v", r)
	}
	if _, _, _, err := StepTick(sc, st, []Input{{EffectiveTick: 1, Seq: 1, Kind: InputCommand, Command: Command{Kind: CmdScheduleOvertime, Qty: max + 1}}}); err == nil {
		t.Error("engine applied overtime above the cap")
	}

	next := workforceStep(t, sc, st, Command{Kind: CmdScheduleOvertime, Qty: 2})
	if next.Workforce.OvertimeHours != 2 {
		t.Fatalf("overtime hours = %d", next.Workforce.OvertimeHours)
	}
	got := laborDebits(next, 1)
	want := []Cents{
		Cents(sc.Workforce.RegularWorkers) * sc.Wages.RegularPerHourCents,
		Cents(sc.Workforce.RegularWorkers*2) * sc.Wages.OvertimePerHourCents,
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("labor lines = %v, want %v", got, want)
	}
	if cleared := workforceStep(t, sc, next, Command{Kind: CmdScheduleOvertime, Qty: 0}); cleared.Workforce.OvertimeHours != 0 {
		t.Error("overtime not cancelled by 0")
	}
}

func TestWorkforceTempPaidFromHireIncludingOnboarding(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	onboard := sc.Workforce.TempOnboardingTicks
	const n = 5

	st = workforceStep(t, sc, st, Command{Kind: CmdHireTempWorkers, Qty: n})
	c := st.Workforce.Temps[0]
	if c.Count != n || c.HiredAt != 0 || c.ReadyAt != SimMinute(onboard*MinutesPerTick) {
		t.Fatalf("cohort = %+v", c)
	}
	// Paid in the hire Tick (1) and every onboarding Tick.
	for tick := Tick(1); int64(tick) <= onboard+1; tick++ {
		if tick > 1 {
			st, _ = mustStep(t, sc, st)
		}
		lines := laborDebits(st, tick)
		want := Cents(n) * sc.Wages.TempPerHourCents
		if len(lines) != 2 || lines[1] != want {
			t.Errorf("tick %d labor lines = %v, want temp line %d", tick, lines, want)
		}
	}
}

func TestWorkforceTempCapacityOnlyAfterOnboarding(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	base := fulfilmentCapacity(&sc, &st, 30)
	st = workforceStep(t, sc, st, Command{Kind: CmdHireTempWorkers, Qty: 5})
	add := 5 * sc.Workforce.UnitsPerWorkerHour
	ready := st.Workforce.Temps[0].ReadyAt

	if got := fulfilmentCapacity(&sc, &st, ready-1); got != base {
		t.Errorf("capacity during onboarding = %d, want %d", got, base)
	}
	if got := fulfilmentCapacity(&sc, &st, ready); got != base+add {
		t.Errorf("capacity once ready = %d, want %d", got, base+add)
	}
}

func TestWorkforceTempLimitAndCash(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	max := sc.Workforce.MaxTempWorkers
	obs := Observe(sc, st)

	r := Validate(sc, obs, []Command{
		{Kind: CmdHireTempWorkers, Qty: max},
		{Kind: CmdHireTempWorkers, Qty: 1},
		{Kind: CmdHireTempWorkers, Qty: 0},
	}, 5)
	if !r[0].OK || r[1].Code != ReasonTooManyTempWorkers || r[2].Code != ReasonInvalidQuantity {
		t.Errorf("results = %+v", r)
	}
	poor := obs
	poor.Cash = 1
	if r := Validate(sc, poor, []Command{{Kind: CmdHireTempWorkers, Qty: 1}}, 5); r[0].Code != ReasonInsufficientCash {
		t.Errorf("cash code = %q", r[0].Code)
	}
	if _, _, _, err := StepTick(sc, st, []Input{{EffectiveTick: 1, Seq: 1, Kind: InputCommand, Command: Command{Kind: CmdHireTempWorkers, Qty: max + 1}}}); err == nil {
		t.Error("engine applied hire above the cap")
	}
}
