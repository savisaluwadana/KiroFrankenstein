package ensemble

import (
	"context"
	"encoding/hex"
	"errors"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

const (
	scenarioPath  = "../../../scenarios/black-friday.json"
	demoHumanPath = "../../../scenarios/demo-duel-human.json"
	demoHumanHash = "9786f5397548d09290ed9b5f122172eb009546118d1d1b9d8d9d31015b8daca8"
)

func load(t *testing.T) (*sim.Scenario, string, inputsfile.File) {
	t.Helper()
	sc, h, err := replayfile.LoadScenario(scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := replayfile.LoadInputsAnySeed(demoHumanPath, h)
	if err != nil {
		t.Fatal(err)
	}
	return sc, h, f
}

func seeds(t *testing.T, n int) []sim.Seed {
	t.Helper()
	s, err := SeedRange(1, n)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRejectedBeforeSimulating(t *testing.T) {
	sc, h, f := load(t)
	fixed := Arm{Kind: KindFixedReplay, File: &f}
	var inits atomic.Int64
	base := Config{Scenario: sc, ScenarioHash: h, Arms: []Arm{fixed}, onInit: func() { inits.Add(1) }}

	seedList := make([]sim.Seed, 51)
	for i := range seedList {
		seedList[i] = sim.Seed(i + 1)
	}
	dup := []sim.Seed{3, 4, 3}
	tests := []struct {
		name string
		mut  func(*Config)
		want error
	}{
		{"51 seeds", func(c *Config) { c.Seeds = seedList }, ErrTooManySeeds},
		{"duplicate seeds", func(c *Config) { c.Seeds = dup }, ErrDuplicateSeeds},
		{"no seeds", func(c *Config) {}, ErrNoSeeds},
		{"llm arm", func(c *Config) { c.Seeds = []sim.Seed{1}; c.Arms = []Arm{fixed, {Kind: KindLLM}} }, ErrUnsupportedArm},
		{"hash mismatch", func(c *Config) {
			bad := f
			bad.ScenarioHash = strings.Repeat("0", 64)
			c.Seeds = []sim.Seed{1}
			c.Arms = []Arm{{Kind: KindFixedReplay, File: &bad}}
		}, ErrMismatch},
		{"version mismatch", func(c *Config) {
			bad := f
			bad.EngineVersion = "999"
			c.Seeds = []sim.Seed{1}
			c.Arms = []Arm{{Kind: KindFixedReplay, File: &bad}}
		}, ErrMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mut(&cfg)
			if _, err := Run(context.Background(), cfg); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if n := inits.Load(); n != 0 {
				t.Fatalf("%d runs were simulated before rejection", n)
			}
		})
	}
	if _, err := ParseArmSpec("llm"); !errors.Is(err, ErrUnsupportedArm) {
		t.Fatalf("ParseArmSpec(llm) = %v", err)
	}
	if _, err := ParseSeeds("1..51"); !errors.Is(err, ErrTooManySeeds) {
		t.Fatalf("ParseSeeds(1..51) = %v", err)
	}
}

func TestParse(t *testing.T) {
	if s, err := ParseSeeds("1..3"); err != nil || !reflect.DeepEqual(s, []sim.Seed{1, 2, 3}) {
		t.Fatalf("range: %v %v", s, err)
	}
	if s, err := ParseSeeds("7, 5,6"); err != nil || !reflect.DeepEqual(s, []sim.Seed{7, 5, 6}) {
		t.Fatalf("list: %v %v", s, err)
	}
	a, err := ParseArmSpec("adaptive:x.json@40")
	if err != nil || a.Kind != KindAdaptiveEvaluation || a.Path != "x.json" || a.StartTick != 40 {
		t.Fatalf("adaptive: %+v %v", a, err)
	}
	if _, err := ParseArmSpec("bogus"); !errors.Is(err, ErrInvalidArm) {
		t.Fatalf("bogus: %v", err)
	}
}

func TestFixedReplayOriginalSeed(t *testing.T) {
	sc, h, f := load(t)
	res, err := Run(context.Background(), Config{Scenario: sc, ScenarioHash: h, Seeds: []sim.Seed{f.Seed},
		Arms: []Arm{{Kind: KindFixedReplay, File: &f}}})
	if err != nil {
		t.Fatal(err)
	}
	r := res.Runs[0]
	if got := hex.EncodeToString(r.Hash[:]); got != demoHumanHash {
		t.Fatalf("hash = %s, want %s", got, demoHumanHash)
	}
	if len(r.Skipped) != 0 {
		t.Fatalf("skipped = %+v", r.Skipped)
	}
	// Same as the CLI replay path.
	st, err := replayfile.Simulate(sc, f.Seed, &f, -1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sim.Hash(st) != r.Hash {
		t.Fatal("ensemble hash differs from replayfile.Simulate")
	}
}

func command(tick sim.Tick, c sim.Command) inputsfile.Input {
	return inputsfile.Input{EffectiveTick: tick, Seq: 1, Command: &c, Provenance: inputsfile.ProvHuman}
}

func TestFixedReplaySkipsInvalidCommand(t *testing.T) {
	sc, h, demo := load(t)
	good := *demo.Inputs[0].Command
	// A purchase order far beyond available cash, and an expedite of a PO that
	// does not exist.
	huge := sim.Command{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: sim.MaxOrderQuantity}
	ghost := sim.Command{Kind: sim.CmdExpeditePurchaseOrder, PORef: 987654}
	mk := func(ins ...inputsfile.Input) *inputsfile.File {
		return &inputsfile.File{ScenarioHash: h, Seed: demo.Seed, EngineVersion: demo.EngineVersion, Inputs: ins}
	}
	tick := demo.Inputs[0].EffectiveTick
	withBad := mk(command(tick, good), command(tick, huge), command(tick, ghost))
	onlyGood := mk(command(tick, good))

	run := func(f *inputsfile.File) RunResult {
		res, err := Run(context.Background(), Config{Scenario: sc, ScenarioHash: h, Seeds: []sim.Seed{777}, Arms: []Arm{{Kind: KindFixedReplay, File: f}}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Runs[0]
	}
	a, b := run(withBad), run(onlyGood)
	if len(a.Skipped) != 2 {
		t.Fatalf("skipped = %+v", a.Skipped)
	}
	if a.Skipped[0].Reason != string(sim.ReasonInsufficientCash) || a.Skipped[0].Kind != "place_purchase_order" || a.Skipped[0].Tick != int64(tick) {
		t.Fatalf("skipped[0] = %+v", a.Skipped[0])
	}
	if a.Skipped[1].Reason != string(sim.ReasonUnknownPO) || a.Skipped[1].Kind != "expedite_purchase_order" {
		t.Fatalf("skipped[1] = %+v", a.Skipped[1])
	}
	// Nothing was altered or substituted: the run equals the run without them.
	if a.Hash != b.Hash {
		t.Fatal("skipped commands changed the run")
	}
	if len(b.Skipped) != 0 {
		t.Fatalf("good plan skipped %+v", b.Skipped)
	}

	// A command outside any decision window is skipped with its own code.
	off := mk(command(sim.Tick(3), good))
	if r := run(off); len(r.Skipped) != 1 || r.Skipped[0].Reason != ReasonNoDecisionWindow {
		t.Fatalf("off-window skipped = %+v", r.Skipped)
	}
}

func TestAdaptiveDeterministicAndConcurrencyOrder(t *testing.T) {
	sc, h, f := load(t)
	seedList := seeds(t, 4)
	cfg := Config{Scenario: sc, ScenarioHash: h, Seeds: seedList, Arms: []Arm{
		{Kind: KindFixedReplay, File: &f},
		{Kind: KindAdaptiveEvaluation, StartTick: 0},
	}}
	cfg.Concurrency = 1
	a, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Concurrency = 8
	b, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("results differ between concurrency 1 and 8")
	}
	if len(a.Runs) != 8 {
		t.Fatalf("runs = %d", len(a.Runs))
	}
	for i, r := range a.Runs {
		wantArm, wantSeed := i/4, sim.Seed(i%4+1)
		if r.ArmIndex != wantArm || r.Seed != wantSeed {
			t.Fatalf("run %d = arm %d seed %d", i, r.ArmIndex, uint64(r.Seed))
		}
	}
	decided := 0
	for _, r := range a.Runs[4:] {
		decided += r.Decisions
	}
	if decided == 0 {
		t.Fatal("the adaptive arm never decided")
	}
	// Paired runs share the Seed: same exogenous draws, but different arms differ.
	if a.Runs[0].Hash == a.Runs[4].Hash {
		t.Fatal("arms produced identical states")
	}
}

func TestAdaptivePrefix(t *testing.T) {
	sc, h, f := load(t)
	start := DefaultStart(&f)
	res, err := Run(context.Background(), Config{Scenario: sc, ScenarioHash: h, Seeds: []sim.Seed{f.Seed},
		Arms: []Arm{{Kind: KindAdaptiveEvaluation, File: &f, StartTick: start}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Runs[0].Skipped) != 0 {
		t.Fatalf("skipped = %+v", res.Runs[0].Skipped)
	}
	if res.Runs[0].Arm != LabelAdaptive+" (start tick "+strconv.FormatInt(start, 10)+")" {
		t.Fatalf("label = %q", res.Runs[0].Arm)
	}
}

func TestNoDatabaseOrNetworkImports(t *testing.T) {
	banned := []string{"net/http", "net", "database/sql", "github.com/jackc/pgx", "internal/store/postgres", "internal/httpapi"}
	for _, file := range []string{"runner.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, b := range banned {
				if p == b || strings.HasPrefix(p, b+"/") || strings.HasSuffix(p, b) {
					t.Errorf("%s imports %s", file, p)
				}
			}
		}
	}
}
