package sim

import (
	"errors"
	"strings"
	"testing"
)

// validScenario builds a valid Scenario whose metadata is generated from the
// Assumption table, with wide bounds.
func validScenario() *Scenario {
	s := &Scenario{
		SchemaVersion:          1,
		Name:                   "t",
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
		DisruptionScript:       []DisruptionSpec{},
	}
	for i := 0; i < HourlyProfileLen; i++ {
		s.Demand.HourlyProfile = append(s.Demand.HourlyProfile, 10000)
	}
	s.AssumptionMeta = map[string]AssumptionMeta{}
	for _, r := range assumptionRefs(s) {
		unit := "count"
		min, max := int64(0), int64(1_000_000)
		switch r.kind {
		case akMoney:
			unit = "cents"
		case akRate:
			unit = "bps"
			max = BpsScale
			if r.path == "pricing.maxPriceBps" {
				max = 100000
			}
		}
		s.AssumptionMeta[r.path] = AssumptionMeta{Unit: unit, Default: r.get(), Min: min, Max: max, Rationale: "synthetic, illustrative"}
	}
	s.AssumptionMeta[hourlyProfilePath] = AssumptionMeta{Unit: "bps", Default: 10000, Min: 0, Max: 30000, Rationale: "synthetic, illustrative"}
	return s
}

func TestScenarioValidateValid(t *testing.T) {
	if err := ValidateScenario(validScenario()); err != nil {
		t.Fatalf("valid scenario rejected: %v", err)
	}
}

func TestScenarioValidateRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(s *Scenario)
		want   []string // substrings that must all appear in the error
	}{
		{"value below min", func(s *Scenario) { s.Workforce.RegularWorkers = -3 },
			[]string{"workforce.regularWorkers", "-3", "[0, 1000000]"}},
		{"value above max", func(s *Scenario) {
			m := s.AssumptionMeta["horizonTicks"]
			m.Max = 100
			s.AssumptionMeta["horizonTicks"] = m
		}, []string{"horizonTicks", "120", "[0, 100]"}},
		{"sku assumption bound", func(s *Scenario) { s.SKUs[0].OpeningUnits = 2_000_000 },
			[]string{"skus.a.openingUnits", "2000000", "[0, 1000000]"}},
		{"hourly profile element", func(s *Scenario) { s.Demand.HourlyProfile[5] = 40000 },
			[]string{"demand.hourlyProfile[5]", "40000", "[0, 30000]"}},
		{"hourly profile length", func(s *Scenario) { s.Demand.HourlyProfile = s.Demand.HourlyProfile[:23] },
			[]string{"demand.hourlyProfile", "23", "24"}},
		{"money unit not cents", func(s *Scenario) {
			m := s.AssumptionMeta["wages.regularPerHourCents"]
			m.Unit = "dollars"
			s.AssumptionMeta["wages.regularPerHourCents"] = m
		}, []string{"wages.regularPerHourCents", "cents", "dollars"}},
		{"rate unit not bps", func(s *Scenario) {
			m := s.AssumptionMeta["demand.priceElasticityBps"]
			m.Unit = "percent"
			s.AssumptionMeta["demand.priceElasticityBps"] = m
		}, []string{"demand.priceElasticityBps", "bps", "percent"}},
		{"missing metadata", func(s *Scenario) { delete(s.AssumptionMeta, "commandBudget") },
			[]string{"commandBudget", "missing assumption metadata"}},
		{"empty unit", func(s *Scenario) {
			m := s.AssumptionMeta["commandBudget"]
			m.Unit = ""
			s.AssumptionMeta["commandBudget"] = m
		}, []string{"commandBudget", "unit is empty"}},
		{"empty rationale", func(s *Scenario) {
			m := s.AssumptionMeta["commandBudget"]
			m.Rationale = " "
			s.AssumptionMeta["commandBudget"] = m
		}, []string{"commandBudget", "rationale is empty"}},
		{"default outside bounds", func(s *Scenario) {
			m := s.AssumptionMeta["commandBudget"]
			m.Default = 2_000_000
			s.AssumptionMeta["commandBudget"] = m
		}, []string{"commandBudget", "default 2000000", "[0, 1000000]"}},
		{"min above max", func(s *Scenario) {
			m := s.AssumptionMeta["commandBudget"]
			m.Min, m.Max = 9, 3
			s.AssumptionMeta["commandBudget"] = m
		}, []string{"commandBudget", "minimum 9", "maximum 3"}},
		{"metadata for non-assumption", func(s *Scenario) {
			s.AssumptionMeta["nope.field"] = AssumptionMeta{Unit: "x", Rationale: "r"}
		}, []string{"nope.field", "not an assumption"}},
		{"score weights sum", func(s *Scenario) { s.ScoreWeights.OnTimeWeightBps = 1000 },
			[]string{"scoreWeights", "5000", "3000", "1000", "= 9000", "10000"}},
		{"score weight out of range", func(s *Scenario) {
			s.ScoreWeights = ScoreWeights{12000, -2000, 0}
		}, []string{"scoreWeights.financialWeightBps", "12000", "[0, 10000]", "scoreWeights.fulfilmentWeightBps", "-2000"}},
		{"target equals floor", func(s *Scenario) { s.ProfitTargetCents = s.ProfitFloorCents },
			[]string{"profitTargetCents", "greater than profitFloorCents"}},
		{"target below floor", func(s *Scenario) { s.ProfitFloorCents, s.ProfitTargetCents = 500, 100 },
			[]string{"profitTargetCents", "100", "> 500"}},
		{"tie tolerance high", func(s *Scenario) { s.TieToleranceHundredths = 10001 },
			[]string{"tieToleranceHundredths", "10001", "[0, 10000]"}},
		{"tie tolerance negative", func(s *Scenario) { s.TieToleranceHundredths = -1 },
			[]string{"tieToleranceHundredths", "-1", "[0, 10000]"}},
		{"duplicate sku", func(s *Scenario) { s.SKUs = append(s.SKUs, s.SKUs[0]) },
			[]string{"skus[1].id", "duplicate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validScenario()
			tc.mutate(s)
			err := ValidateScenario(s)
			var ve *ScenarioValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want *ScenarioValidationError, got %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err.Error(), w)
				}
			}
		})
	}
}

func TestScenarioValidateOverrides(t *testing.T) {
	base := validScenario()

	got, err := ApplyOverridesAndValidate(base, map[string]int64{
		"workforce.tempOnboardingTicks": 9,
		"skus.a.openingUnits":           70,
		"profitTargetCents":             250000,
	})
	if err != nil {
		t.Fatalf("valid overrides rejected: %v", err)
	}
	if got.Workforce.TempOnboardingTicks != 9 || got.SKUs[0].OpeningUnits != 70 || got.ProfitTargetCents != 250000 {
		t.Fatalf("overrides not applied: %+v", got)
	}
	if base.Workforce.TempOnboardingTicks != 6 || base.SKUs[0].OpeningUnits != 50 {
		t.Fatal("base scenario was modified")
	}

	// Out-of-bounds override is rejected after being applied.
	got, err = ApplyOverridesAndValidate(base, map[string]int64{"workforce.regularWorkers": 5_000_000})
	if got != nil || err == nil {
		t.Fatalf("want rejection, got %v, %v", got, err)
	}
	for _, w := range []string{"workforce.regularWorkers", "5000000", "[0, 1000000]"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q missing %q", err.Error(), w)
		}
	}

	// Cross-field rule is checked against the overridden value.
	if _, err = ApplyOverridesAndValidate(base, map[string]int64{"profitTargetCents": -5}); err == nil {
		t.Fatal("override making target <= floor must fail")
	}

	// Unknown path.
	if _, err = ApplyOverridesAndValidate(base, map[string]int64{"bogus": 1}); !errors.Is(err, ErrUnknownAssumption) {
		t.Fatalf("want ErrUnknownAssumption, got %v", err)
	}
}
