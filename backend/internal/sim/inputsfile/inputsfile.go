// Package inputsfile parses and prints the Inputs_File: a JSON document that
// lists the Inputs of a Branch (effective Tick, kind, payload, Manager
// provenance and optional rationale) together with the Scenario hash, Seed and
// engine version under which they were recorded (Req 30.6 to 30.8).
//
// The package may import sim; sim never imports it. Provenance and rationale
// are input metadata: they are carried by the file but never reach the engine
// and never enter a State_Hash (INV-15). Idempotency keys, server timestamps
// and model names are not part of the format.
package inputsfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// MaxSeed is the largest Seed a file can carry, so seeds stay exact in
// JavaScript clients (the same bound as the HTTP API).
const MaxSeed = 1<<53 - 1

// Provenance values accepted in a file (Manager_Provenance and the origin of
// scenario and injected inputs).
const (
	ProvHuman               = "human"
	ProvAIRuleBased         = "ai_rule_based"
	ProvAILLM               = "ai_llm"
	ProvAIRuleBasedFallback = "ai_rule_based_fallback"
	ProvScenario            = "scenario"
	ProvInjected            = "injected"
)

var provenances = []string{ProvHuman, ProvAIRuleBased, ProvAILLM, ProvAIRuleBasedFallback, ProvScenario, ProvInjected}

// ErrInvalidInput is returned by ToInputs and FromInputs for an Input that
// cannot be converted.
var ErrInvalidInput = errors.New("inputsfile: invalid input")

// File is a parsed Inputs_File.
type File struct {
	// ScenarioHash is the lowercase hex SHA-256 of the canonical Scenario.
	ScenarioHash  string
	Seed          sim.Seed
	EngineVersion string
	Inputs        []Input
}

// Input is one entry of a file. Exactly one of Command and Disruption is set;
// it selects the kind.
type Input struct {
	EffectiveTick sim.Tick
	Seq           uint64
	Command       *sim.Command
	Disruption    *sim.DisruptionInput
	Provenance    string
	Rationale     string // optional, untrusted display text
}

// Kind returns the engine input kind of in.
func (in Input) Kind() sim.InputKind {
	if in.Disruption != nil {
		return sim.InputDisruption
	}
	return sim.InputCommand
}

// Meta is the non-engine information that accompanies one sim.Input.
type Meta struct {
	Provenance string
	Rationale  string
}

// ParseError reports the first offending field of an Inputs_File. Path uses
// dotted keys and bracketed indexes, for example "inputs[2].command.kind". An
// empty Path means the document as a whole.
type ParseError struct {
	Path string
	Msg  string
}

func (e *ParseError) Error() string {
	if e.Path == "" {
		return "inputs file: " + e.Msg
	}
	return fmt.Sprintf("inputs file: %s: %s", e.Path, e.Msg)
}

func perr(path, format string, a ...any) error {
	return &ParseError{Path: path, Msg: fmt.Sprintf(format, a...)}
}

// Parse strictly parses an Inputs_File. Unknown fields, missing required
// fields, wrong types and non-integer numbers are errors that name the JSON
// path of the first offending field. Within one object unknown keys are
// reported (in sorted order) before missing or mistyped fields (in
// declaration order). Inputs must be ordered by effective Tick and carry
// strictly increasing Seq values starting at 1 or above.
func Parse(data []byte) (File, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return File{}, perr("", "invalid JSON: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return File{}, perr("", "unexpected data after top-level value")
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return File{}, perr("", "expected object, got %s", kindOf(doc))
	}
	if err := unknownKeys(root, "", "scenarioHash", "seed", "engineVersion", "inputs"); err != nil {
		return File{}, err
	}

	var f File
	h, err := reqString(root, "", "scenarioHash")
	if err != nil {
		return File{}, err
	}
	if !validHash(h) {
		return File{}, perr("scenarioHash", "must be 64 lowercase hexadecimal characters")
	}
	f.ScenarioHash = h

	seed, err := reqUint(root, "", "seed")
	if err != nil {
		return File{}, err
	}
	if seed > MaxSeed {
		return File{}, perr("seed", "must be at most %d", uint64(MaxSeed))
	}
	f.Seed = sim.Seed(seed)

	if f.EngineVersion, err = reqString(root, "", "engineVersion"); err != nil {
		return File{}, err
	}
	if f.EngineVersion == "" {
		return File{}, perr("engineVersion", "must not be empty")
	}

	raw, present := root["inputs"]
	if !present {
		return File{}, perr("inputs", "missing required field")
	}
	arr, ok := raw.([]any)
	if !ok {
		return File{}, perr("inputs", "expected array, got %s", kindOf(raw))
	}
	var prevTick sim.Tick
	var prevSeq uint64
	for i, e := range arr {
		path := fmt.Sprintf("inputs[%d]", i)
		in, err := parseInput(e, path)
		if err != nil {
			return File{}, err
		}
		if in.EffectiveTick < prevTick {
			return File{}, perr(path+".effectiveTick", "inputs must be ordered by effective tick (%d after %d)", in.EffectiveTick, prevTick)
		}
		if in.Seq <= prevSeq {
			return File{}, perr(path+".seq", "must be greater than the previous seq %d", prevSeq)
		}
		prevTick, prevSeq = in.EffectiveTick, in.Seq
		f.Inputs = append(f.Inputs, in)
	}
	return f, nil
}

func parseInput(v any, path string) (Input, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return Input{}, perr(path, "expected object, got %s", kindOf(v))
	}
	if err := unknownKeys(m, path, "effectiveTick", "seq", "kind", "command", "disruption", "provenance", "rationale"); err != nil {
		return Input{}, err
	}
	var in Input
	tick, err := reqInt(m, path, "effectiveTick")
	if err != nil {
		return Input{}, err
	}
	if tick < 1 {
		return Input{}, perr(join(path, "effectiveTick"), "must be at least 1")
	}
	in.EffectiveTick = sim.Tick(tick)
	if in.Seq, err = reqUint(m, path, "seq"); err != nil {
		return Input{}, err
	}
	if in.Seq < 1 {
		return Input{}, perr(join(path, "seq"), "must be at least 1")
	}
	kind, err := reqString(m, path, "kind")
	if err != nil {
		return Input{}, err
	}
	switch kind {
	case "command":
		if _, has := m["disruption"]; has {
			return Input{}, perr(join(path, "disruption"), "not allowed for kind command")
		}
		raw, has := m["command"]
		if !has {
			return Input{}, perr(join(path, "command"), "missing required field")
		}
		c, err := parseCommand(raw, join(path, "command"))
		if err != nil {
			return Input{}, err
		}
		in.Command = &c
	case "disruption":
		if _, has := m["command"]; has {
			return Input{}, perr(join(path, "command"), "not allowed for kind disruption")
		}
		raw, has := m["disruption"]
		if !has {
			return Input{}, perr(join(path, "disruption"), "missing required field")
		}
		d, err := parseDisruption(raw, join(path, "disruption"))
		if err != nil {
			return Input{}, err
		}
		in.Disruption = &d
	default:
		return Input{}, perr(join(path, "kind"), "must be command or disruption, got %q", kind)
	}
	if in.Provenance, err = reqString(m, path, "provenance"); err != nil {
		return Input{}, err
	}
	if !validProvenance(in.Provenance) {
		return Input{}, perr(join(path, "provenance"), "must be one of %s", strings.Join(provenances, ", "))
	}
	if raw, has := m["rationale"]; has && raw != nil {
		s, ok := raw.(string)
		if !ok {
			return Input{}, perr(join(path, "rationale"), "expected string, got %s", kindOf(raw))
		}
		in.Rationale = s
	}
	return in, nil
}

func parseCommand(v any, path string) (sim.Command, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return sim.Command{}, perr(path, "expected object, got %s", kindOf(v))
	}
	if err := unknownKeys(m, path, "kind", "sku", "role", "qty", "amountCents", "poRef"); err != nil {
		return sim.Command{}, err
	}
	var c sim.Command
	name, err := reqString(m, path, "kind")
	if err != nil {
		return c, err
	}
	kind, ok := commandKindByName(name)
	if !ok {
		return c, perr(join(path, "kind"), "unknown command kind %q", name)
	}
	c.Kind = kind
	if c.SKU, err = reqInt(m, path, "sku"); err != nil {
		return c, err
	}
	role, err := reqUint(m, path, "role")
	if err != nil {
		return c, err
	}
	if role > 255 {
		return c, perr(join(path, "role"), "must be 0 to 255")
	}
	c.Role = uint8(role)
	if c.Qty, err = reqInt(m, path, "qty"); err != nil {
		return c, err
	}
	amount, err := reqInt(m, path, "amountCents")
	if err != nil {
		return c, err
	}
	c.AmountCents = sim.Cents(amount)
	if c.PORef, err = reqUint(m, path, "poRef"); err != nil {
		return c, err
	}
	return c, nil
}

func parseDisruption(v any, path string) (sim.DisruptionInput, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return sim.DisruptionInput{}, perr(path, "expected object, got %s", kindOf(v))
	}
	if err := unknownKeys(m, path, "kind", "target", "role", "durationTicks", "magnitudeBps"); err != nil {
		return sim.DisruptionInput{}, err
	}
	var d sim.DisruptionInput
	name, err := reqString(m, path, "kind")
	if err != nil {
		return d, err
	}
	kind, ok := disruptionKindByName(name)
	if !ok {
		return d, perr(join(path, "kind"), "unknown disruption kind %q", name)
	}
	d.Kind = kind
	if d.Target, err = reqInt(m, path, "target"); err != nil {
		return d, err
	}
	role, err := reqUint(m, path, "role")
	if err != nil {
		return d, err
	}
	if role > 255 {
		return d, perr(join(path, "role"), "must be 0 to 255")
	}
	d.Role = uint8(role)
	if d.DurationTicks, err = reqInt(m, path, "durationTicks"); err != nil {
		return d, err
	}
	mag, err := reqInt(m, path, "magnitudeBps")
	if err != nil {
		return d, err
	}
	d.MagnitudeBps = sim.Bps(mag)
	return d, nil
}

// ---- strict field access ----

func join(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func kindOf(v any) string {
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

func unknownKeys(m map[string]any, path string, known ...string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ok := false
		for _, n := range known {
			if n == k {
				ok = true
				break
			}
		}
		if !ok {
			return perr(join(path, k), "unknown field")
		}
	}
	return nil
}

func field(m map[string]any, path, name string) (any, error) {
	v, ok := m[name]
	if !ok {
		return nil, perr(join(path, name), "missing required field")
	}
	return v, nil
}

func reqString(m map[string]any, path, name string) (string, error) {
	v, err := field(m, path, name)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", perr(join(path, name), "expected string, got %s", kindOf(v))
	}
	return s, nil
}

func number(v any, p string) (json.Number, error) {
	n, ok := v.(json.Number)
	if !ok {
		return "", perr(p, "expected integer, got %s", kindOf(v))
	}
	if strings.ContainsAny(n.String(), ".eE") {
		return "", perr(p, "expected integer, got non-integer number %s", n)
	}
	return n, nil
}

func reqInt(m map[string]any, path, name string) (int64, error) {
	v, err := field(m, path, name)
	if err != nil {
		return 0, err
	}
	p := join(path, name)
	n, err := number(v, p)
	if err != nil {
		return 0, err
	}
	i, perrv := strconv.ParseInt(n.String(), 10, 64)
	if perrv != nil {
		return 0, perr(p, "integer out of range: %s", n)
	}
	return i, nil
}

func reqUint(m map[string]any, path, name string) (uint64, error) {
	v, err := field(m, path, name)
	if err != nil {
		return 0, err
	}
	p := join(path, name)
	n, err := number(v, p)
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(n.String(), "-") {
		return 0, perr(p, "must not be negative")
	}
	u, perrv := strconv.ParseUint(n.String(), 10, 64)
	if perrv != nil {
		return 0, perr(p, "integer out of range: %s", n)
	}
	return u, nil
}

func validHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validProvenance(s string) bool {
	for _, p := range provenances {
		if p == s {
			return true
		}
	}
	return false
}

func commandKindByName(name string) (sim.CommandKind, bool) {
	for k := sim.CommandKind(0); k.Name() != ""; k++ {
		if k.Name() == name {
			return k, true
		}
	}
	return 0, false
}

var disruptionKinds = []struct {
	code uint8
	name string
}{
	{sim.DisruptionDemandSurge, "demand_surge"},
	{sim.DisruptionSupplierOutage, "supplier_outage"},
	{sim.DisruptionWorkforceAbsence, "workforce_absence"},
}

func disruptionKindByName(name string) (uint8, bool) {
	for _, k := range disruptionKinds {
		if k.name == name {
			return k.code, true
		}
	}
	return 0, false
}

func disruptionKindName(code uint8) (string, bool) {
	for _, k := range disruptionKinds {
		if k.code == code {
			return k.name, true
		}
	}
	return "", false
}

// ---- printer ----

func quote(s string) string {
	b, _ := json.Marshal(s) // a string always marshals
	return string(b)
}

// Print returns the canonical encoding of f: two-space indentation, fields in
// the fixed order of the format, one input per block, a trailing newline and
// the rationale omitted when empty. Print does not validate f; an Input whose
// kind or provenance cannot be named is printed with the numeric code or the
// value as is, and Parse then rejects it. Parse(Print(f)) equals f for every
// valid f.
func Print(f File) []byte {
	var b bytes.Buffer
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"scenarioHash\": %s,\n", quote(f.ScenarioHash))
	fmt.Fprintf(&b, "  \"seed\": %d,\n", uint64(f.Seed))
	fmt.Fprintf(&b, "  \"engineVersion\": %s,\n", quote(f.EngineVersion))
	if len(f.Inputs) == 0 {
		b.WriteString("  \"inputs\": []\n}\n")
		return b.Bytes()
	}
	b.WriteString("  \"inputs\": [\n")
	for i, in := range f.Inputs {
		b.WriteString("    {\n")
		fmt.Fprintf(&b, "      \"effectiveTick\": %d,\n", int64(in.EffectiveTick))
		fmt.Fprintf(&b, "      \"seq\": %d,\n", in.Seq)
		switch {
		case in.Disruption != nil:
			d := in.Disruption
			name, ok := disruptionKindName(d.Kind)
			if !ok {
				name = "unknown_" + strconv.Itoa(int(d.Kind))
			}
			b.WriteString("      \"kind\": \"disruption\",\n")
			b.WriteString("      \"disruption\": {\n")
			fmt.Fprintf(&b, "        \"kind\": %s,\n", quote(name))
			fmt.Fprintf(&b, "        \"target\": %d,\n", d.Target)
			fmt.Fprintf(&b, "        \"role\": %d,\n", d.Role)
			fmt.Fprintf(&b, "        \"durationTicks\": %d,\n", d.DurationTicks)
			fmt.Fprintf(&b, "        \"magnitudeBps\": %d\n", int64(d.MagnitudeBps))
			b.WriteString("      },\n")
		case in.Command != nil:
			c := in.Command
			name := c.Kind.Name()
			if name == "" {
				name = "unknown_" + strconv.Itoa(int(c.Kind))
			}
			b.WriteString("      \"kind\": \"command\",\n")
			b.WriteString("      \"command\": {\n")
			fmt.Fprintf(&b, "        \"kind\": %s,\n", quote(name))
			fmt.Fprintf(&b, "        \"sku\": %d,\n", c.SKU)
			fmt.Fprintf(&b, "        \"role\": %d,\n", c.Role)
			fmt.Fprintf(&b, "        \"qty\": %d,\n", c.Qty)
			fmt.Fprintf(&b, "        \"amountCents\": %d,\n", int64(c.AmountCents))
			fmt.Fprintf(&b, "        \"poRef\": %d\n", c.PORef)
			b.WriteString("      },\n")
		default:
			b.WriteString("      \"kind\": \"unknown\",\n")
		}
		if in.Rationale != "" {
			fmt.Fprintf(&b, "      \"provenance\": %s,\n", quote(in.Provenance))
			fmt.Fprintf(&b, "      \"rationale\": %s\n", quote(in.Rationale))
		} else {
			fmt.Fprintf(&b, "      \"provenance\": %s\n", quote(in.Provenance))
		}
		b.WriteString("    }")
		if i < len(f.Inputs)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return b.Bytes()
}

// ---- conversion ----

// ToInputs returns the engine inputs of f in file order. Provenance and
// rationale are dropped: the engine never sees them.
func ToInputs(f File) ([]sim.Input, error) {
	out := make([]sim.Input, 0, len(f.Inputs))
	for i, in := range f.Inputs {
		si := sim.Input{EffectiveTick: in.EffectiveTick, Seq: in.Seq, Kind: in.Kind()}
		switch {
		case in.Command != nil && in.Disruption != nil:
			return nil, fmt.Errorf("%w: inputs[%d] has both a command and a disruption", ErrInvalidInput, i)
		case in.Command != nil:
			si.Command = *in.Command
		case in.Disruption != nil:
			si.Disruption = *in.Disruption
		default:
			return nil, fmt.Errorf("%w: inputs[%d] has neither a command nor a disruption", ErrInvalidInput, i)
		}
		out = append(out, si)
	}
	return out, nil
}

// FromInputs is the inverse of ToInputs: it pairs each engine input with its
// metadata (meta[i] belongs to ins[i]). An empty provenance becomes "human".
func FromInputs(ins []sim.Input, meta []Meta) ([]Input, error) {
	if len(meta) != len(ins) {
		return nil, fmt.Errorf("%w: %d inputs but %d metadata entries", ErrInvalidInput, len(ins), len(meta))
	}
	var out []Input
	for i, si := range ins {
		in := Input{EffectiveTick: si.EffectiveTick, Seq: si.Seq, Provenance: meta[i].Provenance, Rationale: meta[i].Rationale}
		if in.Provenance == "" {
			in.Provenance = ProvHuman
		}
		switch si.Kind {
		case sim.InputCommand:
			c := si.Command
			in.Command = &c
		case sim.InputDisruption:
			d := si.Disruption
			in.Disruption = &d
		default:
			return nil, fmt.Errorf("%w: input %d has unknown kind %d", ErrInvalidInput, i, si.Kind)
		}
		out = append(out, in)
	}
	return out, nil
}
