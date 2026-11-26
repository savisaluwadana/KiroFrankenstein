package sim

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func flagshipObs(t *testing.T) (Scenario, State, Observation) {
	t.Helper()
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	return sc, st, Observe(sc, st)
}

// obsSKU returns the observation of the SKU with the given index (the
// Observation lists SKUs by identifier, not by index).
func obsSKU(t *testing.T, obs Observation, idx int64) (int, ObsSKU) {
	t.Helper()
	for i, s := range obs.SKUs {
		if s.SKU == idx {
			return i, s
		}
	}
	t.Fatalf("no sku %d", idx)
	return 0, ObsSKU{}
}

func TestValidateRejections(t *testing.T) {
	sc, _, obs := flagshipObs(t)
	pos, s0 := obsSKU(t, obs, 0)
	ref := s0.ReferencePriceCents
	_, hi := PriceBounds(ref)
	outage := obs
	outage.SKUs = append([]ObsSKU(nil), obs.SKUs...)
	outage.SKUs[pos].Suppliers = append([]ObsSupplier(nil), s0.Suppliers...)
	outage.SKUs[pos].Suppliers[0].Available = false

	tests := []struct {
		name string
		obs  Observation
		cmd  Command
		bud  int64
		want RejectCode
	}{
		{"unknown kind", obs, Command{Kind: 99}, 5, ReasonUnknownCommand},
		{"not implemented", obs, Command{Kind: CmdHireTempWorkers, Qty: 2}, 5, ReasonNotImplemented},
		{"budget", obs, Command{Kind: CmdSetPrice, SKU: 0, AmountCents: ref}, 0, ReasonBudgetExhausted},
		{"unknown sku", obs, Command{Kind: CmdSetPrice, SKU: 77, AmountCents: ref}, 5, ReasonUnknownSKU},
		{"price high", obs, Command{Kind: CmdSetPrice, SKU: 0, AmountCents: hi + 1}, 5, ReasonPriceOutOfBounds},
		{"qty zero", obs, Command{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: SupplierBackup}, 5, ReasonInvalidQuantity},
		{"no supplier", obs, Command{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: 9, Qty: 1}, 5, ReasonUnknownSupplier},
		{"outage", outage, Command{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: SupplierPrimary, Qty: 1}, 5, ReasonSupplierUnavailable},
		{"cash", obs, Command{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: SupplierBackup, Qty: MaxOrderQuantity}, 5, ReasonInsufficientCash},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := tc.cmd
			got := Validate(sc, tc.obs, []Command{tc.cmd}, tc.bud)
			if len(got) != 1 || got[0].OK || got[0].Code != tc.want || got[0].Message == "" {
				t.Fatalf("result = %+v, want code %s with message", got, tc.want)
			}
			if got[0].Command != orig || tc.cmd != orig {
				t.Errorf("command changed: %+v", got[0].Command)
			}
		})
	}
}

func TestValidateSequentialCashAndBudget(t *testing.T) {
	sc, _, obs := flagshipObs(t)
	_, s0 := obsSKU(t, obs, 0)
	unit := int64(s0.Suppliers[1].UnitCostCents) // backup
	obs.Cash = Cents(unit * 100)
	po := func(q int64) Command {
		return Command{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: SupplierBackup, Qty: q}
	}
	got := Validate(sc, obs, []Command{po(60), po(60), po(40), {Kind: CmdSetPrice, SKU: 0, AmountCents: s0.PriceCents}, po(1)}, 3)
	want := []struct {
		ok   bool
		code RejectCode
	}{{true, ""}, {false, ReasonInsufficientCash}, {true, ""}, {true, ""}, {false, ReasonBudgetExhausted}}
	for i, w := range want {
		if got[i].OK != w.ok || got[i].Code != w.code {
			t.Errorf("result %d = %+v, want ok=%v code=%q", i, got[i], w.ok, w.code)
		}
	}
	if !reflect.DeepEqual(got, Validate(sc, obs, []Command{po(60), po(60), po(40), {Kind: CmdSetPrice, SKU: 0, AmountCents: s0.PriceCents}, po(1)}, 3)) {
		t.Error("validation is not deterministic")
	}
}

func TestValidateReservesOpenPurchaseOrders(t *testing.T) {
	sc, _, obs := flagshipObs(t)
	_, s0 := obsSKU(t, obs, 0)
	unit := int64(s0.Suppliers[1].UnitCostCents)
	obs.Cash = Cents(unit * 10)
	obs.PurchaseOrders = []ObsPurchaseOrder{{ID: 1, SKU: 0, Supplier: SupplierBackup, Qty: 10, UnitCost: Cents(unit)}}
	got := Validate(sc, obs, []Command{{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: SupplierBackup, Qty: 1}}, 5)
	if got[0].Code != ReasonInsufficientCash {
		t.Errorf("result = %+v", got[0])
	}
}

func TestObserveOmitsFuture(t *testing.T) {
	sc, st, obs := flagshipObs(t)
	if len(sc.DisruptionScript) == 0 {
		t.Fatal("flagship has no script")
	}
	onset := sc.DisruptionScript[0].OnsetTick
	for _, d := range sc.DisruptionScript {
		if d.OnsetTick < onset {
			onset = d.OnsetTick
		}
	}
	if len(obs.Disruptions) != 0 || obs.Tick != 0 || len(obs.SKUs) != 4 {
		t.Fatalf("tick 0 observation = %+v", obs)
	}
	for i := 1; i < len(obs.SKUs); i++ {
		if obs.SKUs[i-1].ID >= obs.SKUs[i].ID {
			t.Errorf("SKUs not sorted by id: %q then %q", obs.SKUs[i-1].ID, obs.SKUs[i].ID)
		}
	}
	for st.Tick() < Tick(onset-1) {
		st, _ = mustStep(t, sc, st)
	}
	if got := Observe(sc, st); len(got.Disruptions) != 0 {
		t.Errorf("tick %d shows %d disruptions before onset %d", got.Tick, len(got.Disruptions), onset)
	}
	st, _ = mustStep(t, sc, st)
	got := Observe(sc, st)
	if len(got.Disruptions) == 0 {
		t.Fatalf("tick %d shows no disruption at onset", got.Tick)
	}
	for _, d := range got.Disruptions {
		if d.OnsetTick > got.Tick {
			t.Errorf("future disruption %+v at tick %d", d, got.Tick)
		}
	}

	// No seed, draw, stream or script field exists on the Observation.
	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			n := strings.ToLower(rt.Field(i).Name)
			for _, bad := range []string{"seed", "stream", "draw", "script", "pending"} {
				if strings.Contains(n, bad) {
					t.Errorf("%s.%s exposes hidden state", rt.Name(), rt.Field(i).Name)
				}
			}
			walk(rt.Field(i).Type)
		}
	}
	walk(reflect.TypeOf(obs))
}

func TestCommandApplyPurchaseOrder(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	for st.Tick() < 4 {
		st, _ = mustStep(t, sc, st)
	}
	cmd := Command{Kind: CmdPlacePurchaseOrder, SKU: 0, Role: SupplierBackup, Qty: 50}
	if r := Validate(sc, Observe(sc, st), []Command{cmd}, 5); !r[0].OK {
		t.Fatalf("validate: %+v", r[0])
	}
	onHand := st.Inventory[0].OnHand
	cash := st.Ledger.Balance(AccountCash)
	next, _, _, err := StepTick(sc, st, []Input{{EffectiveTick: 5, Seq: 1, Kind: InputCommand, Command: cmd}})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.PurchaseOs) != 1 {
		t.Fatalf("purchase orders = %+v", next.PurchaseOs)
	}
	po := next.PurchaseOs[0]
	if po.PlacedAt != 4*60 || po.DueAt != 4*60+8*60 || po.Qty != 50 || po.Supplier != SupplierBackup {
		t.Errorf("po = %+v, want placed at the N*60 boundary of tick 4", po)
	}
	if next.Ledger.Balance(AccountCash) < cash-1 && len(next.Ledger.Entries) == len(st.Ledger.Entries) {
		t.Error("cash changed without ledger entries")
	}
	for next.Tick() < 4+8+1 {
		next, _ = mustStep(t, sc, next)
	}
	if len(next.PurchaseOs) != 0 || next.Inventory[0].Received != 50 {
		t.Errorf("after lead time: pos=%+v received=%d", next.PurchaseOs, next.Inventory[0].Received)
	}
	if next.Inventory[0].Received+onHand < 50 {
		t.Error("unexpected inventory")
	}
}

func TestCommandApplySetPriceChangesConversion(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	_, hi := PriceBounds(sc.SKUs[0].ReferencePriceCents)
	cmd := Command{Kind: CmdSetPrice, SKU: 0, AmountCents: hi}

	base, baseOut := mustStep(t, sc, st)
	next, out, _, err := StepTick(sc, st, []Input{{EffectiveTick: 1, Seq: 1, Kind: InputCommand, Command: cmd}})
	if err != nil {
		t.Fatal(err)
	}
	if next.Prices[0].PriceCents != hi || base.Prices[0].PriceCents == hi {
		t.Fatalf("prices = %d / base %d", next.Prices[0].PriceCents, base.Prices[0].PriceCents)
	}
	var baseConv, conv int64
	for i := range out {
		if out[i].Kind == OutcomeDemand && out[i].SKU == 0 {
			conv, baseConv = out[i].Ref, baseOut[i].Ref
			if out[i].Qty != baseOut[i].Qty {
				t.Error("potential arrivals depend on the command")
			}
		}
	}
	if conv >= baseConv {
		t.Errorf("conversion with higher price = %d, baseline %d", conv, baseConv)
	}
	if streamState(next, StreamDemand) != streamState(base, StreamDemand) {
		t.Error("command changed the demand stream")
	}
	for _, o := range next.Orders.Open {
		if o.SKU == 0 && o.UnitPrice != hi {
			t.Errorf("order priced %d, want %d (command applies before arrivals)", o.UnitPrice, hi)
		}
	}
}

func TestCommandApplyErrors(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 1)
	in := func(tick Tick, c Command) []Input {
		return []Input{{EffectiveTick: tick, Seq: 1, Kind: InputCommand, Command: c}}
	}
	if _, _, _, err := StepTick(sc, st, in(2, Command{Kind: CmdSetPrice})); !errors.Is(err, ErrBadInputTick) {
		t.Errorf("wrong tick err = %v", err)
	}
	if _, _, _, err := StepTick(sc, st, in(1, Command{Kind: CmdScheduleOvertime, Qty: 1})); !errors.Is(err, ErrCommandNotImplemented) {
		t.Errorf("not implemented err = %v", err)
	}
	if _, _, _, err := StepTick(sc, st, in(1, Command{Kind: CmdSetPrice, SKU: 9, AmountCents: 100})); !errors.Is(err, ErrBadCommand) {
		t.Errorf("bad command err = %v", err)
	}
}

func TestCommandEncodeRoundTrip(t *testing.T) {
	c := Command{Kind: CmdPlacePurchaseOrder, SKU: 2, Role: 1, Qty: 40, AmountCents: 5, PORef: 7}
	got, err := DecodeCommand(EncodeCommand(c))
	if err != nil || got != c {
		t.Errorf("round trip = %+v, %v", got, err)
	}
	if _, err := DecodeCommand([]byte{1}); err == nil {
		t.Error("short input accepted")
	}
}
