package sim

import (
	"errors"
	"testing"
)

// marketingStep advances st one Tick, applying set_marketing_spend for sku 0
// when amount >= 0 (a negative amount means no command).
func marketingStep(t *testing.T, sc Scenario, st State, amount Cents) (State, []Outcome) {
	t.Helper()
	var in []Input
	if amount >= 0 {
		in = []Input{{EffectiveTick: st.Tick() + 1, Seq: 1, Kind: InputCommand,
			Command: Command{Kind: CmdSetMarketingSpend, SKU: 0, AmountCents: amount}}}
	}
	next, j, _, err := StepTick(sc, st, in)
	if err != nil {
		t.Fatal(err)
	}
	return next, j
}

func demandOutcome(t *testing.T, j []Outcome, sku int64) Outcome {
	t.Helper()
	for _, o := range j {
		if o.Kind == OutcomeDemand && o.SKU == sku {
			return o
		}
	}
	t.Fatalf("no demand outcome for sku %d", sku)
	return Outcome{}
}

func TestMarketingRaisesConversionOfSamePotentialArrivals(t *testing.T) {
	sc := loadFlagship(t)
	base := mustInit(t, sc, 2026)
	for i := 0; i < 18; i++ { // evening peak
		base, _ = mustStep(t, sc, base)
	}
	plain, plainJ := marketingStep(t, sc, base, -1)
	spent, spentJ := marketingStep(t, sc, base, 20000)

	a, b := demandOutcome(t, plainJ, 0), demandOutcome(t, spentJ, 0)
	if a.Qty != b.Qty {
		t.Errorf("potential arrivals differ: %d vs %d", a.Qty, b.Qty)
	}
	if b.Ref <= a.Ref || b.Aux < a.Aux {
		t.Errorf("conversion %d / orders %d with marketing, %d / %d without; want higher", b.Ref, b.Aux, a.Ref, a.Aux)
	}
	// 20000 cents is two $100 units at 300 bps each.
	if b.Ref-a.Ref != 600 {
		t.Errorf("uplift = %d bps, want 600", b.Ref-a.Ref)
	}
	if got, want := streamState(spent, StreamDemand), streamState(plain, StreamDemand); got != want {
		t.Errorf("demand stream state %d, want %d", got, want)
	}
}

func TestMarketingExpensePostedEveryTick(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	st, _ = marketingStep(t, sc, st, 12345)
	for i := 1; i <= 3; i++ {
		if got := st.Ledger.Balance(AccountMarketingExpense); got != Cents(int64(i)*12345) {
			t.Fatalf("after %d ticks Marketing_Expense = %d, want %d", i, got, int64(i)*12345)
		}
		st, _ = mustStep(t, sc, st)
	}
	// Setting the spend to 0 stops the accrual.
	before := st.Ledger.Balance(AccountMarketingExpense)
	st, _ = marketingStep(t, sc, st, 0)
	if got := st.Ledger.Balance(AccountMarketingExpense); got != before {
		t.Errorf("Marketing_Expense moved from %d to %d after spend 0", before, got)
	}
}

func TestMarketingValidation(t *testing.T) {
	sc, _, obs := flagshipObs(t)
	max := sc.Demand.MaxMarketingSpendCentsPerHour
	poor := obs
	poor.Cash = 100
	cases := []struct {
		name string
		obs  Observation
		cmd  Command
		want RejectCode
	}{
		{"at limit", obs, Command{Kind: CmdSetMarketingSpend, SKU: 0, AmountCents: max}, ""},
		{"zero cancels", obs, Command{Kind: CmdSetMarketingSpend, SKU: 0}, ""},
		{"above limit", obs, Command{Kind: CmdSetMarketingSpend, SKU: 0, AmountCents: max + 1}, ReasonMarketingExceedsLimit},
		{"negative", obs, Command{Kind: CmdSetMarketingSpend, SKU: 0, AmountCents: -1}, ReasonInvalidQuantity},
		{"unknown sku", obs, Command{Kind: CmdSetMarketingSpend, SKU: 77, AmountCents: 1}, ReasonUnknownSKU},
		{"first tick unaffordable", poor, Command{Kind: CmdSetMarketingSpend, SKU: 0, AmountCents: 101}, ReasonInsufficientCash},
	}
	for _, tc := range cases {
		r := Validate(sc, tc.obs, []Command{tc.cmd}, 5)[0]
		if r.Code != tc.want || (tc.want != "" && r.Message == "") {
			t.Errorf("%s: code %q message %q, want %q", tc.name, r.Code, r.Message, tc.want)
		}
	}
	// Cash committed by an accepted command is not available to the next one.
	r := Validate(sc, poor, []Command{
		{Kind: CmdSetMarketingSpend, SKU: 0, AmountCents: 60},
		{Kind: CmdSetMarketingSpend, SKU: 1, AmountCents: 60},
	}, 5)
	if !r[0].OK || r[1].Code != ReasonInsufficientCash {
		t.Errorf("batch = %+v", r)
	}
}

func TestMarketingEngineRejectsAboveLimit(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 1)
	_, _, _, err := StepTick(sc, st, []Input{{EffectiveTick: 1, Seq: 1, Kind: InputCommand,
		Command: Command{Kind: CmdSetMarketingSpend, SKU: 0, AmountCents: sc.Demand.MaxMarketingSpendCentsPerHour + 1}}})
	if !errors.Is(err, ErrBadCommand) || !errors.Is(err, ErrMarketingLimit) {
		t.Errorf("err = %v", err)
	}
}
