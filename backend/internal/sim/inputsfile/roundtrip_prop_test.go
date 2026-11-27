package inputsfile_test

import (
	"bytes"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

var propProvenances = []string{
	inputsfile.ProvHuman, inputsfile.ProvAIRuleBased, inputsfile.ProvAILLM,
	inputsfile.ProvAIRuleBasedFallback, inputsfile.ProvScenario, inputsfile.ProvInjected,
}

var propDisruptionKinds = []uint8{
	sim.DisruptionDemandSurge, sim.DisruptionSupplierOutage, sim.DisruptionWorkforceAbsence,
}

func genCommand(t *rapid.T) *sim.Command {
	var kinds []sim.CommandKind
	for k := sim.CommandKind(0); k.Name() != ""; k++ {
		kinds = append(kinds, k)
	}
	return &sim.Command{
		Kind:        rapid.SampledFrom(kinds).Draw(t, "cmdKind"),
		SKU:         rapid.Int64Range(-1, 1000).Draw(t, "sku"),
		Role:        rapid.SampledFrom([]uint8{sim.SupplierPrimary, sim.SupplierBackup}).Draw(t, "role"),
		Qty:         rapid.Int64Range(-1000000, 1000000).Draw(t, "qty"),
		AmountCents: sim.Cents(rapid.Int64Range(-1<<40, 1<<40).Draw(t, "amount")),
		PORef:       rapid.Uint64Range(0, 1<<53).Draw(t, "poRef"),
	}
}

func genDisruption(t *rapid.T) *sim.DisruptionInput {
	return &sim.DisruptionInput{
		Kind:          rapid.SampledFrom(propDisruptionKinds).Draw(t, "disKind"),
		Target:        rapid.Int64Range(-1, 1000).Draw(t, "target"),
		Role:          rapid.SampledFrom([]uint8{sim.SupplierPrimary, sim.SupplierBackup}).Draw(t, "dRole"),
		DurationTicks: rapid.Int64Range(0, 1000).Draw(t, "duration"),
		MagnitudeBps:  sim.Bps(rapid.Int64Range(0, 100000).Draw(t, "magnitude")),
	}
}

func genFile(t *rapid.T) inputsfile.File {
	f := inputsfile.File{
		ScenarioHash:  rapid.StringMatching(`[0-9a-f]{64}`).Draw(t, "hash"),
		Seed:          sim.Seed(rapid.Uint64Range(0, inputsfile.MaxSeed).Draw(t, "seed")),
		EngineVersion: rapid.StringMatching(`[0-9]{1,3}(\.[0-9]{1,3}){0,2}`).Draw(t, "engine"),
	}
	n := rapid.IntRange(0, 12).Draw(t, "n")
	tick := sim.Tick(1)
	seq := uint64(0)
	for i := 0; i < n; i++ {
		tick += sim.Tick(rapid.Int64Range(0, 20).Draw(t, "dTick"))
		seq += rapid.Uint64Range(1, 5).Draw(t, "dSeq")
		in := inputsfile.Input{
			EffectiveTick: tick,
			Seq:           seq,
			Provenance:    rapid.SampledFrom(propProvenances).Draw(t, "prov"),
		}
		if rapid.Bool().Draw(t, "isCommand") {
			in.Command = genCommand(t)
		} else {
			in.Disruption = genDisruption(t)
		}
		if rapid.Bool().Draw(t, "hasRationale") {
			in.Rationale = rapid.OneOf(
				rapid.String(),
				rapid.SampledFrom([]string{"say \"hi\"", "back\\slash\n", "<b>x</b> & é 日本語 😀", "\u2028\t\x00"}),
			).Draw(t, "rationale")
		}
		f.Inputs = append(f.Inputs, in)
	}
	return f
}

// Feature: parallel-simulation-mvp, Property 3: Inputs file round trip
//
// **Validates: Requirements 30.7**
func TestPropInputsFileRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := genFile(t)

		printed := inputsfile.Print(f)
		got, err := inputsfile.Parse(printed)
		if err != nil {
			t.Fatalf("Parse(Print(f)): %v\n%s", err, printed)
		}
		if !reflect.DeepEqual(got, f) {
			t.Fatalf("Parse(Print(f)) != f\n got %+v\nwant %+v", got, f)
		}
		if again := inputsfile.Print(got); !bytes.Equal(again, printed) {
			t.Fatalf("Print(Parse(Print(f))) differs from Print(f)\n%s\n---\n%s", again, printed)
		}

		ins, err := inputsfile.ToInputs(f)
		if err != nil {
			t.Fatalf("ToInputs: %v", err)
		}
		meta := make([]inputsfile.Meta, len(f.Inputs))
		for i, in := range f.Inputs {
			meta[i] = inputsfile.Meta{Provenance: in.Provenance, Rationale: in.Rationale}
		}
		back, err := inputsfile.FromInputs(ins, meta)
		if err != nil {
			t.Fatalf("FromInputs: %v", err)
		}
		if !reflect.DeepEqual(back, f.Inputs) {
			t.Fatalf("FromInputs(ToInputs(f)) != f.Inputs\n got %+v\nwant %+v", back, f.Inputs)
		}
		ins2, err := inputsfile.ToInputs(inputsfile.File{Inputs: back})
		if err != nil || !reflect.DeepEqual(ins2, ins) {
			t.Fatalf("ToInputs(FromInputs(ins)) != ins: %v", err)
		}
	})
}
