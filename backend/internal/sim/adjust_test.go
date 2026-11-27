package sim

import (
	"errors"
	"testing"
)

func adjusted(t *testing.T, ov map[string]int64) Scenario {
	t.Helper()
	sc := loadFlagship(t)
	out, err := ApplyOverridesAndValidate(&sc, ov)
	if err != nil {
		t.Fatal(err)
	}
	return *out
}

func TestAdjustAssumptionsListed(t *testing.T) {
	sc := loadFlagship(t)
	want := map[string]int64{
		"demand.noiseBps": 2000, "demand.marketingSpendUnitCents": 10000, "demand.maxMarketingSpendCentsPerHour": 50000,
		"pricing.minPriceBps": 5000, "pricing.maxPriceBps": 20000,
	}
	for _, a := range EffectiveAssumptions(&sc) {
		if v, ok := want[a.Path]; ok {
			if a.Value != v || a.Default != v || a.Overridden || a.Rationale == "" {
				t.Errorf("%s = %+v", a.Path, a)
			}
			delete(want, a.Path)
		}
	}
	if len(want) != 0 {
		t.Errorf("not listed: %v", want)
	}
}

func TestAdjustNoiseAmplitude(t *testing.T) {
	sc := adjusted(t, map[string]int64{"demand.noiseBps": 0})
	st := mustInit(t, sc, 2026)
	for i := 0; i < 24; i++ {
		var j []Outcome
		st, j = mustStep(t, sc, st)
		for _, o := range j {
			if o.Kind != OutcomeDemand {
				continue
			}
			exact := MulDiv(sc.Demand.BaseArrivalsPerTick, int64(sc.Demand.HourlyProfile[hourOfDay(o.At)]), BpsScale)
			if o.Qty != exact {
				t.Fatalf("tick %d: potential arrivals %d, want exactly %d", i+1, o.Qty, exact)
			}
		}
	}
	// With the default amplitude the same draws are not all exact.
	def := loadFlagship(t)
	var differ bool
	for draw := uint64(0); draw < 4001; draw += 500 {
		if potentialArrivals(def.Demand, 18, BpsScale, draw) != potentialArrivals(sc.Demand, 18, BpsScale, draw) {
			differ = true
		}
	}
	if !differ {
		t.Error("default noise amplitude produces no spread")
	}
}

func TestAdjustPriceBounds(t *testing.T) {
	sc := adjusted(t, map[string]int64{"pricing.minPriceBps": 8000, "pricing.maxPriceBps": 12000})
	st := mustInit(t, sc, 2026)
	obs := Observe(sc, st)
	o0, _ := obs.sku(0)
	ref := o0.ReferencePriceCents
	lo, hi := MulDiv(int64(ref), 8000, BpsScale), MulDiv(int64(ref), 12000, BpsScale)
	if int64(o0.MinPriceCents) != lo || int64(o0.MaxPriceCents) != hi {
		t.Fatalf("observation bounds %d..%d, want %d..%d", o0.MinPriceCents, o0.MaxPriceCents, lo, hi)
	}
	price := func(c int64) RejectCode {
		return Validate(sc, obs, []Command{{Kind: CmdSetPrice, SKU: 0, AmountCents: Cents(c)}}, 5)[0].Code
	}
	if price(lo) != "" || price(hi) != "" {
		t.Error("bounds themselves must be accepted")
	}
	if price(lo-1) != ReasonPriceOutOfBounds || price(hi+1) != ReasonPriceOutOfBounds {
		t.Error("prices outside the adjusted bounds must be rejected")
	}
	// 60% of reference was fine by default and is now out of bounds.
	if price(MulDiv(int64(ref), 6000, BpsScale)) != ReasonPriceOutOfBounds {
		t.Error("60% should be out of bounds")
	}
	loose := loadFlagship(t)
	if c := Validate(loose, Observe(loose, mustInit(t, loose, 2026)),
		[]Command{{Kind: CmdSetPrice, SKU: 0, AmountCents: Cents(MulDiv(int64(ref), 6000, BpsScale))}}, 5)[0].Code; c != "" {
		t.Errorf("default bounds rejected 60%%: %q", c)
	}

	base := loadFlagship(t)
	if _, err := ApplyOverridesAndValidate(&base, map[string]int64{"pricing.minPriceBps": 10000, "pricing.maxPriceBps": 10000 - 1}); err == nil {
		t.Error("inverted bounds accepted")
	}
	var ve *ScenarioValidationError
	_, err := ApplyOverridesAndValidate(&base, map[string]int64{"pricing.maxPriceBps": 5000})
	if !errors.As(err, &ve) {
		t.Errorf("max below its minimum bound: err = %v", err)
	}
}

func TestAdjustMarketingUnit(t *testing.T) {
	def := loadFlagship(t)
	half := adjusted(t, map[string]int64{"demand.marketingSpendUnitCents": 5000})
	sku := def.SKUs[0]
	base := conversionBps(def.Demand, sku, sku.ReferencePriceCents, 0)
	if got := conversionBps(def.Demand, sku, sku.ReferencePriceCents, 10000) - base; got != 300 {
		t.Errorf("default uplift = %d, want 300", got)
	}
	if got := conversionBps(half.Demand, sku, sku.ReferencePriceCents, 10000) - base; got != 600 {
		t.Errorf("half-unit uplift = %d, want 600", got)
	}
}
