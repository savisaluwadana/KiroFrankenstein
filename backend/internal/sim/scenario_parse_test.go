package sim

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// validScenarioDoc lists fields in Scenario struct order so that marshalling
// a parsed value reproduces the compacted document byte for byte.
const validScenarioDoc = `{
  "schemaVersion": 1,
  "name": "Black Friday Meltdown",
  "horizonTicks": 120,
  "startClock": "Thu 00:00",
  "decisionCadenceTicks": 4,
  "commandBudget": 5,
  "snapshotIntervalTicks": 24,
  "tieToleranceHundredths": 10,
  "scoreWeights": {"financialWeightBps": 5000, "fulfilmentWeightBps": 3000, "onTimeWeightBps": 2000},
  "profitFloorCents": -5000000,
  "profitTargetCents": 20000000,
  "openingCashCents": 10000000,
  "skus": [{"id": "SKU1", "referencePriceCents": 2999, "openingUnits": 500, "openingUnitCostCents": 1500, "promiseTicks": 24}],
  "suppliers": [
    {"skuId": "SKU1", "role": "primary", "unitCostCents": 1500, "leadTimeTicks": 12},
    {"skuId": "SKU1", "role": "backup", "unitCostCents": 1900, "leadTimeTicks": 6}
  ],
  "workforce": {"regularWorkers": 20, "unitsPerWorkerHour": 8, "maxOvertimePerWorker": 4, "maxTempWorkers": 30, "tempOnboardingTicks": 6},
  "wages": {"regularPerHourCents": 2200, "overtimePerHourCents": 3300, "tempPerHourCents": 2500},
  "costs": {"outboundShipCentsPerUnit": 400, "expediteFeeCents": 5000, "expediteReductionTicks": 4, "hourlyOverheadCents": 120000, "serviceCreditPerLateOrderCents": 500},
  "demand": {"hourlyProfile": [100, 200, 300], "baseArrivalsPerTick": 60, "baseConversionBps": 5000, "priceElasticityBps": 1500, "marketingEffectBps": 300, "cancelThresholdTicks": 48},
  "assumptionMeta": {
    "wages.tempPerHourCents": {"unit": "cents/hour", "default": 2500, "min": 1000, "max": 6000, "rationale": "Agency rate"},
    "workforce.tempOnboardingTicks": {"unit": "ticks", "default": 6, "min": 0, "max": 24, "rationale": "Hiring lag"}
  },
  "disruptionScript": [
    {"kind": "demand_surge", "onsetTick": 48, "durationTicks": 24, "magnitudeBps": 25000},
    {"kind": "supplier_outage", "onsetTick": 60, "durationTicks": 18, "target": "primary"}
  ]
}`

// mutate replaces old with new in the valid document; it fails if old is absent.
func mutate(t *testing.T, old, repl string) []byte {
	t.Helper()
	if !strings.Contains(validScenarioDoc, old) {
		t.Fatalf("test bug: %q not in document", old)
	}
	return []byte(strings.Replace(validScenarioDoc, old, repl, 1))
}

func TestScenarioParseValid(t *testing.T) {
	s, err := ParseScenario([]byte(validScenarioDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Name != "Black Friday Meltdown" || s.HorizonTicks != 120 || s.ProfitFloorCents != -5000000 ||
		s.TieToleranceHundredths != 10 || s.ScoreWeights.OnTimeWeightBps != 2000 ||
		s.Workforce.TempOnboardingTicks != 6 || s.Wages.TempPerHourCents != 2500 {
		t.Fatalf("unexpected values: %+v", s)
	}
	if m := s.AssumptionMeta["workforce.tempOnboardingTicks"]; m.Unit != "ticks" || m.Max != 24 || m.Rationale == "" {
		t.Fatalf("bad meta: %+v", m)
	}
	d := s.DisruptionScript
	if len(d) != 2 || d[0].MagnitudeBps == nil || *d[0].MagnitudeBps != 25000 || d[0].Target != nil ||
		d[1].Target == nil || *d[1].Target != "primary" || d[1].MagnitudeBps != nil {
		t.Fatalf("bad disruption script: %+v", d)
	}
}

// Keeps the path-checking schema and the Scenario types in sync: every field
// of the document must survive a parse and re-marshal unchanged.
func TestScenarioParseSchemaMatchesTypes(t *testing.T) {
	s, err := ParseScenario([]byte(validScenarioDoc))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, []byte(validScenarioDoc)); err != nil {
		t.Fatal(err)
	}
	// AssumptionMeta is a map, which json.Marshal sorts; the document keys are
	// already in sorted order.
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("round trip mismatch\n got: %s\nwant: %s", got, want.Bytes())
	}
}

func TestScenarioParseErrors(t *testing.T) {
	tests := []struct {
		name string
		doc  []byte
		path string
		frag string
	}{
		{"unknown top-level", mutate(t, `"name":`, `"bogus": 1, "name":`), "bogus", "unknown field"},
		{"unknown nested", mutate(t, `"promiseTicks": 24`, `"promiseTicks": 24, "extra": 1`), "skus[0].extra", "unknown field"},
		{"missing top-level", mutate(t, `"commandBudget": 5,`, ``), "commandBudget", "missing required"},
		{"missing nested", mutate(t, `"tempOnboardingTicks": 6`, `"x": 1`), "workforce.x", "unknown field"},
		{"missing nested required", mutate(t, `, "cancelThresholdTicks": 48`, ``), "demand.cancelThresholdTicks", "missing required"},
		{"wrong type string", mutate(t, `"name": "Black Friday Meltdown"`, `"name": 5`), "name", "expected string"},
		{"wrong type number", mutate(t, `"horizonTicks": 120`, `"horizonTicks": "120"`), "horizonTicks", "expected integer"},
		{"null required", mutate(t, `"horizonTicks": 120`, `"horizonTicks": null`), "horizonTicks", "expected integer"},
		{"float", mutate(t, `"referencePriceCents": 2999`, `"referencePriceCents": 29.99`), "skus[0].referencePriceCents", "non-integer"},
		{"exponent", mutate(t, `"horizonTicks": 120`, `"horizonTicks": 1.2e2`), "horizonTicks", "non-integer"},
		{"float in array", mutate(t, `[100, 200, 300]`, `[100, 200.5, 300]`), "demand.hourlyProfile[1]", "non-integer"},
		{"float in meta", mutate(t, `"min": 1000`, `"min": 1000.0`), "assumptionMeta.wages.tempPerHourCents.min", "non-integer"},
		{"out of range", mutate(t, `"horizonTicks": 120`, `"horizonTicks": 99999999999999999999`), "horizonTicks", "out of range"},
		{"wrong disruption type", mutate(t, `"onsetTick": 60`, `"onsetTick": true`), "disruptionScript[1].onsetTick", "expected integer"},
		{"array expected", mutate(t, `"skus": [{"id": "SKU1", "referencePriceCents": 2999, "openingUnits": 500, "openingUnitCostCents": 1500, "promiseTicks": 24}]`, `"skus": 7`), "skus", "expected array"},
		{"object expected", mutate(t, `"scoreWeights": {"financialWeightBps": 5000, "fulfilmentWeightBps": 3000, "onTimeWeightBps": 2000}`, `"scoreWeights": []`), "scoreWeights", "expected object"},
		{"first offender in order", mutate(t, `"horizonTicks": 120`, `"horizonTicks": "x", "zzz": 1`), "zzz", "unknown field"},
		{"missing name reported before later wrong type", mutate(t, `"name": "Black Friday Meltdown",`, `"startClock": 7,`), "name", "missing required"},
		{"top-level not object", []byte(`[1]`), "", "expected object"},
		{"invalid JSON", []byte(`{"schemaVersion": `), "", "invalid JSON"},
		{"trailing data", []byte(validScenarioDoc + ` {}`), "", "after top-level"},
		{"schemaVersion missing", mutate(t, `"schemaVersion": 1,`, ``), "schemaVersion", "missing required"},
		{"schemaVersion wrong type", mutate(t, `"schemaVersion": 1`, `"schemaVersion": "1"`), "schemaVersion", "expected integer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScenario(tc.doc)
			if err == nil {
				t.Fatal("expected error")
			}
			var pe *ScenarioParseError
			if !errors.As(err, &pe) {
				t.Fatalf("want *ScenarioParseError, got %T: %v", err, err)
			}
			if pe.Path != tc.path {
				t.Errorf("path = %q, want %q (err: %v)", pe.Path, tc.path, err)
			}
			if !strings.Contains(err.Error(), tc.frag) {
				t.Errorf("error %q missing %q", err, tc.frag)
			}
			if tc.path != "" && !strings.Contains(err.Error(), tc.path) {
				t.Errorf("error %q does not name path %q", err, tc.path)
			}
		})
	}
}

func TestScenarioParseUnsupportedVersion(t *testing.T) {
	for _, v := range []string{"2", "0", "-1"} {
		doc := mutate(t, `"schemaVersion": 1`, `"schemaVersion": `+v)
		_, err := ParseScenario(doc)
		if !errors.Is(err, ErrUnsupportedSchemaVersion) {
			t.Errorf("version %s: want ErrUnsupportedSchemaVersion, got %v", v, err)
		}
	}
	// A future-version document with otherwise unknown shape still reports the version.
	_, err := ParseScenario([]byte(`{"schemaVersion": 2, "brandNew": true}`))
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Errorf("want ErrUnsupportedSchemaVersion, got %v", err)
	}
}
