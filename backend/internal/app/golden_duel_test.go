package app_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/replayfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// Golden demo duel (Req 30.5, 6.4, INV-1).
//
// A Duel on the Black Friday Meltdown at Seed 2026 is forked at the
// Demo_Fork_Tick of demoplan.Derive. The Human Branch submits the
// Demo_Commands at the fork window and ends every later window with no
// Commands; the rule-based AI Branch decides at every window. Both Branches
// run to the horizon. Each Branch's resolved Inputs are exported as an
// Inputs_File (scenarios/demo-duel-human.json, scenarios/demo-duel-ai.json)
// and the State_Hash of each Branch at a few Ticks is committed in
// scenarios/golden/demo-duel.hashes (lines "<branch> <tick|final> <hex>").
//
// The test asserts only reproduction. It never compares the two Branches'
// scores, and no Duel outcome is a golden value.
//
// The files change only with an engine version bump (or a deliberate Scenario
// or policy change); the commit message must say so. Regenerate with:
//
//	UPDATE_GOLDEN=1 go test ./internal/app -run TestGoldenDuelHashes -count=1
//
// The CLI reproduces each hash from the exported Inputs_File (replace <hash>
// with the "final" line of demo-duel.hashes for that Branch):
//
//	go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 --inputs scenarios/demo-duel-human.json --expect <human hash>
//	go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 --inputs scenarios/demo-duel-ai.json --expect <ai hash>
//
// CI runs this test and both commands on amd64 and arm64.

const (
	duelScenarioPath = "../../scenarios/black-friday.json"
	duelHumanPath    = "../../scenarios/demo-duel-human.json"
	duelAIPath       = "../../scenarios/demo-duel-ai.json"
	duelHashesPath   = "../../scenarios/golden/demo-duel.hashes"
	duelSeed         = sim.Seed(2026)
)

func duelExport(t *testing.T, svc *app.BranchService, id app.BranchID) []byte {
	t.Helper()
	ex, err := svc.ExportInputs(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	meta := make([]inputsfile.Meta, len(ex.Metadata))
	for i, m := range ex.Metadata {
		meta[i] = inputsfile.Meta{Provenance: string(m.Provenance), Rationale: m.Rationale}
	}
	ins, err := inputsfile.FromInputs(ex.Inputs, meta)
	if err != nil {
		t.Fatal(err)
	}
	return inputsfile.Print(inputsfile.File{
		ScenarioHash:  hex.EncodeToString(ex.World.ScenarioHash[:]),
		Seed:          ex.World.Seed,
		EngineVersion: ex.World.EngineVersion,
		Inputs:        ins,
	})
}

func TestGoldenDuelHashes(t *testing.T) {
	ctx := context.Background()
	data := bfJSON(t)
	sc, scHash, err := replayfile.LoadScenario(duelScenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := demoplan.Derive(*sc, duelSeed)
	if err != nil {
		t.Fatal(err)
	}
	cmds := plan.SimCommands()
	if len(cmds) == 0 {
		t.Fatal("demo plan has no commands")
	}
	fork := sim.Tick(plan.ForkTick)
	horizon := sim.Tick(sc.HorizonTicks)

	svc := app.NewBranchService(memory.New(), nil, nil)
	_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "golden duel", ScenarioJSON: data, Seed: duelSeed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: root.ID, RunToTick: &fork}); err != nil {
		t.Fatal(err)
	}
	ds := app.NewDuelService(svc, nil)
	du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: fork})
	if err != nil {
		t.Fatal(err)
	}
	sr, err := ds.SubmitHuman(ctx, du.ID, app.SubmitRequest{WindowTick: fork, Commands: cmds, Metadata: app.Metadata{Provenance: app.ProvHuman}})
	if err != nil || len(sr.Accepted) != len(cmds) || len(sr.Rejected) != 0 {
		t.Fatalf("submit demo commands = %+v err %v", sr, err)
	}
	for i := 0; ; i++ {
		if i > 400 {
			t.Fatal("duel did not reach the horizon")
		}
		s, err := ds.DuelStatus(ctx, du.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.HumanHead >= horizon && s.AIHead >= horizon {
			break
		}
		if s.WaitingOnHuman {
			if err := ds.EndHumanWindow(ctx, du.ID, s.HumanHead, nil); err != nil {
				t.Fatal(err)
			}
		}
		to := horizon
		if _, err := ds.AdvanceDuel(ctx, du.ID, &to); err != nil {
			t.Fatal(err)
		}
	}

	ticks := []sim.Tick{fork, fork + 1, 60, horizon}
	branches := []struct {
		name string
		id   app.BranchID
		path string
	}{
		{"human", du.HumanBranchID, duelHumanPath},
		{"ai", du.AIBranchID, duelAIPath},
	}
	var b strings.Builder
	finals := map[string][32]byte{}
	for _, br := range branches {
		for _, tk := range ticks {
			h, err := svc.HashAt(ctx, br.id, tk)
			if err != nil {
				t.Fatal(err)
			}
			label := fmt.Sprint(int64(tk))
			if tk == horizon {
				label = "final"
				finals[br.name] = h
			}
			fmt.Fprintf(&b, "%s %s %s\n", br.name, label, hex.EncodeToString(h[:]))
		}
	}
	gotHashes := b.String()

	exports := map[string][]byte{}
	for _, br := range branches {
		exports[br.path] = duelExport(t, svc, br.id)
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("../../scenarios/golden", 0o755); err != nil {
			t.Fatal(err)
		}
		for p, body := range exports {
			if err := os.WriteFile(p, body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(duelHashesPath, []byte(gotHashes), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The live run reproduces the committed files.
	want, err := os.ReadFile(duelHashesPath)
	if err != nil {
		t.Fatalf("read %s (regenerate with UPDATE_GOLDEN=1): %v", duelHashesPath, err)
	}
	if string(want) != gotHashes {
		t.Fatalf("%s differs (change only with an engine version bump; regenerate with UPDATE_GOLDEN=1)\ngot:\n%swant:\n%s", duelHashesPath, gotHashes, want)
	}
	for p, body := range exports {
		committed, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s (regenerate with UPDATE_GOLDEN=1): %v", p, err)
		}
		if !bytes.Equal(committed, body) {
			t.Fatalf("%s differs from the exported branch inputs (regenerate with UPDATE_GOLDEN=1)\ngot:\n%s\nwant:\n%s", p, body, committed)
		}
	}

	// Both final hashes reproduce through the replay path (validator gate
	// included) from the committed Inputs_Files, at every golden Tick.
	for _, br := range branches {
		f, err := replayfile.LoadInputs(br.path, scHash, duelSeed)
		if err != nil {
			t.Fatalf("%s: %v", br.name, err)
		}
		perTick := map[sim.Tick]string{}
		var tick sim.Tick
		final, err := replayfile.Simulate(sc, duelSeed, &f, -1, func(st sim.State) {
			h := sim.Hash(st)
			perTick[tick] = hex.EncodeToString(h[:])
			tick++
		})
		if err != nil {
			t.Fatalf("%s replay: %v", br.name, err)
		}
		fh := sim.Hash(final)
		if fh != finals[br.name] {
			t.Fatalf("%s replay final hash %x, live %x", br.name, fh, finals[br.name])
		}
		for _, tk := range ticks {
			live, _ := svc.HashAt(ctx, br.id, tk)
			if perTick[tk] != hex.EncodeToString(live[:]) {
				t.Fatalf("%s replay hash at tick %d = %s, live %x", br.name, tk, perTick[tk], live)
			}
		}
	}

	// INV-5: both Branches share the fork Tick State.
	hf, _ := svc.HashAt(ctx, du.HumanBranchID, fork)
	af, _ := svc.HashAt(ctx, du.AIBranchID, fork)
	if hf != af {
		t.Fatalf("fork tick hashes differ: %x vs %x", hf, af)
	}
}
