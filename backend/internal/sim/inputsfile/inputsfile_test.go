package inputsfile_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

const hash64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func sample() inputsfile.File {
	return inputsfile.File{
		ScenarioHash:  hash64,
		Seed:          2026,
		EngineVersion: "2",
		Inputs: []inputsfile.Input{
			{EffectiveTick: 4, Seq: 1, Provenance: inputsfile.ProvHuman,
				Command: &sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: 1, Role: sim.SupplierBackup, Qty: 300}},
			{EffectiveTick: 4, Seq: 2, Provenance: inputsfile.ProvAILLM, Rationale: "stock \"low\" <b>x</b>\n",
				Command: &sim.Command{Kind: sim.CmdSetPrice, SKU: 0, AmountCents: 1999}},
			{EffectiveTick: 9, Seq: 3, Provenance: inputsfile.ProvInjected,
				Disruption: &sim.DisruptionInput{Kind: sim.DisruptionSupplierOutage, Target: -1, Role: sim.SupplierPrimary, DurationTicks: 12}},
		},
	}
}

func TestRoundTrip(t *testing.T) {
	f := sample()
	b := inputsfile.Print(f)
	got, err := inputsfile.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, b)
	}
	if !reflect.DeepEqual(got, f) {
		t.Fatalf("parse(print(f)) != f\n got %+v\nwant %+v", got, f)
	}
	if !reflect.DeepEqual(inputsfile.Print(got), b) {
		t.Fatal("print is not canonical")
	}
	empty := inputsfile.File{ScenarioHash: hash64, Seed: 1, EngineVersion: "2"}
	got, err = inputsfile.Parse(inputsfile.Print(empty))
	if err != nil || !reflect.DeepEqual(got, empty) {
		t.Fatalf("empty round trip: %v %+v", err, got)
	}
}

func TestParseErrorsNameJSONPath(t *testing.T) {
	good := string(inputsfile.Print(sample()))
	rep := func(old, new string) string {
		if !strings.Contains(good, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return strings.Replace(good, old, new, 1)
	}
	tests := []struct {
		name, doc, path string
	}{
		{"unknown top-level", rep(`"seed": 2026,`, `"seed": 2026, "extra": 1,`), "extra"},
		{"missing seed", rep(`"seed": 2026,`, ``), "seed"},
		{"bad hash", rep(hash64, "XYZ"), "scenarioHash"},
		{"seed string", rep(`"seed": 2026`, `"seed": "x"`), "seed"},
		{"seed too large", rep(`"seed": 2026`, `"seed": 9007199254740992`), "seed"},
		{"fractional tick", rep(`"effectiveTick": 9`, `"effectiveTick": 9.5`), "inputs[2].effectiveTick"},
		{"unknown input field", rep(`"seq": 2,`, `"seq": 2, "idempotencyKey": "a",`), "inputs[1].idempotencyKey"},
		{"bad command kind", rep(`"set_price"`, `"set_cost"`), "inputs[1].command.kind"},
		{"bad disruption kind", rep(`"supplier_outage"`, `"flood"`), "inputs[2].disruption.kind"},
		{"missing command field", rep(`"poRef": 0`, `"x": 0`), "inputs[0].command.x"},
		{"bad provenance", rep(`"ai_llm"`, `"robot"`), "inputs[1].provenance"},
		{"rationale type", rep(`"rationale": "stock`, `"rationale": 5, "r": "stock`), "inputs[1].r"},
		{"seq not increasing", rep(`"seq": 2,`, `"seq": 1,`), "inputs[1].seq"},
		{"ticks out of order", rep(`"effectiveTick": 9`, `"effectiveTick": 3`), "inputs[2].effectiveTick"},
		{"inputs not array", rep(`"inputs": [`, `"inputs": {"a": [`) + `}`, "inputs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := inputsfile.Parse([]byte(tc.doc))
			var pe *inputsfile.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want *ParseError", err)
			}
			if pe.Path != tc.path {
				t.Fatalf("path = %q (%v), want %q", pe.Path, err, tc.path)
			}
		})
	}
	for _, doc := range []string{``, `[]`, `{} {}`, `{"seed":`} {
		var pe *inputsfile.ParseError
		if _, err := inputsfile.Parse([]byte(doc)); !errors.As(err, &pe) || pe.Path != "" {
			t.Errorf("%q: err = %v, want document-level ParseError", doc, err)
		}
	}
}

func TestConversion(t *testing.T) {
	f := sample()
	ins, err := inputsfile.ToInputs(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 3 || ins[0].Kind != sim.InputCommand || ins[2].Kind != sim.InputDisruption ||
		ins[1].Command.AmountCents != 1999 || ins[2].Disruption.DurationTicks != 12 || ins[2].Seq != 3 {
		t.Fatalf("inputs = %+v", ins)
	}
	meta := make([]inputsfile.Meta, len(f.Inputs))
	for i, in := range f.Inputs {
		meta[i] = inputsfile.Meta{Provenance: in.Provenance, Rationale: in.Rationale}
	}
	back, err := inputsfile.FromInputs(ins, meta)
	if err != nil || !reflect.DeepEqual(back, f.Inputs) {
		t.Fatalf("FromInputs: %v\n got %+v\nwant %+v", err, back, f.Inputs)
	}
	if _, err := inputsfile.FromInputs(ins, nil); !errors.Is(err, inputsfile.ErrInvalidInput) {
		t.Errorf("metadata mismatch: err = %v", err)
	}
	if _, err := inputsfile.ToInputs(inputsfile.File{Inputs: []inputsfile.Input{{EffectiveTick: 1, Seq: 1}}}); !errors.Is(err, inputsfile.ErrInvalidInput) {
		t.Errorf("empty input: err = %v", err)
	}
}
