package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func runOnce(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := runCmd([]string{"--scenario", "../../scenarios/black-friday.json", "--seed", "2026"}, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestRunDeterministicAndDisclaimer(t *testing.T) {
	a, b := runOnce(t), runOnce(t)
	if a != b {
		t.Fatalf("outputs differ:\n%s\n%s", a, b)
	}
	if !strings.Contains(a, sim.Disclaimer) {
		t.Fatal("disclaimer missing")
	}
	for _, k := range []string{"state_hash: ", "net_profit_cents: ", "service_level_bps: ", "engine_version: "} {
		if !strings.Contains(a, k) {
			t.Fatalf("missing %q", k)
		}
	}
}

func TestRunRequiresScenario(t *testing.T) {
	if err := runCmd(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("want error")
	}
}
