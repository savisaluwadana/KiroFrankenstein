package demoplan

import (
	"bytes"
	"os"
	"strconv"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

const demoInputsPath = "../../../scenarios/demo-fork-inputs.json"

// TestDemoForkInputsFile checks that the committed scenarios/demo-fork-inputs.json
// is exactly the Demo_Commands of Derive at the Demo_Fork_Tick, as an
// Inputs_File for the demo Seed. Regenerate with UPDATE_GOLDEN=1 (then
// regenerate the hashes with UPDATE_GOLDEN=1 go test ./internal/sim -run
// TestGoldenFlagshipHash). The file changes only with a Scenario change or an
// engine version bump, which the commit message must note.
func TestDemoForkInputsFile(t *testing.T) {
	sc := loadFlagship(t)
	plan, err := Derive(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	f := inputsfile.File{
		ScenarioHash:  replayfile.ScenarioHash(&sc),
		Seed:          DefaultSeed,
		EngineVersion: strconv.Itoa(sim.EngineVersion),
	}
	for i, c := range plan.SimCommands() {
		c := c
		f.Inputs = append(f.Inputs, inputsfile.Input{
			EffectiveTick: sim.Tick(plan.ForkTick) + 1,
			Seq:           uint64(i) + 1,
			Command:       &c,
			Provenance:    inputsfile.ProvHuman,
		})
	}
	if len(f.Inputs) == 0 {
		t.Fatal("demo plan has no commands")
	}
	got := inputsfile.Print(f)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(demoInputsPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(demoInputsPath)
	if err != nil {
		t.Fatalf("read %s (regenerate with UPDATE_GOLDEN=1): %v", demoInputsPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the derived plan (regenerate with UPDATE_GOLDEN=1)\ngot:\n%s\nwant:\n%s", demoInputsPath, got, want)
	}
}
