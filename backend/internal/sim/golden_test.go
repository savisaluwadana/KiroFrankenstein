package sim_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Golden flagship hashes (INV-1, Req 6.4).
//
// The files under scenarios/golden pin the engine's output for the Black
// Friday Meltdown at Seed 2026: the final State_Hash with the demo-fork
// Inputs_File (demo-fork.hash), the State_Hash at a few Ticks
// (demo-fork.ticks) and the final State_Hash with no Commands
// (no-command.hash). They change only with an engine version bump (or a
// deliberate Scenario change), and the commit message must say so. Regenerate
// them with:
//
//	UPDATE_GOLDEN=1 go test ./internal/app/demoplan -run TestDemoForkInputsFile
//	UPDATE_GOLDEN=1 go test ./internal/sim -run TestGoldenFlagshipHash
//
// CI runs this test on amd64 and arm64.

const (
	goldenScenario = "../../scenarios/black-friday.json"
	goldenInputs   = "../../scenarios/demo-fork-inputs.json"
	goldenForkHash = "../../scenarios/golden/demo-fork.hash"
	goldenTicks    = "../../scenarios/golden/demo-fork.ticks"
	goldenNoCmd    = "../../scenarios/golden/no-command.hash"
	goldenSeed     = sim.Seed(2026)
)

var goldenTickList = []int64{20, 21, 60, 120}

func hashHex(st sim.State) string {
	h := sim.Hash(st)
	return hex.EncodeToString(h[:])
}

func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("../../scenarios/golden", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (regenerate with UPDATE_GOLDEN=1): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s differs (change only with an engine version bump; regenerate with UPDATE_GOLDEN=1)\ngot:\n%swant:\n%s", path, got, want)
	}
}

func TestGoldenFlagshipHash(t *testing.T) {
	sc, scHash, err := replayfile.LoadScenario(goldenScenario)
	if err != nil {
		t.Fatal(err)
	}
	f, err := replayfile.LoadInputs(goldenInputs, scHash, goldenSeed)
	if err != nil {
		t.Fatal(err)
	}

	perTick := map[int64]string{}
	var tick int64
	final, err := replayfile.Simulate(sc, goldenSeed, &f, -1, func(st sim.State) {
		perTick[tick] = hashHex(st)
		tick++
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(final.Tick()) != sc.HorizonTicks {
		t.Fatalf("final tick %d, want %d", final.Tick(), sc.HorizonTicks)
	}
	checkGolden(t, goldenForkHash, hashHex(final)+"\n")

	var b strings.Builder
	for _, k := range goldenTickList {
		h, ok := perTick[k]
		if !ok {
			t.Fatalf("no hash recorded for tick %d", k)
		}
		fmt.Fprintf(&b, "%d %s\n", k, h)
	}
	checkGolden(t, goldenTicks, b.String())

	t.Run("no-command", func(t *testing.T) {
		st, err := replayfile.Simulate(sc, goldenSeed, nil, -1, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := hashHex(st)
		checkGolden(t, goldenNoCmd, got+"\n")
		readme, err := os.ReadFile("../../../README.md")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(readme), got) {
			t.Errorf("README.md does not mention the engine version %d no-command hash %s", sim.EngineVersion, got)
		}
	})
}
