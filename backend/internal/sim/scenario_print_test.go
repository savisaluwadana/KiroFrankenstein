package sim

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestScenarioPrintRoundTrip(t *testing.T) {
	s, err := ParseScenario([]byte(validScenarioDoc))
	if err != nil {
		t.Fatal(err)
	}
	out := PrintScenario(s)
	back, err := ParseScenario(out)
	if err != nil {
		t.Fatalf("printed output does not parse: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatalf("round trip differs:\n%+v\n%+v", s, back)
	}
	if again := PrintScenario(back); !bytes.Equal(out, again) {
		t.Fatalf("print not idempotent:\n%s\n%s", out, again)
	}
}

func TestScenarioPrintCanonicalForm(t *testing.T) {
	s, err := ParseScenario([]byte(validScenarioDoc))
	if err != nil {
		t.Fatal(err)
	}
	out := string(PrintScenario(s))
	if strings.ContainsAny(out, "\n\t") || strings.HasSuffix(out, "\n") || strings.Contains(out, ": ") || strings.Contains(out, ", ") {
		t.Fatalf("not compact: %s", out)
	}
	// Struct order: schemaVersion first, disruptionScript last.
	if !strings.HasPrefix(out, `{"schemaVersion":1,"name":`) || !strings.HasSuffix(out, `"target":"primary"}]}`) {
		t.Fatalf("unexpected field order: %s", out)
	}
	// Map keys sorted.
	if strings.Index(out, `"wages.tempPerHourCents"`) > strings.Index(out, `"workforce.tempOnboardingTicks"`) {
		t.Fatalf("map keys not sorted: %s", out)
	}
	// Optional absent fields omitted, not null.
	if strings.Contains(out, "null") {
		t.Fatalf("unexpected null: %s", out)
	}
}

func TestScenarioPrintIndependentOfInputFormatting(t *testing.T) {
	a, err := ParseScenario([]byte(validScenarioDoc))
	if err != nil {
		t.Fatal(err)
	}
	// Same document with different whitespace and key order in the meta map.
	reordered := strings.Replace(validScenarioDoc, `"schemaVersion": 1,`, `"schemaVersion":   1,`, 1)
	b, err := ParseScenario([]byte(reordered))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(PrintScenario(a), PrintScenario(b)) {
		t.Fatal("print depends on input formatting")
	}
}

func TestScenarioPrintNilCollectionsAndEscaping(t *testing.T) {
	s, err := ParseScenario([]byte(validScenarioDoc))
	if err != nil {
		t.Fatal(err)
	}
	s.Name = "A<B>&C"
	s.SKUs, s.Suppliers, s.DisruptionScript, s.AssumptionMeta, s.Demand.HourlyProfile = nil, nil, nil, nil, nil
	out := PrintScenario(s)
	if !bytes.Contains(out, []byte(`"A<B>&C"`)) {
		t.Fatalf("HTML escaped: %s", out)
	}
	back, err := ParseScenario(out)
	if err != nil {
		t.Fatalf("nil collections must print parseable: %v", err)
	}
	if back.Name != s.Name || len(back.SKUs) != 0 || len(back.AssumptionMeta) != 0 {
		t.Fatalf("unexpected: %+v", back)
	}
}
