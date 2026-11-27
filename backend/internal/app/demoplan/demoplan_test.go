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

func TestDemoPlanFlagshipHasNoStaffingCommand(t *testing.T) {
	plan, err := Derive(loadFlagship(t), DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Commands) != 1 || plan.Commands[0].Kind != "place_purchase_order" {
		t.Fatalf("commands = %+v, want the single backup purchase order", plan.Commands)
	}
	found := false
	for _, r := range plan.Reasons {
		if r == "no capacity-bound order after the fork tick in the no-command run: no staffing command proposed" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing no-capacity-bound reason in %v", plan.Reasons)
	}
}

// With a smaller regular workforce the no-command run is capacity bound, so the
// plan gains a hire_temp_workers Command that passes the validator.
func TestDeriveAddsStaffingWhenCapacityBound(t *testing.T) {
	sc := loadFlagship(t)
	sc.Workforce.RegularWorkers = 4
	plan, err := Derive(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	var hire *Command
	for i := range plan.Commands {
		if plan.Commands[i].Kind == "hire_temp_workers" {
			hire = &plan.Commands[i]
		}
	}
	if hire == nil {
		t.Fatalf("no staffing command; reasons: %v", plan.Reasons)
	}
	if hire.Qty < 1 || hire.Qty > sc.Workforce.MaxTempWorkers {
		t.Errorf("hire qty %d outside 1..%d", hire.Qty, sc.Workforce.MaxTempWorkers)
	}
	st, err := sim.Init(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	for int64(st.Tick()) < plan.ForkTick {
		step, err := sim.Advance(sc, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		st = step.State
	}
	for _, v := range sim.Validate(sc, sim.Observe(sc, st), plan.SimCommands(), sc.CommandBudget) {
		if !v.OK {
			t.Errorf("rejected: %s %s", v.Code, v.Message)
		}
	}
}
