package sim

import (
	"errors"
	"os"
	"testing"
)

func outageScenario(onset, duration int64) Scenario {
	sc := fulfilScenario()
	primary := "primary"
	sc.DisruptionScript = []DisruptionSpec{{Kind: "supplier_outage", OnsetTick: onset, DurationTicks: duration, Target: &primary}}
	return sc
}

func placeAt(t *testing.T, sc *Scenario, st *State, at SimMinute) PurchaseOrder {
	t.Helper()
	q, err := st.Queue()
	if err != nil {
		t.Fatal(err)
	}
	po, err := PlacePurchaseOrder(sc, st, q, 0, SupplierPrimary, 10, at)
	if err != nil {
		t.Fatal(err)
	}
	st.SetQueue(q)
	return po
}

func TestSupplierOutageDelaysReceiptInsideOutage(t *testing.T) {
	// Outage covers minutes [240, 840). A PO due at 720 (lead 12 ticks) moves
	// to outage end 840 + lead 720 = 1560.
	sc := outageScenario(5, 10)
	st := mustInit(t, sc, 1)
	po := placeAt(t, &sc, &st, 0)
	if po.DueAt != 720 {
		t.Fatalf("due before outage = %d", po.DueAt)
	}
	for i := 0; i < 5; i++ {
		st = stepNoErr(t, sc, st)
	}
	if len(st.Disruptions) != 1 || st.PurchaseOs[0].DueAt != 1560 {
		t.Fatalf("after activation: disruptions %+v, PO %+v", st.Disruptions, st.PurchaseOs)
	}
	for st.Tick() < 26 { // minute 1559 is the last of Tick 26
		st = stepNoErr(t, sc, st)
		if st.Inventory[0].Received != 0 {
			t.Fatalf("received at tick %d, before the delayed due time", st.Tick())
		}
	}
	if len(st.Disruptions) != 0 {
		t.Errorf("outage not expired: %+v", st.Disruptions)
	}
	st = stepNoErr(t, sc, st) // Tick 27 contains minute 1560
	if inv := st.Inventory[0]; inv.Received != 10 || inv.OnHand != 60 || len(st.PurchaseOs) != 0 {
		t.Errorf("inventory %+v, POs %+v", inv, st.PurchaseOs)
	}
}

func TestSupplierOutageReceiptOutsideOutageUnchanged(t *testing.T) {
	sc := outageScenario(20, 2) // [1140, 1260), after the 720 due time
	st := mustInit(t, sc, 1)
	placeAt(t, &sc, &st, 0)
	for i := 0; i < 13; i++ {
		st = stepNoErr(t, sc, st)
	}
	if st.Inventory[0].Received != 10 {
		t.Errorf("receipt before the outage should arrive on time: %+v", st.Inventory[0])
	}
}

func TestSupplierOutagePlacedDuringOutage(t *testing.T) {
	sc := outageScenario(2, 5) // [60, 360)
	st := mustInit(t, sc, 1)
	for i := 0; i < 2; i++ {
		st = stepNoErr(t, sc, st)
	}
	po := placeAt(t, &sc, &st, st.Clock)
	if want := SimMinute(360 + 12*60); po.DueAt != want {
		t.Errorf("due = %d, want %d", po.DueAt, want)
	}
}

func TestSupplierOutageBackupUnaffected(t *testing.T) {
	d := ActiveDisruption{Kind: DisruptionSupplierOutage, Onset: 0, End: 100, MagnitudeBps: Bps(SupplierPrimary), Target: -1}
	if _, down := outageEnd([]ActiveDisruption{d}, 0, SupplierBackup, 10); down {
		t.Error("primary outage must not affect the backup supplier")
	}
	if end, down := outageEnd([]ActiveDisruption{d}, 0, SupplierPrimary, 10); !down || end != 100 {
		t.Errorf("primary: %d %v", end, down)
	}
	if _, down := outageEnd([]ActiveDisruption{d}, 0, SupplierPrimary, 100); down {
		t.Error("outage is over at its end minute")
	}
}

func TestSupplierOutageSameSimTimeOrdering(t *testing.T) {
	// Entry 0 ends and entry 1 starts at minute 240; entry 2 also starts there.
	primary := "primary"
	sc := fulfilScenario()
	sc.DisruptionScript = []DisruptionSpec{
		{Kind: "supplier_outage", OnsetTick: 3, DurationTicks: 2, Target: &primary},
		{Kind: "supplier_outage", OnsetTick: 5, DurationTicks: 1, Target: &primary},
		{Kind: "supplier_outage", OnsetTick: 5, DurationTicks: 1, Target: &primary},
	}
	st := mustInit(t, sc, 1)
	q, err := st.Queue()
	if err != nil {
		t.Fatal(err)
	}
	// Pushed after the disruptions, yet the command must run first and the
	// receipt after both disruption steps.
	q.Push(Event{At: 240, Priority: PriorityPurchaseReceipt, Kind: EvPurchaseReceipt})
	q.Push(Event{At: 240, Priority: PriorityCommandApply, Kind: EvCommandApply})

	type step struct {
		kind EventKind
		ref  int64
	}
	var got []step
	for {
		ev, ok := q.Pop()
		if !ok || ev.At > 240 {
			break
		}
		if ev.At == 240 {
			got = append(got, step{ev.Kind, ev.Payload.Ref})
		}
	}
	want := []step{
		{EvCommandApply, 0},
		{EvDisruptionEnd, 0},
		{EvDisruptionOnset, 1},
		{EvDisruptionOnset, 2},
		{EvPurchaseReceipt, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("events at 240 = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events at 240 = %+v, want %+v", got, want)
		}
	}
}

func TestSupplierOutageRejectsBadScript(t *testing.T) {
	bad := "nowhere"
	for name, spec := range map[string]DisruptionSpec{
		"unknown kind": {Kind: "meteor", OnsetTick: 3, DurationTicks: 2},
		"zero onset":   {Kind: "supplier_outage", OnsetTick: 0, DurationTicks: 2, Target: &bad},
		"bad role":     {Kind: "supplier_outage", OnsetTick: 3, DurationTicks: 2, Target: &bad},
		"no magnitude": {Kind: "demand_surge", OnsetTick: 3, DurationTicks: 2},
	} {
		sc := fulfilScenario()
		sc.DisruptionScript = []DisruptionSpec{spec}
		if _, err := Init(sc, 1); !errors.Is(err, ErrBadDisruption) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestSupplierOutageFlagshipDisruptionsActivateAtOnset(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	st := mustInit(t, *sc, 2026)
	kinds := map[string]uint8{
		"demand_surge": DisruptionDemandSurge, "supplier_outage": DisruptionSupplierOutage, "workforce_absence": DisruptionWorkforceAbsence,
	}
	for st.Tick() < 100 {
		st = stepNoErr(t, *sc, st)
		tick := int64(st.Tick())
		active := map[uint8]bool{}
		for _, d := range st.Disruptions {
			active[d.Kind] = true
		}
		for _, d := range sc.DisruptionScript {
			want := d.OnsetTick <= tick && tick < d.OnsetTick+d.DurationTicks
			if got := active[kinds[d.Kind]]; got != want {
				t.Fatalf("tick %d: %s active = %v, want %v", tick, d.Kind, got, want)
			}
		}
	}
}
