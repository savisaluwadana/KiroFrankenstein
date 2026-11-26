package sim

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// HourlyProfileLen is the required length of demand.hourlyProfile (one value
// per hour of day).
const HourlyProfileLen = 24

// ValidationIssue is one validation failure. Assumption names the offending
// Assumption (dotted path) or rule subject (for example "scoreWeights").
type ValidationIssue struct {
	Assumption string
	Msg        string
}

func (i ValidationIssue) String() string { return i.Assumption + ": " + i.Msg }

// ScenarioValidationError carries every issue found, in a deterministic order.
type ScenarioValidationError struct {
	Issues []ValidationIssue
}

func (e *ScenarioValidationError) Error() string {
	parts := make([]string, len(e.Issues))
	for i, is := range e.Issues {
		parts[i] = is.String()
	}
	return "scenario validation failed: " + strings.Join(parts, "; ")
}

// ErrUnknownAssumption is wrapped by ApplyOverrides for a path that names no Assumption.
var ErrUnknownAssumption = errors.New("unknown assumption")

type assumptionKind uint8

const (
	akPlain assumptionKind = iota
	akMoney                // unit must be "cents"
	akRate                 // unit must be "bps"
)

// assumptionRef addresses one numeric Assumption of a Scenario.
type assumptionRef struct {
	path string
	kind assumptionKind
	get  func() int64
	set  func(int64)
}

func refInt(path string, p *int64) assumptionRef {
	return assumptionRef{path, akPlain, func() int64 { return *p }, func(v int64) { *p = v }}
}

func refCents(path string, p *Cents) assumptionRef {
	return assumptionRef{path, akMoney, func() int64 { return int64(*p) }, func(v int64) { *p = Cents(v) }}
}

func refBps(path string, p *Bps) assumptionRef {
	return assumptionRef{path, akRate, func() int64 { return int64(*p) }, func(v int64) { *p = Bps(v) }}
}

// assumptionRefs lists every numeric Assumption in a fixed order. Per-SKU and
// per-supplier Assumptions are keyed by SKU id (and role), for example
// "skus.widget.openingUnits" and "suppliers.widget.primary.leadTimeTicks".
// demand.hourlyProfile is one Assumption whose metadata applies to every
// element. schemaVersion is not an Assumption.
func assumptionRefs(s *Scenario) []assumptionRef {
	refs := []assumptionRef{
		refInt("horizonTicks", &s.HorizonTicks),
		refInt("decisionCadenceTicks", &s.DecisionCadenceTicks),
		refInt("commandBudget", &s.CommandBudget),
		refInt("snapshotIntervalTicks", &s.SnapshotIntervalTicks),
		refInt("tieToleranceHundredths", &s.TieToleranceHundredths),
		refBps("scoreWeights.financialWeightBps", &s.ScoreWeights.FinancialWeightBps),
		refBps("scoreWeights.fulfilmentWeightBps", &s.ScoreWeights.FulfilmentWeightBps),
		refBps("scoreWeights.onTimeWeightBps", &s.ScoreWeights.OnTimeWeightBps),
		refCents("profitFloorCents", &s.ProfitFloorCents),
		refCents("profitTargetCents", &s.ProfitTargetCents),
		refCents("openingCashCents", &s.OpeningCashCents),
		refInt("workforce.regularWorkers", &s.Workforce.RegularWorkers),
		refInt("workforce.unitsPerWorkerHour", &s.Workforce.UnitsPerWorkerHour),
		refInt("workforce.maxOvertimePerWorker", &s.Workforce.MaxOvertimePerWorker),
		refInt("workforce.maxTempWorkers", &s.Workforce.MaxTempWorkers),
		refInt("workforce.tempOnboardingTicks", &s.Workforce.TempOnboardingTicks),
		refCents("wages.regularPerHourCents", &s.Wages.RegularPerHourCents),
		refCents("wages.overtimePerHourCents", &s.Wages.OvertimePerHourCents),
		refCents("wages.tempPerHourCents", &s.Wages.TempPerHourCents),
		refCents("costs.outboundShipCentsPerUnit", &s.Costs.OutboundShipCentsPerUnit),
		refCents("costs.expediteFeeCents", &s.Costs.ExpediteFeeCents),
		refInt("costs.expediteReductionTicks", &s.Costs.ExpediteReductionTicks),
		refCents("costs.hourlyOverheadCents", &s.Costs.HourlyOverheadCents),
		refCents("costs.serviceCreditPerLateOrderCents", &s.Costs.ServiceCreditPerLateOrderCents),
		refInt("demand.baseArrivalsPerTick", &s.Demand.BaseArrivalsPerTick),
		refBps("demand.baseConversionBps", &s.Demand.BaseConversionBps),
		refBps("demand.priceElasticityBps", &s.Demand.PriceElasticityBps),
		refBps("demand.marketingEffectBps", &s.Demand.MarketingEffectBps),
		refInt("demand.cancelThresholdTicks", &s.Demand.CancelThresholdTicks),
	}
	for i := range s.SKUs {
		k := &s.SKUs[i]
		p := "skus." + k.ID + "."
		refs = append(refs,
			refCents(p+"referencePriceCents", &k.ReferencePriceCents),
			refInt(p+"openingUnits", &k.OpeningUnits),
			refCents(p+"openingUnitCostCents", &k.OpeningUnitCostCents),
			refInt(p+"promiseTicks", &k.PromiseTicks),
		)
	}
	for i := range s.Suppliers {
		u := &s.Suppliers[i]
		p := "suppliers." + u.SKUID + "." + u.Role + "."
		refs = append(refs,
			refCents(p+"unitCostCents", &u.UnitCostCents),
			refInt(p+"leadTimeTicks", &u.LeadTimeTicks),
		)
	}
	return refs
}

// hourlyProfilePath is the Assumption path covering demand.hourlyProfile.
const hourlyProfilePath = "demand.hourlyProfile"

// clone returns a deep copy of s.
func (s *Scenario) clone() *Scenario {
	c := *s
	c.SKUs = append([]SKUSpec(nil), s.SKUs...)
	c.Suppliers = append([]SupplierSpec(nil), s.Suppliers...)
	c.Demand.HourlyProfile = append([]Bps(nil), s.Demand.HourlyProfile...)
	if s.AssumptionMeta != nil {
		c.AssumptionMeta = make(map[string]AssumptionMeta, len(s.AssumptionMeta))
		for k, v := range s.AssumptionMeta {
			c.AssumptionMeta[k] = v
		}
	}
	c.DisruptionScript = make([]DisruptionSpec, len(s.DisruptionScript))
	for i, d := range s.DisruptionScript {
		if d.MagnitudeBps != nil {
			v := *d.MagnitudeBps
			d.MagnitudeBps = &v
		}
		if d.Target != nil {
			v := *d.Target
			d.Target = &v
		}
		c.DisruptionScript[i] = d
	}
	return &c
}

// ApplyOverrides returns a copy of s with each overridden Assumption set to
// its override value (Req 4.4). Keys are Assumption paths such as
// "workforce.tempOnboardingTicks". An unknown path wraps ErrUnknownAssumption.
// The result is not validated; use ValidateScenario or
// ApplyOverridesAndValidate. s is never modified.
func ApplyOverrides(s *Scenario, overrides map[string]int64) (*Scenario, error) {
	c := s.clone()
	refs := assumptionRefs(c)
	byPath := make(map[string]assumptionRef, len(refs))
	for _, r := range refs {
		byPath[r.path] = r
	}
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r, ok := byPath[k]
		if !ok {
			return nil, fmt.Errorf("override %q: %w", k, ErrUnknownAssumption)
		}
		r.set(overrides[k])
	}
	return c, nil
}

// ApplyOverridesAndValidate applies overrides and then validates the result
// (Req 4.4, 4.5). On failure it returns a nil Scenario and the error
// (a *ScenarioValidationError for validation failures).
func ApplyOverridesAndValidate(s *Scenario, overrides map[string]int64) (*Scenario, error) {
	c, err := ApplyOverrides(s, overrides)
	if err != nil {
		return nil, err
	}
	if err := ValidateScenario(c); err != nil {
		return nil, err
	}
	return c, nil
}

func rangeText(min, max int64) string { return fmt.Sprintf("[%d, %d]", min, max) }

// ValidateScenario checks a parsed Scenario against its own Assumption
// metadata and the cross-field rules (Req 1.3, 1.4, 1.6-1.9). It returns nil
// or a *ScenarioValidationError listing every issue in a deterministic order.
// Integer-ness of cents and basis points is enforced by the field types; the
// validator additionally requires monetary Assumptions to declare unit
// "cents" and rate Assumptions to declare unit "bps".
func ValidateScenario(s *Scenario) error {
	var issues []ValidationIssue
	add := func(a, format string, args ...any) {
		issues = append(issues, ValidationIssue{Assumption: a, Msg: fmt.Sprintf(format, args...)})
	}

	// Identifier uniqueness: Assumption paths for SKUs and suppliers depend on it.
	skuSeen := map[string]bool{}
	for i, k := range s.SKUs {
		switch {
		case k.ID == "":
			add(fmt.Sprintf("skus[%d].id", i), "must not be empty")
		case skuSeen[k.ID]:
			add(fmt.Sprintf("skus[%d].id", i), "duplicate sku id %q", k.ID)
		}
		skuSeen[k.ID] = true
	}
	supSeen := map[string]bool{}
	for i, u := range s.Suppliers {
		key := u.SKUID + "\x00" + u.Role
		switch {
		case u.Role == "":
			add(fmt.Sprintf("suppliers[%d].role", i), "must not be empty")
		case supSeen[key]:
			add(fmt.Sprintf("suppliers[%d]", i), "duplicate supplier role %q for sku %q", u.Role, u.SKUID)
		}
		supSeen[key] = true
		if !skuSeen[u.SKUID] {
			add(fmt.Sprintf("suppliers[%d].skuId", i), "unknown sku %q", u.SKUID)
		}
	}

	refs := assumptionRefs(s)
	known := make(map[string]bool, len(refs)+1)
	checkMeta := func(path string, kind assumptionKind) (AssumptionMeta, bool) {
		known[path] = true
		m, ok := s.AssumptionMeta[path]
		if !ok {
			add(path, "missing assumption metadata (unit, default, min, max, rationale)")
			return m, false
		}
		good := true
		if strings.TrimSpace(m.Unit) == "" {
			add(path, "metadata unit is empty")
			good = false
		}
		if strings.TrimSpace(m.Rationale) == "" {
			add(path, "metadata rationale is empty")
			good = false
		}
		if m.Min > m.Max {
			add(path, "metadata minimum %d exceeds maximum %d", m.Min, m.Max)
			good = false
		} else if m.Default < m.Min || m.Default > m.Max {
			add(path, "metadata default %d outside allowed range %s", m.Default, rangeText(m.Min, m.Max))
			good = false
		}
		if kind == akMoney && m.Unit != "" && m.Unit != "cents" {
			add(path, "monetary assumption must have unit \"cents\", got %q", m.Unit)
			good = false
		}
		if kind == akRate && m.Unit != "" && m.Unit != "bps" {
			add(path, "rate assumption must have unit \"bps\", got %q", m.Unit)
			good = false
		}
		return m, good
	}

	for _, r := range refs {
		m, ok := checkMeta(r.path, r.kind)
		if !ok {
			continue
		}
		if v := r.get(); v < m.Min || v > m.Max {
			add(r.path, "value %d outside allowed range %s", v, rangeText(m.Min, m.Max))
		}
	}

	// demand.hourlyProfile: fixed length, one metadata entry for all elements.
	if len(s.Demand.HourlyProfile) != HourlyProfileLen {
		add(hourlyProfilePath, "length %d, must be %d (one value per hour of day)",
			len(s.Demand.HourlyProfile), HourlyProfileLen)
	}
	if m, ok := checkMeta(hourlyProfilePath, akRate); ok {
		for i, v := range s.Demand.HourlyProfile {
			if int64(v) < m.Min || int64(v) > m.Max {
				add(fmt.Sprintf("%s[%d]", hourlyProfilePath, i), "value %d outside allowed range %s",
					int64(v), rangeText(m.Min, m.Max))
			}
		}
	}

	// Metadata for something that is not an Assumption.
	extra := make([]string, 0)
	for k := range s.AssumptionMeta {
		if !known[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		add(k, "metadata given for a path that is not an assumption")
	}

	// Req 1.8: Score_Weights.
	w := s.ScoreWeights
	for _, p := range []struct {
		name string
		v    Bps
	}{
		{"scoreWeights.financialWeightBps", w.FinancialWeightBps},
		{"scoreWeights.fulfilmentWeightBps", w.FulfilmentWeightBps},
		{"scoreWeights.onTimeWeightBps", w.OnTimeWeightBps},
	} {
		if p.v < 0 || p.v > BpsScale {
			add(p.name, "value %d outside allowed range %s", int64(p.v), rangeText(0, BpsScale))
		}
	}
	if sum := int64(w.FinancialWeightBps) + int64(w.FulfilmentWeightBps) + int64(w.OnTimeWeightBps); sum != BpsScale {
		add("scoreWeights", "financialWeightBps %d + fulfilmentWeightBps %d + onTimeWeightBps %d = %d, must sum to exactly %d",
			int64(w.FinancialWeightBps), int64(w.FulfilmentWeightBps), int64(w.OnTimeWeightBps), sum, BpsScale)
	}

	// Req 1.9: Profit_Target > Profit_Floor; Tie_Tolerance in 0..10000.
	if s.ProfitTargetCents <= s.ProfitFloorCents {
		add("profitTargetCents", "value %d must be greater than profitFloorCents %d (allowed range: > %d)",
			int64(s.ProfitTargetCents), int64(s.ProfitFloorCents), int64(s.ProfitFloorCents))
	}
	if s.TieToleranceHundredths < 0 || s.TieToleranceHundredths > BpsScale {
		add("tieToleranceHundredths", "value %d outside allowed range %s", s.TieToleranceHundredths, rangeText(0, BpsScale))
	}

	if len(issues) == 0 {
		return nil
	}
	return &ScenarioValidationError{Issues: issues}
}
