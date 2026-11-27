package sim

import (
	"os"
	"strings"
	"testing"
)

// The flagship's disruption script (demand_surge, workforce_absence,
// supplier_outage) is run by the engine (see disruption_kinds_test.go). The
// script is not an Assumption and is not in assumptionMeta.

func TestFlagshipScenario(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseScenario(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ValidateScenario(s); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if s.HorizonTicks != 120 || s.StartClock != "Thu 00:00" {
		t.Errorf("horizon/start = %d %q", s.HorizonTicks, s.StartClock)
	}
	if len(s.SKUs) != 4 {
		t.Errorf("skus = %d, want 4", len(s.SKUs))
	}
	roles := map[string]map[string]SupplierSpec{}
	for _, u := range s.Suppliers {
		if roles[u.SKUID] == nil {
			roles[u.SKUID] = map[string]SupplierSpec{}
		}
		roles[u.SKUID][u.Role] = u
	}
	for _, k := range s.SKUs {
		p, okP := roles[k.ID]["primary"]
		b, okB := roles[k.ID]["backup"]
		if !okP || !okB {
			t.Errorf("sku %s lacks primary/backup supplier", k.ID)
			continue
		}
		if !(p.UnitCostCents < b.UnitCostCents && p.LeadTimeTicks > b.LeadTimeTicks) {
			t.Errorf("sku %s: primary must be cheaper and slower than backup", k.ID)
		}
	}
	if s.DecisionCadenceTicks != 4 || s.CommandBudget != 5 || s.TieToleranceHundredths != 10 {
		t.Errorf("cadence/budget/tie = %d/%d/%d", s.DecisionCadenceTicks, s.CommandBudget, s.TieToleranceHundredths)
	}
	if w := s.ScoreWeights; w.FinancialWeightBps != 5000 || w.FulfilmentWeightBps != 3000 || w.OnTimeWeightBps != 2000 {
		t.Errorf("weights = %+v", w)
	}
	for _, p := range []string{"profitFloorCents", "profitTargetCents"} {
		if r := s.AssumptionMeta[p].Rationale; !strings.Contains(r, "synthetic, illustrative; uncalibrated") {
			t.Errorf("%s rationale %q lacks the uncalibrated label", p, r)
		}
	}
	for _, p := range []string{"workforce.tempOnboardingTicks", "wages.tempPerHourCents"} {
		if _, ok := s.AssumptionMeta[p]; !ok {
			t.Errorf("missing %s metadata", p)
		}
	}
	for p, m := range s.AssumptionMeta {
		if !strings.Contains(m.Rationale, "synthetic, illustrative") && !strings.Contains(m.Rationale, "Source:") {
			t.Errorf("%s rationale %q is neither labelled synthetic nor cites a source", p, m.Rationale)
		}
	}

	onsets := map[string]int64{}
	for _, d := range s.DisruptionScript {
		onsets[d.Kind] = d.OnsetTick
		if d.OnsetTick <= 24 {
			t.Errorf("%s onset %d not after Tick 24", d.Kind, d.OnsetTick)
		}
	}
	want := map[string]int64{"demand_surge": 48, "workforce_absence": 54, "supplier_outage": 60}
	for k, v := range want {
		if onsets[k] != v {
			t.Errorf("%s onset = %d, want %d", k, onsets[k], v)
		}
	}
	if len(s.DisruptionScript) != 3 {
		t.Errorf("disruptions = %d, want 3", len(s.DisruptionScript))
	}
}
