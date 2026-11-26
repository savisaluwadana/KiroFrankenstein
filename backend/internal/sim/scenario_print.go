package sim

import (
	"bytes"
	"encoding/json"
)

// PrintScenario returns the canonical encoding of s: compact JSON with no
// insignificant whitespace, members in Scenario struct order, map keys in
// sorted order, and absent optional fields omitted. Nil slices and maps print
// as [] and {} so that the output always parses with ParseScenario. The
// output is a pure function of s, and ParseScenario(PrintScenario(s)) yields
// a value equal to s (nil collections come back empty).
func PrintScenario(s *Scenario) []byte {
	c := *s // shallow copy; only the copy's collection headers are replaced
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
	if c.DisruptionScript == nil {
		c.DisruptionScript = []DisruptionSpec{}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&c); err != nil {
		// Scenario holds only strings, integers, slices, maps and pointers.
		panic("sim: scenario print: " + err.Error())
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}
