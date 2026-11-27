package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const (
	bfScenario = "../../scenarios/black-friday.json"
	bfHuman    = "../../scenarios/demo-duel-human.json"
)

func runCLI(args ...string) (int, string, string) {
	var o, e bytes.Buffer
	code := run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestEnsembleCLI(t *testing.T) {
	code, out, errs := runCLI("ensemble", "--scenario", bfScenario, "--seeds", "2025..2026",
		"--arm", "fixed:"+bfHuman, "--arm", "rulebased", "--concurrency", "2")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"9786f539", "paired:", "arm 1: adaptive policy: rule-based"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestEnsembleCLIUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--scenario", bfScenario, "--seeds", "1..51", "--arm", "rulebased"},
		{"--scenario", bfScenario, "--seeds", "1,1", "--arm", "rulebased"},
		{"--scenario", bfScenario, "--seeds", "1", "--arm", "llm"},
		{"--scenario", bfScenario, "--seeds", "1"},
	} {
		if code, _, _ := runCLI(append([]string{"ensemble"}, args...)...); code != exitUsageOrFileError {
			t.Errorf("%v: exit %d, want %d", args, code, exitUsageOrFileError)
		}
	}
}

func TestEnsembleCLIWritesReportFiles(t *testing.T) {
	dir := t.TempDir()
	prefix := dir + "/ens"
	code, out, errs := runCLI("ensemble", "--scenario", bfScenario, "--seeds", "1..2",
		"--arm", "fixed:"+bfHuman, "--arm", "rulebased", "--out", prefix, "--tie-tolerance", "7")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "paired:") {
		t.Errorf("plain summary missing:\n%s", out)
	}
	js, err := os.ReadFile(prefix + ".json")
	if err != nil || !strings.Contains(string(js), `"tieToleranceHundredths":7`) || !strings.HasSuffix(string(js), "\n") {
		t.Fatalf("json: %v %.200s", err, js)
	}
	cs, err := os.ReadFile(prefix + ".csv")
	if err != nil || !strings.HasPrefix(string(cs), "record,arm_index,") {
		t.Fatalf("csv: %v %.100s", err, cs)
	}
	if code, _, _ := runCLI("ensemble", "--scenario", bfScenario, "--seeds", "1", "--arm", "rulebased", "--out", dir+"/missing/ens"); code == exitOK {
		t.Error("missing parent dir must fail")
	}
}
