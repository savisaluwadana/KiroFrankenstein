package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

const scenarioPath = "../../scenarios/black-friday.json"

type fixture struct {
	sc       *sim.Scenario
	hash     string
	file     inputsfile.File
	expected string // state hash at the horizon, computed without the CLI gate
}

// demoFixture builds the Inputs_File of the demo fork (Demo_Commands at the
// Demo_Fork_Tick) and its expected horizon hash from the engine directly.
func demoFixture(t *testing.T) fixture {
	t.Helper()
	sc, hash, err := loadScenario(scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := demoplan.Derive(*sc, demoplan.DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	f := inputsfile.File{ScenarioHash: hash, Seed: demoplan.DefaultSeed, EngineVersion: strconv.Itoa(sim.EngineVersion)}
	for i, c := range plan.SimCommands() {
		c := c
		f.Inputs = append(f.Inputs, inputsfile.Input{
			EffectiveTick: sim.Tick(plan.ForkTick) + 1, Seq: uint64(i) + 1, Command: &c, Provenance: inputsfile.ProvHuman,
		})
	}
	if len(f.Inputs) == 0 {
		t.Fatal("demo plan has no commands")
	}
	ins, err := inputsfile.ToInputs(f)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sim.Init(*sc, f.Seed)
	if err != nil {
		t.Fatal(err)
	}
	for int64(st.Tick()) < sc.HorizonTicks {
		var at []sim.Input
		for _, in := range ins {
			if in.EffectiveTick == st.Tick()+1 {
				at = append(at, in)
			}
		}
		step, err := sim.Advance(*sc, st, at)
		if err != nil {
			t.Fatal(err)
		}
		st = step.State
	}
	h := sim.Hash(st)
	return fixture{sc: sc, hash: hash, file: f, expected: hex.EncodeToString(h[:])}
}

func writeFile(t *testing.T, f inputsfile.File) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "inputs.json")
	if err := os.WriteFile(p, inputsfile.Print(f), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func exec(args ...string) (int, string, string) {
	var o, e bytes.Buffer
	code := run(args, &o, &e)
	return code, o.String(), e.String()
}

func verifyArgs(inputs, expect string) []string {
	return []string{"verify", "--scenario", scenarioPath, "--seed", "2026", "--inputs", inputs, "--expect", expect}
}

func TestVerifyExitCodes(t *testing.T) {
	fx := demoFixture(t)
	path := writeFile(t, fx.file)

	code, out, errOut := exec(verifyArgs(path, fx.expected)...)
	if code != 0 || !strings.Contains(out, "MATCH") || strings.Contains(out, "MISMATCH") || !strings.Contains(out, sim.Disclaimer) {
		t.Fatalf("match: code %d out %q err %q", code, out, errOut)
	}
	if !strings.Contains(out, "state_hash: "+fx.expected) {
		t.Fatalf("computed hash not printed: %q", out)
	}

	wrong := strings.Repeat("0", 64)
	code, out, _ = exec(verifyArgs(path, wrong)...)
	if code != 1 || !strings.Contains(out, "MISMATCH") {
		t.Fatalf("mismatch: code %d out %q", code, out)
	}

	old := fx.file
	old.EngineVersion = strconv.Itoa(sim.EngineVersion + 1)
	code, _, errOut = exec(verifyArgs(writeFile(t, old), fx.expected)...)
	if code != 2 || !strings.Contains(errOut, "engine version mismatch") {
		t.Fatalf("engine version: code %d err %q", code, errOut)
	}
}

func TestVerifyTickAndRunInputsAgree(t *testing.T) {
	fx := demoFixture(t)
	path := writeFile(t, fx.file)
	code, out, errOut := exec("run", "--scenario", scenarioPath, "--seed", "2026", "--inputs", path)
	if code != 0 || !strings.Contains(out, "state_hash: "+fx.expected) {
		t.Fatalf("run --inputs: code %d out %q err %q", code, out, errOut)
	}
	// Verifying an earlier tick against the horizon hash must mismatch.
	args := append(verifyArgs(path, fx.expected), "--tick", "10")
	if code, out, _ := exec(args...); code != 1 || !strings.Contains(out, "tick: 10") {
		t.Fatalf("--tick: code %d out %q", code, out)
	}
}

func TestInputsRejectedAndMismatchesDetected(t *testing.T) {
	fx := demoFixture(t)
	sha := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

	// A command the validator rejects (quantity 0) fails the run with code 3.
	bad := fx.file
	c := *bad.Inputs[0].Command
	c.Qty = 0
	bad.Inputs = []inputsfile.Input{{EffectiveTick: bad.Inputs[0].EffectiveTick, Seq: 1, Command: &c, Provenance: inputsfile.ProvHuman}}
	code, _, errOut := exec(verifyArgs(writeFile(t, bad), fx.expected)...)
	if code != 3 || !strings.Contains(errOut, "rejected") || !strings.Contains(errOut, string(sim.ReasonInvalidQuantity)) {
		t.Fatalf("invalid quantity: code %d err %q", code, errOut)
	}

	// A command outside any decision window is rejected.
	off := fx.file
	c2 := *off.Inputs[0].Command
	off.Inputs = []inputsfile.Input{{EffectiveTick: 3, Seq: 1, Command: &c2, Provenance: inputsfile.ProvHuman}}
	code, _, errOut = exec(verifyArgs(writeFile(t, off), fx.expected)...)
	if code != 3 || !strings.Contains(errOut, "no decision window") {
		t.Fatalf("no window: code %d err %q", code, errOut)
	}

	// Scenario hash mismatch.
	sm := fx.file
	sm.ScenarioHash = sha("other")
	code, _, errOut = exec(verifyArgs(writeFile(t, sm), fx.expected)...)
	if code != 3 || !strings.Contains(errOut, "scenario mismatch") {
		t.Fatalf("scenario: code %d err %q", code, errOut)
	}

	// Seed mismatch.
	code, _, errOut = exec("verify", "--scenario", scenarioPath, "--seed", "7", "--inputs", writeFile(t, fx.file), "--expect", fx.expected)
	if code != 3 || !strings.Contains(errOut, "seed mismatch") {
		t.Fatalf("seed: code %d err %q", code, errOut)
	}

	// Disruption outside the documented bounds is rejected.
	dis := fx.file
	dis.Inputs = []inputsfile.Input{{
		EffectiveTick: 8, Seq: 1, Provenance: inputsfile.ProvInjected,
		Disruption: &sim.DisruptionInput{Kind: sim.DisruptionWorkforceAbsence, DurationTicks: 4, MagnitudeBps: 10001},
	}}
	code, _, errOut = exec(verifyArgs(writeFile(t, dis), fx.expected)...)
	if code != 3 || !strings.Contains(errOut, "disruption rejected") {
		t.Fatalf("disruption: code %d err %q", code, errOut)
	}
}

func TestUsageAndFileErrorsExit3(t *testing.T) {
	cases := map[string][]string{
		"no args":        nil,
		"unknown":        {"nope"},
		"verify no flag": {"verify"},
		"bad expect":     {"verify", "--scenario", scenarioPath, "--seed", "2026", "--inputs", "x", "--expect", "abc"},
		"missing file":   verifyArgs("/nonexistent/inputs.json", strings.Repeat("0", 64)),
	}
	for name, args := range cases {
		if code, _, errOut := exec(args...); code != 3 || errOut == "" {
			t.Errorf("%s: code %d err %q", name, code, errOut)
		}
	}
	// Garbage file content.
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte(`{"nope":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := exec(verifyArgs(p, strings.Repeat("0", 64))...); code != 3 {
		t.Errorf("invalid file: code %d", code)
	}
}
