package demoplan

import (
	"bytes"
	"os"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

const (
	scenarioPath = "../../../scenarios/black-friday.json"
	fixturePath  = "../../../../web/e2e/fixtures/demo-plan.json"
)

func loadFlagship(t *testing.T) sim.Scenario {
	t.Helper()
	data, err := os.ReadFile(scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := sim.ParseScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	return *sc
}

func TestDemoPlanMatchesFixture(t *testing.T) {
	sc := loadFlagship(t)
	plan, err := Derive(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := plan.Fixture()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_DEMOPLAN") == "1" {
		if err := os.MkdirAll("../../../../web/e2e/fixtures", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixturePath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture (regenerate with UPDATE_DEMOPLAN=1): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixture differs from computation (regenerate with UPDATE_DEMOPLAN=1)\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestDemoPlanStructure(t *testing.T) {
	sc := loadFlagship(t)
	plan, err := Derive(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ForkTick < plan.CrisisTick {
		t.Errorf("fork %d before crisis %d", plan.ForkTick, plan.CrisisTick)
	}
	if open, _ := sim.DecisionWindowOpen(sc, sim.Tick(plan.ForkTick)); !open {
		t.Errorf("fork tick %d is not a scheduled decision window", plan.ForkTick)
	}
	if len(plan.Reasons) == 0 {
		t.Error("no reasons")
	}
	// Re-derivation is deterministic.
	again, err := Derive(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := plan.Fixture()
	b, _ := again.Fixture()
	if !bytes.Equal(a, b) {
		t.Error("Derive is not deterministic")
	}
}
