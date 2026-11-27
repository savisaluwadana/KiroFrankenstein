package sim

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func loadFlagship(t *testing.T) Scenario {
	t.Helper()
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	return *s
}

func mustInit(t *testing.T, sc Scenario, seed Seed) State {
	t.Helper()
	st, err := Init(sc, seed)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func mustStep(t *testing.T, sc Scenario, st State) (State, []Outcome) {
	t.Helper()
	out, j, _, err := StepTick(sc, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out, j
}

func streamState(st State, name StreamName) StreamState {
	for _, ns := range st.Streams {
		if ns.Name == name {
			return ns.State
		}
	}
	return 0
}

// Pinned by running the engine at seed 2026 on the flagship scenario.
const (
	pinnedTick1Potential int64 = 111
	pinnedTick1Orders    int64 = 27
	pinnedTenTickOrders  int64 = 538
)

func TestInitFlagship(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)

	if st.Clock != 0 || st.Tick() != 0 {
		t.Errorf("clock = %d", st.Clock)
	}
	if len(st.Inventory) != 4 || st.Inventory[0].OnHand != 1400 || st.Inventory[0].Value != 1400*3000 {
		t.Errorf("inventory = %+v", st.Inventory)
	}
	if got := st.Ledger.Balance(AccountCash); got != sc.OpeningCashCents {
		t.Errorf("cash = %d, want %d", got, sc.OpeningCashCents)
	}
	if len(st.Ledger.Entries) != 1 || st.Ledger.Entries[0].Cause != CauseOpening {
		t.Errorf("ledger entries = %d", len(st.Ledger.Entries))
	}
	if st.Prices[2].PriceCents != sc.SKUs[2].ReferencePriceCents || st.Workforce.Regular != 14 {
		t.Errorf("prices/workforce = %+v %+v", st.Prices, st.Workforce)
	}
	names := []StreamName{}
	for _, ns := range st.Streams {
		names = append(names, ns.Name)
	}
	if !reflect.DeepEqual(names, []StreamName{"demand", "disruption", "supplier", "workforce"}) {
		t.Errorf("streams = %v", names)
	}
	if st.Streams[0].State != NewStream(2026, "demand").State() {
		t.Error("demand stream not derived from seed and name")
	}

	// Tick 1's recurring events at the engine-version-1 offsets. The flagship's
	// three scripted disruptions add six later events (activation and expiry).
	want := []struct {
		at   SimMinute
		prio uint8
		kind EventKind
	}{
		{5, 4, EvDemandArrivals}, {30, 5, EvFulfilment}, {55, 6, EvOrderCancel},
		{58, 7, EvLaborAccrual}, {58, 7, EvMarketingAccrual}, {58, 7, EvOverheadAccrual},
	}
	if len(st.Pending) != len(want)+6 {
		t.Fatalf("pending = %d, want %d", len(st.Pending), len(want)+6)
	}
	for i, w := range want {
		p := st.Pending[i]
		if p.At != w.at || p.Priority != w.prio || p.Kind != w.kind {
			t.Errorf("pending[%d] = %+v, want %+v", i, p, w)
		}
	}
	if st.EventSeq != 12 {
		t.Errorf("EventSeq = %d, want 12", st.EventSeq)
	}

	again := mustInit(t, sc, 2026)
	if Hash(st) != Hash(again) {
		t.Error("Init is not deterministic")
	}
	if Hash(st) == Hash(mustInit(t, sc, 2027)) {
		t.Error("different seeds give the same Tick 0 hash")
	}
}

func TestInitRejectsInvalidScenario(t *testing.T) {
	sc := loadFlagship(t)
	sc.HorizonTicks = 0 // outside its assumption bounds
	if _, err := Init(sc, 1); !errors.Is(err, ErrInvalidScenario) {
		t.Errorf("err = %v, want ErrInvalidScenario", err)
	}
	sc = loadFlagship(t)
	sc.SKUs[0].ReferencePriceCents = 0
	sc.AssumptionMeta["skus.headphones.referencePriceCents"] = AssumptionMeta{Unit: "cents", Default: 0, Min: 0, Max: 100000, Rationale: "synthetic, illustrative"}
	if _, err := Init(sc, 1); !errors.Is(err, ErrInvalidScenario) {
		t.Errorf("err = %v, want ErrInvalidScenario", err)
	}
}

// Fixed-seed example: the flagship at seed 2026, Tick 1 (hour 0, profile 3000).
func TestDemandFixedSeedPinned(t *testing.T) {
	sc := loadFlagship(t)
	st := mustInit(t, sc, 2026)
	out, journal := mustStep(t, sc, st)

	var potential, orders int64
	for _, o := range journal {
		if o.Kind == OutcomeShipment {
			continue // fulfilment ships some of the new orders at minute 30
		}
		if o.Kind != OutcomeDemand {
			t.Fatalf("unexpected outcome %+v", o)
		}
		potential += o.Qty
		orders += o.Aux
	}
	if potential != pinnedTick1Potential || orders != pinnedTick1Orders {
		t.Errorf("tick 1: potential = %d, orders = %d; want %d, %d", potential, orders, pinnedTick1Potential, pinnedTick1Orders)
	}
	if out.Orders.Created != orders || int64(len(out.Orders.Open))+out.Orders.Shipped != orders || out.OrderSeq != uint64(1+orders) {
		t.Errorf("order book = created %d open %d shipped %d seq %d", out.Orders.Created, len(out.Orders.Open), out.Orders.Shipped, out.OrderSeq)
	}
	if out.Clock != 60 {
		t.Errorf("clock = %d", out.Clock)
	}
	// With no workers nothing ships, so the created orders stay open to inspect.
	sc0 := sc
	st0 := mustInit(t, sc0, 2026)
	st0.Workforce.Regular = 0
	held, _ := mustStep(t, sc0, st0)
	if o := held.Orders.Open[0]; o.CreatedAt != 5 || o.PromisedBy != 5+24*60 || o.Qty != 1 || o.UnitPrice != sc.SKUs[0].ReferencePriceCents {
		t.Errorf("first order = %+v", o)
	}

	// Ten ticks at the same seed.
	cur := st
	var totalOrders int64
	for i := 0; i < 10; i++ {
		var j []Outcome
		cur, j = mustStep(t, sc, cur)
		for _, o := range j {
			if o.Kind == OutcomeDemand {
				totalOrders += o.Aux
			}
		}
	}
	if totalOrders != pinnedTenTickOrders {
		t.Errorf("10 ticks: orders = %d, want %d", totalOrders, pinnedTenTickOrders)
	}
}

func TestDemandPriceLowersOrdersWithoutChangingStream(t *testing.T) {
	sc := loadFlagship(t)
	// Evening peak so conversions are large enough to show the effect.
	base := mustInit(t, sc, 2026)
	for i := 0; i < 18; i++ {
		base, _ = mustStep(t, sc, base)
	}

	count := func(st State) (int64, State) {
		out, j := mustStep(t, sc, st)
		var n int64
		for _, o := range j {
			n += o.Aux
		}
		return n, out
	}
	baseOrders, baseOut := count(base)

	high := base.clone()
	for i := range high.Prices {
		high.Prices[i].PriceCents = high.Prices[i].PriceCents * 2 // +100%
	}
	highOrders, highOut := count(high)
	if highOrders >= baseOrders {
		t.Errorf("doubling prices: orders %d, base %d; want fewer", highOrders, baseOrders)
	}
	if !reflect.DeepEqual(baseOut.Streams, highOut.Streams) {
		t.Errorf("price changed the stream state: %v vs %v", baseOut.Streams, highOut.Streams)
	}

	mk := base.clone()
	for i := range mk.Prices {
		mk.Prices[i].MarketingCents = 20000
	}
	mkOrders, mkOut := count(mk)
	if mkOrders <= baseOrders {
		t.Errorf("marketing: orders %d, base %d; want more", mkOrders, baseOrders)
	}
	if !reflect.DeepEqual(baseOut.Streams, mkOut.Streams) {
		t.Error("marketing changed the stream state")
	}

	// Exactly DemandDrawsPerSKUPerTick draws per SKU were taken.
	fresh := RestoreStream(streamState(base, StreamDemand))
	for i := 0; i < len(sc.SKUs)*DemandDrawsPerSKUPerTick; i++ {
		fresh.Next()
	}
	if fresh.State() != streamState(baseOut, StreamDemand) {
		t.Error("demand stream did not advance by the fixed draw count")
	}
	// Streams other than demand are untouched.
	if streamState(baseOut, StreamSupplier) != streamState(base, StreamSupplier) {
		t.Error("supplier stream moved")
	}
}

func TestDemandSurgeAndConversion(t *testing.T) {
	d := DemandSpec{BaseArrivalsPerTick: 100, BaseConversionBps: 2500, PriceElasticityBps: 1500, MarketingEffectBps: 300, NoiseBps: 2000, MarketingSpendUnitCents: 10000}
	d.HourlyProfile = make([]Bps, 24)
	for i := range d.HourlyProfile {
		d.HourlyProfile[i] = 10000
	}
	// Noise draw 2000 is the zero-noise point of the uniform range.
	if got := potentialArrivals(d, 3, 10000, 2000); got != 100 {
		t.Errorf("no surge = %d, want 100", got)
	}
	if got := potentialArrivals(d, 3, 25000, 2000); got != 250 {
		t.Errorf("2.5x surge = %d, want 250", got)
	}
	active := []ActiveDisruption{
		{Kind: DisruptionDemandSurge, MagnitudeBps: 25000, Target: -1},
		{Kind: DisruptionDemandSurge, MagnitudeBps: 20000, Target: 1},
		{Kind: DisruptionSupplierOutage, MagnitudeBps: 99999, Target: -1},
	}
	if got := surgeBps(active, 0); got != 25000 {
		t.Errorf("surge sku0 = %d", got)
	}
	if got := surgeBps(active, 1); got != 50000 {
		t.Errorf("surge sku1 = %d", got)
	}

	sku := SKUSpec{ReferencePriceCents: 2000}
	cases := []struct {
		name         string
		price, spend Cents
		want         Bps
	}{
		{"reference", 2000, 0, 2500},
		{"price +10%", 2200, 0, 2350},
		{"price -10%", 1800, 0, 2650},
		{"marketing $100", 2000, 10000, 2800},
		{"clamped low", 20000, 0, 0},
		{"clamped high", 2000, 10000000, 10000},
	}
	for _, c := range cases {
		if got := conversionBps(d, sku, c.price, c.spend); got != c.want {
			t.Errorf("%s: conversion = %d, want %d", c.name, got, c.want)
		}
	}
}

func cancelScenario(t *testing.T) Scenario {
	t.Helper()
	sc := *validScenario()
	sc.HorizonTicks = 10
	sc.Demand.CancelThresholdTicks = 3
	sc.Demand.BaseConversionBps = 10000
	// No workers means no fulfilment capacity, so orders stay open until they
	// are cancelled and these tests isolate the cancellation behaviour.
	sc.Workforce.RegularWorkers = 0
	return sc
}

func TestCancelAtThresholdRecordsNoLedgerEntry(t *testing.T) {
	sc := cancelScenario(t)
	st := mustInit(t, sc, 7)

	created := map[int]int64{}   // orders created per tick
	cancelled := map[int]int64{} // orders cancelled per tick
	for tick := 1; tick <= 5; tick++ {
		var j []Outcome
		st, j = mustStep(t, sc, st)
		for _, o := range j {
			switch o.Kind {
			case OutcomeDemand:
				created[tick] += o.Aux
			case OutcomeOrderCancelled:
				cancelled[tick]++
				if o.Aux < 3*60 {
					t.Errorf("cancelled at age %d minutes, below threshold", o.Aux)
				}
			}
		}
		for _, e := range st.Ledger.Entries {
			if e.Cause != CauseOpening && e.Cause != CauseLabor && e.Cause != CauseMarketing && e.Cause != CauseOverhead {
				t.Fatalf("tick %d: entry cause %d; cancellation must post none", tick, e.Cause)
			}
		}
	}
	// Orders created in tick k reach age 3*60 at the tick k+3 cancel event.
	if created[1] == 0 || cancelled[1]+cancelled[2]+cancelled[3] != 0 ||
		cancelled[4] != created[1] || cancelled[5] != created[2] {
		t.Errorf("created = %v, cancelled = %v", created, cancelled)
	}
	if st.Orders.Cancelled != created[1]+created[2] || st.Orders.UnitsCancelled != st.Orders.Cancelled {
		t.Errorf("book cancelled = %d units %d", st.Orders.Cancelled, st.Orders.UnitsCancelled)
	}
	// Order conservation (INV-9).
	if got := int64(len(st.Orders.Open)) + st.Orders.Shipped + st.Orders.Cancelled; got != st.Orders.Created {
		t.Errorf("created %d != open+shipped+cancelled %d", st.Orders.Created, got)
	}
}

func TestStepTickPurityHorizonAndRecurrence(t *testing.T) {
	sc := cancelScenario(t)
	sc.HorizonTicks = 3
	st := mustInit(t, sc, 11)
	before := Hash(st)

	next, _ := mustStep(t, sc, st)
	if Hash(st) != before {
		t.Fatal("StepTick modified its input state")
	}
	if len(next.Pending) != 6 || next.Pending[0].At != 60+5 {
		t.Errorf("pending after tick 1 = %+v", next.Pending)
	}
	// Replay from the same input gives the same hash (INV-1).
	again, _ := mustStep(t, sc, st)
	if Hash(again) != Hash(next) {
		t.Error("StepTick is not deterministic")
	}
	// Snapshot round trip mid-run resumes identically (INV-2).
	dec, err := Decode(Encode(next))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := mustStep(t, sc, next)
	b, _ := mustStep(t, sc, dec)
	if Hash(a) != Hash(b) {
		t.Error("resuming from a decoded snapshot diverged")
	}

	last, _ := mustStep(t, sc, a)
	if len(last.Pending) != 0 {
		t.Errorf("events pending past the horizon: %+v", last.Pending)
	}
	if _, _, _, err := StepTick(sc, last, nil); !errors.Is(err, ErrHorizonReached) {
		t.Errorf("err = %v, want ErrHorizonReached", err)
	}
	if _, _, _, err := StepTick(sc, st, []Input{{EffectiveTick: 1, Kind: InputKind(99)}}); !errors.Is(err, ErrInputsUnsupported) {
		t.Errorf("err = %v, want ErrInputsUnsupported", err)
	}
}
