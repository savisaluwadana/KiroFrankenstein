package sim

import (
	"errors"
	"testing"
)

// expediteFixture returns the flagship at tick 4 advanced one tick past a
// backup purchase order, and that order.
func expediteFixture(t *testing.T) (Scenario, State, PurchaseOrder) {
	t.Helper()
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	for st.Tick() < 4 {
		st, _ = mustStep(t, sc, st)
	}
	cmd := Command{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: SupplierBackup, Qty: 50}
	next, _, _, err := StepTick(sc, st, []Input{{EffectiveTick: st.Tick() + 1, Seq: 1, Kind: InputCommand, Command: cmd}})
	if err != nil || len(next.PurchaseOs) != 1 {
		t.Fatalf("setup: %v, %+v", err, next.PurchaseOs)
	}
	return sc, next, next.PurchaseOs[0]
}

func TestExpediteReducesLeadTimeAndPostsFee(t *testing.T) {
	sc, st, po := expediteFixture(t)
	fee := sc.Costs.ExpediteFeeCents

	cmd := Command{Kind: CmdExpeditePurchaseOrder, PORef: po.ID}
	if r := Validate(sc, Observe(sc, st), []Command{cmd}, 5); !r[0].OK {
		t.Fatalf("validate: %+v", r[0])
	}
	next, _, _, err := StepTick(sc, st, []Input{{EffectiveTick: st.Tick() + 1, Seq: 2, Kind: InputCommand, Command: cmd}})
	if err != nil {
		t.Fatal(err)
	}
	got := next.PurchaseOs[0]
	want := po.DueAt - SimMinute(sc.Costs.ExpediteReductionTicks*MinutesPerTick)
	if got.DueAt != want || got.Expedited != 1 {
		t.Errorf("po = %+v, want due %d expedited", got, want)
	}
	var feeEntries int
	for _, en := range next.Ledger.Entries[len(st.Ledger.Entries):] {
		if en.Cause != CauseFreight {
			continue
		}
		feeEntries++
		if en.CauseRef != int64(po.ID) || len(en.Lines) != 2 ||
			en.Lines[0] != (LedgerLine{Account: AccountFreightExpense, Debit: fee}) ||
			en.Lines[1] != (LedgerLine{Account: AccountCash, Credit: fee}) {
			t.Errorf("fee entry = %+v", en)
		}
	}
	if feeEntries != 1 {
		t.Errorf("fee entries = %d, want 1", feeEntries)
	}

	// Received at the new due time; the stale receipt at the old time is ignored.
	for next.Tick() < Tick(int64(po.DueAt)/MinutesPerTick)+2 {
		next, _ = mustStep(t, sc, next)
	}
	if len(next.PurchaseOs) != 0 || next.Inventory[0].Received != 50 {
		t.Errorf("after expedited due: pos=%+v received=%d", next.PurchaseOs, next.Inventory[0].Received)
	}
}

func TestExpediteValidatorRejections(t *testing.T) {
	sc, st, po := expediteFixture(t)
	obs := Observe(sc, st)
	exp := Command{Kind: CmdExpeditePurchaseOrder, PORef: po.ID}

	if r := Validate(sc, obs, []Command{{Kind: CmdExpeditePurchaseOrder, PORef: 999}}, 5); r[0].Code != ReasonUnknownPO {
		t.Errorf("unknown po code = %q", r[0].Code)
	}
	if r := Validate(sc, obs, []Command{exp, exp}, 5); !r[0].OK || r[1].Code != ReasonAlreadyExpedited {
		t.Errorf("duplicate in batch = %+v", r)
	}
	done := obs
	done.PurchaseOrders = append([]ObsPurchaseOrder(nil), obs.PurchaseOrders...)
	done.PurchaseOrders[0].Expedited = true
	if r := Validate(sc, done, []Command{exp}, 5); r[0].Code != ReasonAlreadyExpedited {
		t.Errorf("expedited code = %q", r[0].Code)
	}
	broke := obs
	broke.Cash = 0
	if r := Validate(sc, broke, []Command{exp}, 5); r[0].Code != ReasonInsufficientCash {
		t.Errorf("cash code = %q", r[0].Code)
	}
	down := obs
	down.SKUs = append([]ObsSKU(nil), obs.SKUs...)
	i, s := obsSKU(t, obs, po.SKU)
	down.SKUs[i].Suppliers = append([]ObsSupplier(nil), s.Suppliers...)
	for j := range down.SKUs[i].Suppliers {
		down.SKUs[i].Suppliers[j].Available = false
	}
	if r := Validate(sc, down, []Command{exp}, 5); r[0].Code != ReasonSupplierUnavailable {
		t.Errorf("outage code = %q", r[0].Code)
	}
	if r := Validate(sc, obs, []Command{exp}, 0); r[0].Code != ReasonBudgetExhausted {
		t.Errorf("budget code = %q", r[0].Code)
	}
}

func TestExpediteEngineErrors(t *testing.T) {
	sc, st, po := expediteFixture(t)
	in := func(c Command) []Input {
		return []Input{{EffectiveTick: st.Tick() + 1, Seq: 2, Kind: InputCommand, Command: c}}
	}
	if _, _, _, err := StepTick(sc, st, in(Command{Kind: CmdExpeditePurchaseOrder, PORef: 999})); !errors.Is(err, ErrBadCommand) || !errors.Is(err, ErrUnknownPO) {
		t.Errorf("unknown po err = %v", err)
	}
	next, _, _, err := StepTick(sc, st, in(Command{Kind: CmdExpeditePurchaseOrder, PORef: po.ID}))
	if err != nil {
		t.Fatal(err)
	}
	rep := []Input{{EffectiveTick: next.Tick() + 1, Seq: 3, Kind: InputCommand, Command: Command{Kind: CmdExpeditePurchaseOrder, PORef: po.ID}}}
	if _, _, _, err := StepTick(sc, next, rep); !errors.Is(err, ErrAlreadyExpedited) {
		t.Errorf("repeat err = %v", err)
	}
}
