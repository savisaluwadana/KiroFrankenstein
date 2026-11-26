package sim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// ErrUnsupportedSchemaVersion is returned when a Scenario declares a
// schemaVersion other than SupportedSchemaVersion (Req 1.5).
var ErrUnsupportedSchemaVersion = errors.New("unsupported scenario schema version")

// ScenarioParseError reports the first offending field of a Scenario document.
// Path uses dotted keys and bracketed indexes, for example "skus[0].id". An
// empty Path means the document as a whole.
type ScenarioParseError struct {
	Path string
	Msg  string
}

func (e *ScenarioParseError) Error() string {
	if e.Path == "" {
		return "scenario: " + e.Msg
	}
	return fmt.Sprintf("scenario: %s: %s", e.Path, e.Msg)
}

// ParseScenario strictly parses a Scenario document: unknown fields, missing
// required fields, wrong types and non-integer numbers are errors that name
// the JSON path of the first offending field. Within one object, unknown keys
// are reported (in sorted order) before missing or mistyped fields (in
// declaration order). A schemaVersion other than SupportedSchemaVersion
// returns an error wrapping ErrUnsupportedSchemaVersion before any other
// field is examined. The result is not validated against assumption bounds.
func ParseScenario(data []byte) (*Scenario, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, &ScenarioParseError{Msg: "invalid JSON: " + err.Error()}
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, &ScenarioParseError{Msg: "unexpected data after top-level value"}
	}

	root, ok := doc.(map[string]any)
	if !ok {
		return nil, &ScenarioParseError{Msg: "expected object, got " + jsonKind(doc)}
	}
	if raw, present := root["schemaVersion"]; present {
		if n, isNum := raw.(json.Number); isNum {
			if v, err := strconv.ParseInt(n.String(), 10, 64); err == nil && v != SupportedSchemaVersion {
				return nil, fmt.Errorf("scenario: schemaVersion %d (supported: %d): %w",
					v, SupportedSchemaVersion, ErrUnsupportedSchemaVersion)
			}
		}
	}

	if err := checkAgainst(doc, scenarioSchema, ""); err != nil {
		return nil, err
	}

	// The walker has already proven the document fits; this decode is the
	// authoritative strict decode into the typed value.
	var s Scenario
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&s); err != nil {
		return nil, &ScenarioParseError{Msg: err.Error()}
	}
	return &s, nil
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func wrongType(path, want string, got any) error {
	return &ScenarioParseError{Path: path, Msg: fmt.Sprintf("expected %s, got %s", want, jsonKind(got))}
}

type schemaKind uint8

const (
	kString schemaKind = iota
	kInt
	kArray
	kMap
	kObject
)

// field is one member of an object schema. Optional members may be absent or null.
type field struct {
	name     string
	optional bool
	s        *schema
}

// schema is a minimal document shape used to locate the first offending field.
// It mirrors the Scenario types in scenario.go; TestScenarioParseSchemaMatchesTypes
// keeps the two in sync.
type schema struct {
	kind   schemaKind
	elem   *schema // array and map element
	fields []field // object members, in declaration order
}

var (
	sStr = &schema{kind: kString}
	sInt = &schema{kind: kInt}
)

func obj(fs ...field) *schema       { return &schema{kind: kObject, fields: fs} }
func req(n string, s *schema) field { return field{name: n, s: s} }
func opt(n string, s *schema) field { return field{name: n, optional: true, s: s} }

var scenarioSchema = obj(
	req("schemaVersion", sInt),
	req("name", sStr),
	req("horizonTicks", sInt),
	req("startClock", sStr),
	req("decisionCadenceTicks", sInt),
	req("commandBudget", sInt),
	req("snapshotIntervalTicks", sInt),
	req("tieToleranceHundredths", sInt),
	req("scoreWeights", obj(
		req("financialWeightBps", sInt),
		req("fulfilmentWeightBps", sInt),
		req("onTimeWeightBps", sInt),
	)),
	req("profitFloorCents", sInt),
	req("profitTargetCents", sInt),
	req("openingCashCents", sInt),
	req("skus", &schema{kind: kArray, elem: obj(
		req("id", sStr),
		req("referencePriceCents", sInt),
		req("openingUnits", sInt),
		req("openingUnitCostCents", sInt),
		req("promiseTicks", sInt),
	)}),
	req("suppliers", &schema{kind: kArray, elem: obj(
		req("skuId", sStr),
		req("role", sStr),
		req("unitCostCents", sInt),
		req("leadTimeTicks", sInt),
	)}),
	req("workforce", obj(
		req("regularWorkers", sInt),
		req("unitsPerWorkerHour", sInt),
		req("maxOvertimePerWorker", sInt),
		req("maxTempWorkers", sInt),
		req("tempOnboardingTicks", sInt),
	)),
	req("wages", obj(
		req("regularPerHourCents", sInt),
		req("overtimePerHourCents", sInt),
		req("tempPerHourCents", sInt),
	)),
	req("costs", obj(
		req("outboundShipCentsPerUnit", sInt),
		req("expediteFeeCents", sInt),
		req("expediteReductionTicks", sInt),
		req("hourlyOverheadCents", sInt),
		req("serviceCreditPerLateOrderCents", sInt),
	)),
	req("demand", obj(
		req("hourlyProfile", &schema{kind: kArray, elem: sInt}),
		req("baseArrivalsPerTick", sInt),
		req("baseConversionBps", sInt),
		req("priceElasticityBps", sInt),
		req("marketingEffectBps", sInt),
		req("cancelThresholdTicks", sInt),
	)),
	req("assumptionMeta", &schema{kind: kMap, elem: obj(
		req("unit", sStr),
		req("default", sInt),
		req("min", sInt),
		req("max", sInt),
		req("rationale", sStr),
	)}),
	req("disruptionScript", &schema{kind: kArray, elem: obj(
		req("kind", sStr),
		req("onsetTick", sInt),
		req("durationTicks", sInt),
		opt("magnitudeBps", sInt),
		opt("target", sStr),
	)}),
)

func checkAgainst(v any, sc *schema, path string) error {
	switch sc.kind {
	case kString:
		if _, ok := v.(string); !ok {
			return wrongType(path, "string", v)
		}
	case kInt:
		n, ok := v.(json.Number)
		if !ok {
			return wrongType(path, "integer", v)
		}
		if _, err := strconv.ParseInt(n.String(), 10, 64); err != nil {
			if strings.ContainsAny(n.String(), ".eE") {
				return &ScenarioParseError{Path: path, Msg: "expected integer, got non-integer number " + n.String()}
			}
			return &ScenarioParseError{Path: path, Msg: "integer out of range: " + n.String()}
		}
	case kArray:
		arr, ok := v.([]any)
		if !ok {
			return wrongType(path, "array", v)
		}
		for i, e := range arr {
			if err := checkAgainst(e, sc.elem, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case kMap:
		m, ok := v.(map[string]any)
		if !ok {
			return wrongType(path, "object", v)
		}
		for _, k := range sortedKeys(m) {
			if err := checkAgainst(m[k], sc.elem, joinPath(path, k)); err != nil {
				return err
			}
		}
	case kObject:
		m, ok := v.(map[string]any)
		if !ok {
			return wrongType(path, "object", v)
		}
		known := make(map[string]bool, len(sc.fields))
		for _, f := range sc.fields {
			known[f.name] = true
		}
		for _, k := range sortedKeys(m) {
			if !known[k] {
				return &ScenarioParseError{Path: joinPath(path, k), Msg: "unknown field"}
			}
		}
		for _, f := range sc.fields {
			val, present := m[f.name]
			if !present {
				if f.optional {
					continue
				}
				return &ScenarioParseError{Path: joinPath(path, f.name), Msg: "missing required field"}
			}
			if val == nil && f.optional {
				continue
			}
			if err := checkAgainst(val, f.s, joinPath(path, f.name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
