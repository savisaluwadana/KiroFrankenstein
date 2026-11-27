package sim

import (
	"bytes"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// roundTripBase is a small valid scenario defined for this property test.
func roundTripBase() *Scenario {
	s := &Scenario{
		SchemaVersion:          1,
		Name:                   "base",
		HorizonTicks:           120,
		StartClock:             "Thu 00:00",
		DecisionCadenceTicks:   4,
		CommandBudget:          5,
		SnapshotIntervalTicks:  12,
		TieToleranceHundredths: 10,
		ScoreWeights:           ScoreWeights{5000, 3000, 2000},
		ProfitFloorCents:       0,
		ProfitTargetCents:      100000,
		OpeningCashCents:       1000000,
		SKUs:                   []SKUSpec{{ID: "a", ReferencePriceCents: 1000, OpeningUnits: 50, OpeningUnitCostCents: 400, PromiseTicks: 24}},
		Suppliers:              []SupplierSpec{{SKUID: "a", Role: "primary", UnitCostCents: 400, LeadTimeTicks: 12}},
		Workforce:              WorkforceSpec{10, 20, 2, 5, 6},
		Wages:                  WagesSpec{2000, 3000, 2500},
		Costs:                  CostsSpec{300, 500, 4, 100, 200},
		Demand:                 DemandSpec{BaseArrivalsPerTick: 60, BaseConversionBps: 5000, PriceElasticityBps: 1500, MarketingEffectBps: 300, CancelThresholdTicks: 48, NoiseBps: 2000, MarketingSpendUnitCents: 10000, MaxMarketingSpendCentsPerHour: 50000},
		Pricing:                PricingSpec{MinPriceBps: 5000, MaxPriceBps: 20000},
	}
	for i := 0; i < HourlyProfileLen; i++ {
		s.Demand.HourlyProfile = append(s.Demand.HourlyProfile, 10000)
	}
	return s
}

// genScenario draws a valid Scenario: integer assumptions vary within their
// metadata bounds, and the metadata, disruptions and score weights vary too.
func genScenario(t *rapid.T) *Scenario {
	s := roundTripBase()
	if rapid.Bool().Draw(t, "extraSKU") {
		s.SKUs = append(s.SKUs, SKUSpec{ID: "b", ReferencePriceCents: 500, OpeningUnits: 10, OpeningUnitCostCents: 100, PromiseTicks: 12})
		s.Suppliers = append(s.Suppliers, SupplierSpec{SKUID: "b", Role: "primary", UnitCostCents: 100, LeadTimeTicks: 6})
		if rapid.Bool().Draw(t, "backup") {
			s.Suppliers = append(s.Suppliers, SupplierSpec{SKUID: "a", Role: "backup", UnitCostCents: 600, LeadTimeTicks: 3})
		}
	}
	if rapid.Bool().Draw(t, "emptySKUs") {
		// Valid scenarios may have no SKUs or suppliers; exercises nil vs empty.
		s.SKUs, s.Suppliers = nil, nil
	}
	s.Name = rapid.StringMatching(`[A-Za-z0-9 _\-"\\/é]{0,24}`).Draw(t, "name")
	s.ProfitFloorCents = Cents(rapid.Int64Range(-1_000_000, 1_000_000).Draw(t, "floor"))
	s.ProfitTargetCents = s.ProfitFloorCents + Cents(rapid.Int64Range(1, 5_000_000).Draw(t, "targetGap"))
	s.TieToleranceHundredths = rapid.Int64Range(0, BpsScale).Draw(t, "tie")
	fin := rapid.Int64Range(0, BpsScale).Draw(t, "wFin")
	ful := rapid.Int64Range(0, BpsScale-fin).Draw(t, "wFul")
	s.ScoreWeights = ScoreWeights{Bps(fin), Bps(ful), Bps(BpsScale - fin - ful)}
	for i := range s.Demand.HourlyProfile {
		s.Demand.HourlyProfile[i] = Bps(rapid.Int64Range(0, 30000).Draw(t, "hour"))
	}

	// Vary every integer assumption within [min, max] of its kind.
	fixed := map[string]bool{
		"tieToleranceHundredths": true, "profitFloorCents": true, "profitTargetCents": true,
		"scoreWeights.financialWeightBps": true, "scoreWeights.fulfilmentWeightBps": true, "scoreWeights.onTimeWeightBps": true,
		"demand.marketingSpendUnitCents": true, "pricing.minPriceBps": true, "pricing.maxPriceBps": true,
	}
	s.AssumptionMeta = map[string]AssumptionMeta{}
	for _, r := range assumptionRefs(s) {
		unit, max := "count", int64(1_000_000)
		switch r.kind {
		case akMoney:
			unit = "cents"
		case akRate:
			unit, max = "bps", BpsScale
		}
		if fixed[r.path] {
			// Cross-field constrained: keep the value drawn above, widen the bounds.
			v := r.get()
			lo := v - rapid.Int64Range(0, 1000).Draw(t, r.path+".min")
			hi := v + rapid.Int64Range(0, 1000).Draw(t, r.path+".max")
			s.AssumptionMeta[r.path] = AssumptionMeta{Unit: unit, Default: v, Min: lo, Max: hi, Rationale: "synthetic, illustrative"}
			continue
		}
		v := rapid.Int64Range(0, max).Draw(t, r.path)
		r.set(v)
		lo := rapid.Int64Range(0, v).Draw(t, r.path+".min")
		hi := rapid.Int64Range(v, max).Draw(t, r.path+".max")
		s.AssumptionMeta[r.path] = AssumptionMeta{Unit: unit, Default: v, Min: lo, Max: hi, Rationale: "synthetic, illustrative"}
	}
	s.AssumptionMeta[hourlyProfilePath] = AssumptionMeta{Unit: "bps", Default: 10000, Min: 0, Max: 30000, Rationale: "synthetic, illustrative"}

	if n := rapid.IntRange(0, 3).Draw(t, "nDisruptions"); n > 0 {
		s.DisruptionScript = []DisruptionSpec{}
		for i := 0; i < n; i++ {
			d := DisruptionSpec{
				Kind:          rapid.SampledFrom([]string{"supplier_failure", "demand_spike", "workforce_shortage"}).Draw(t, "kind"),
				OnsetTick:     rapid.Int64Range(0, 119).Draw(t, "onset"),
				DurationTicks: rapid.Int64Range(1, 48).Draw(t, "dur"),
			}
			if rapid.Bool().Draw(t, "hasMag") {
				m := Bps(rapid.Int64Range(0, 30000).Draw(t, "mag"))
				d.MagnitudeBps = &m
			}
			if rapid.Bool().Draw(t, "hasTarget") {
				tg := rapid.StringMatching(`[a-z]{1,6}`).Draw(t, "target")
				d.Target = &tg
			}
			s.DisruptionScript = append(s.DisruptionScript, d)
		}
	}
	return s
}

// normalizeScenario maps nil collections to empty ones so that equality
// ignores the nil-vs-empty distinction, which JSON cannot preserve.
func normalizeScenario(s *Scenario) *Scenario {
	c := s.clone()
	if c.SKUs == nil {
		c.SKUs = []SKUSpec{}
	}
	if c.Suppliers == nil {
		c.Suppliers = []SupplierSpec{}
	}
	if c.Demand.HourlyProfile == nil {
		c.Demand.HourlyProfile = []Bps{}
	}
	if c.AssumptionMeta == nil {
		c.AssumptionMeta = map[string]AssumptionMeta{}
	}
	return c
}

// Feature: parallel-simulation-mvp, Property 1: Scenario round trip
// Validates: Requirements 2.2
func TestPropScenarioRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genScenario(t)
		if err := ValidateScenario(s); err != nil {
			t.Fatalf("generator produced invalid scenario: %v", err)
		}
		out := PrintScenario(s)
		back, err := ParseScenario(out)
		if err != nil {
			t.Fatalf("printed output does not parse: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(normalizeScenario(s), normalizeScenario(back)) {
			t.Fatalf("round trip differs:\n%+v\n%+v", s, back)
		}
		if again := PrintScenario(back); !bytes.Equal(out, again) {
			t.Fatalf("canonical print bytes differ:\n%s\n%s", out, again)
		}
	}) // rapid runs 100 checks by default
}
