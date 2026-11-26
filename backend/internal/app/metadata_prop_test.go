package app_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// miMeta is the metadata one world is created and driven with.
type miMeta struct {
	worldName        string
	manager          app.ManagerKind
	prov             app.Provenance
	idemPrefix       string
	rationale, model string
	clockStart       time.Time
	idPrefix         string
}

// miStep is one decision window's commands, identical in both worlds.
type miStep struct {
	cmds []sim.Command
}

func miGenMeta(t *rapid.T, label string) miMeta {
	name := rapid.StringMatching(`[A-Za-z0-9 _\-]{1,40}`).Draw(t, label+"Name")
	return miMeta{
		worldName:  name,
		manager:    rapid.SampledFrom([]app.ManagerKind{app.ManagerHuman, app.ManagerAIRuleBased, app.ManagerAILLM}).Draw(t, label+"Manager"),
		prov:       rapid.SampledFrom([]app.Provenance{app.ProvHuman, app.ProvAIRuleBased, app.ProvAILLM, app.ProvAIRuleBasedFallback}).Draw(t, label+"Prov"),
		idemPrefix: rapid.StringMatching(`[a-z0-9]{0,12}`).Draw(t, label+"Idem"),
		rationale:  rapid.StringMatching(`[A-Za-z0-9 .,]{0,60}`).Draw(t, label+"Rationale"),
		model:      rapid.StringMatching(`[a-z0-9/_.\-]{0,24}`).Draw(t, label+"Model"),
		clockStart: time.Unix(rapid.Int64Range(0, 4_000_000_000).Draw(t, label+"Clock"), 0).UTC(),
		idPrefix:   rapid.StringMatching(`[a-f0-9]{8}`).Draw(t, label+"IDs"),
	}
}

func miGenPlan(t *rapid.T, windows int) []miStep {
	plan := make([]miStep, windows)
	for i := range plan {
		n := rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("n%d", i))
		for j := 0; j < n; j++ {
			plan[i].cmds = append(plan[i].cmds, sim.Command{
				Kind: sim.CmdPlacePurchaseOrder,
				SKU:  rapid.Int64Range(0, 1).Draw(t, fmt.Sprintf("sku%d_%d", i, j)),
				Role: sim.SupplierPrimary,
				Qty:  rapid.Int64Range(1, 60).Draw(t, fmt.Sprintf("qty%d_%d", i, j)),
			})
		}
	}
	return plan
}

type miTrace struct {
	hashes  [][32]byte
	metrics []sim.Metrics
	results []string
}

// Feature: parallel-simulation-mvp, Property 6: Hash metadata independence
//
// **Validates: Requirements 12.4**
func TestPropHashMetadataIndependence(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	rapid.Check(t, func(t *rapid.T) {
		seed := sim.Seed(rapid.Int64Range(1, 1<<40).Draw(t, "seed"))
		target := sim.Tick(rapid.IntRange(24, 48).Draw(t, "target"))
		plan := miGenPlan(t, int(target)/4)
		a := miGenMeta(t, "a")
		b := miGenMeta(t, "b")

		ta := miRun(t, data, seed, a, plan, target)
		tb := miRun(t, data, seed, b, plan, target)

		if !reflect.DeepEqual(ta.results, tb.results) {
			t.Fatalf("submission outcomes differ: %v vs %v", ta.results, tb.results)
		}
		if len(ta.hashes) != len(tb.hashes) || len(ta.hashes) < int(target)+1 {
			t.Fatalf("frame counts %d, %d, want >= %d", len(ta.hashes), len(tb.hashes), target+1)
		}
		for k := range ta.hashes {
			if ta.hashes[k] != tb.hashes[k] {
				t.Fatalf("tick %d hash differs: %x vs %x (meta %+v vs %+v)", k, ta.hashes[k], tb.hashes[k], a, b)
			}
			if !reflect.DeepEqual(ta.metrics[k], tb.metrics[k]) {
				t.Fatalf("tick %d metrics differ", k)
			}
		}
	})
}

// miRun drives one world with plain (window-stopping) advances: the plan entry
// index is the window number (tick/4), so both worlds submit at the same ticks.
func miRun(t *rapid.T, data []byte, seed sim.Seed, m miMeta, plan []miStep, target sim.Tick) miTrace {
	ctx := context.Background()
	var idn int
	newID := func() string { idn++; return fmt.Sprintf("%s-%d", m.idPrefix, idn) }
	clock := m.clockStart
	now := func() time.Time { clock = clock.Add(37 * time.Second); return clock }
	svc := app.NewBranchService(memory.New(), newID, now)
	_, b, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: m.worldName, ScenarioJSON: data, Seed: seed, Manager: m.manager})
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	var tr miTrace
	for {
		head, err := svc.HeadTick(ctx, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if head >= target {
			break
		}
		if obs, err := svc.ObservationAt(ctx, b.ID, head); err == nil && obs.Window.Open {
			if idx := int(head) / 4; idx < len(plan) && len(plan[idx].cmds) > 0 {
				req := app.SubmitRequest{
					Branch: b.ID, WindowTick: head, Commands: plan[idx].cmds,
					Metadata: app.Metadata{Provenance: m.prov, Rationale: m.rationale, ModelName: m.model},
				}
				if m.idemPrefix != "" {
					req.IdempotencyKey = fmt.Sprintf("%s-%d", m.idemPrefix, head)
				}
				res, err := svc.SubmitCommands(ctx, req)
				if err != nil {
					t.Fatalf("submit at %d: %v", head, err)
				}
				tr.results = append(tr.results, fmt.Sprintf("t%d acc=%d rej=%d", head, len(res.Accepted), len(res.Rejected)))
				for _, r := range res.Rejected {
					tr.results = append(tr.results, fmt.Sprintf("  rej %v", r.Code))
				}
			}
		}
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: b.ID, RunToTick: nil}); err != nil {
			t.Fatalf("advance from %d: %v", head, err)
		}
	}
	head, _ := svc.HeadTick(ctx, b.ID)
	for k := sim.Tick(0); k <= head; k++ {
		h, err := svc.HashAt(ctx, b.ID, k)
		if err != nil {
			t.Fatal(err)
		}
		mt, err := svc.MetricsAt(ctx, b.ID, k)
		if err != nil {
			t.Fatal(err)
		}
		tr.hashes = append(tr.hashes, h)
		tr.metrics = append(tr.metrics, mt)
	}
	return tr
}
