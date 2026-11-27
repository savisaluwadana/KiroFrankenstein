package calibration

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

const scenarioPath = "../../../scenarios/black-friday.json"

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

// TestCalibrationGates holds the only two calibration gates (Req 36.1, 36.2,
// 36.6). Neither compares a Human Manager with an AI Manager, and neither
// requires any arm to win.
func TestCalibrationGates(t *testing.T) {
	sc := loadFlagship(t)
	seed := DefaultSeed

	t.Run("36.1 no-command run has a stockout with open orders", func(t *testing.T) {
		st, err := sim.Init(sc, seed)
		if err != nil {
			t.Fatal(err)
		}
		found := int64(-1)
		for int64(st.Tick()) < sc.HorizonTicks {
			step, err := sim.Advance(sc, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			st = step.State
			open := map[int64]bool{}
			for _, o := range st.Orders.Open {
				open[o.SKU] = true
			}
			for sku := range st.Inventory {
				if st.Inventory[sku].OnHand == 0 && open[int64(sku)] {
					found = int64(st.Tick())
				}
			}
			if found >= 0 {
				break
			}
		}
		if found < 0 {
			t.Fatal("no Tick has a SKU with zero on-hand stock and open orders in the no-command run")
		}
		t.Logf("first stockout with open orders at tick %d (seed %d)", found, uint64(seed))
	})

	t.Run("36.2 rule-based manager scores above the no-command run", func(t *testing.T) {
		none, err := Run(sc, seed, Arm{Name: "no commands"})
		if err != nil {
			t.Fatal(err)
		}
		rb, err := Run(sc, seed, Arm{Name: "rule-based AI", RuleBased: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("composite (hundredths): no commands %d, rule-based %d (seed %d)", none.Score.Composite, rb.Score.Composite, uint64(seed))
		if rb.Score.Composite <= none.Score.Composite {
			t.Fatalf("rule-based composite %d is not above the no-command composite %d", rb.Score.Composite, none.Score.Composite)
		}
	})
}

// TestCalibrationObservations records facts about the lever structure. It
// asserts nothing about who wins; it fails only when the harness cannot run.
// Run with -v to read the log lines that docs/calibration.md quotes.
func TestCalibrationObservations(t *testing.T) {
	sc := loadFlagship(t)
	plan, err := demoplan.Derive(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	arms := StandardArms(sc, plan)
	all, err := RunAll(sc, Seeds(20), arms)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string][]Result{}
	for i, a := range arms {
		byName[a.Name] = all[i]
	}
	none, inv, staff, both := byName["no commands"], byName["inventory-only (restock 4 SKUs)"], byName["staffing-only (8 temps)"], byName["both (restock + 8 temps)"]

	// Informational: capacity-bound orders occur in the no-command run after the surge onset.
	capAfter := 0
	for _, r := range none {
		for _, tk := range r.CapBoundTickList {
			if tk >= 48 {
				capAfter++
				break
			}
		}
	}
	t.Logf("info: no-command runs with a capacity-bound order at or after the surge onset: %d of %d seeds", capAfter, len(none))

	// Informational: which plan scores higher, by seed and by metric.
	cmp := func(name string, a, b []Result) {
		var profitA, scoreA, svcA, laborA int
		for i := range a {
			if a[i].Metrics.NetProfit > b[i].Metrics.NetProfit {
				profitA++
			}
			if a[i].Score.Composite > b[i].Score.Composite {
				scoreA++
			}
			if a[i].Metrics.ServiceLevel > b[i].Metrics.ServiceLevel {
				svcA++
			}
			if a[i].Metrics.LaborExpense < b[i].Metrics.LaborExpense {
				laborA++
			}
		}
		t.Logf("info: %s: higher profit on %d, higher composite on %d, higher service level on %d, lower labor on %d of %d seeds", name, profitA, scoreA, svcA, laborA, len(a))
	}
	cmp("staffing-only vs inventory-only", staff, inv)
	cmp("inventory-only vs staffing-only", inv, staff)
	cmp("inventory-only vs no commands", inv, none)
	cmp("both vs inventory-only", both, inv)
	cmp("both vs staffing-only", both, staff)

	var sb strings.Builder
	WriteTable(&sb, all)
	t.Log("\n" + sb.String())
}

// TestRunIsDeterministic checks that the harness is a pure function of
// (scenario, seed, arm).
func TestRunIsDeterministic(t *testing.T) {
	sc := loadFlagship(t)
	plan, err := demoplan.Derive(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	for _, arm := range StandardArms(sc, plan) {
		a, err := Run(sc, 7, arm)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Run(sc, 7, arm)
		if err != nil {
			t.Fatal(err)
		}
		if a.Hash != b.Hash || a.Score != b.Score {
			t.Errorf("arm %q: two runs differ", arm.Name)
		}
	}
}

// TestNoCommandArmMatchesEngine checks the harness against a plain engine loop.
func TestNoCommandArmMatchesEngine(t *testing.T) {
	sc := loadFlagship(t)
	st, err := sim.Init(sc, DefaultSeed)
	if err != nil {
		t.Fatal(err)
	}
	for int64(st.Tick()) < sc.HorizonTicks {
		step, err := sim.Advance(sc, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		st = step.State
	}
	r, err := Run(sc, DefaultSeed, Arm{Name: "no commands"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Hash != sim.Hash(st) {
		t.Fatal("harness no-command hash differs from the plain engine loop")
	}
}

func TestWriteReportCarriesTables(t *testing.T) {
	sc := loadFlagship(t)
	var buf bytes.Buffer
	if err := WriteReport(&buf, sc, Seeds(1)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"### Arms", "### Evidence at the default Seed", "### Lever sweeps", "no commands", "rule-based AI", "recorded demo human plan"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("report lacks %q", want)
		}
	}
}
