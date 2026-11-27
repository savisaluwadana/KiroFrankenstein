package app_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// diffFixture: root advanced to 8, child forked at 4 with one extra order
// command (effective 5) and advanced to 8 as well.
func diffFixture(t *testing.T) (*app.BranchService, app.Branch, app.Branch) {
	t.Helper()
	svc, _, root := newSvc(t)
	ctx := context.Background()
	to := func(id app.BranchID, n sim.Tick) {
		t.Helper()
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id, RunToTick: &n}); err != nil {
			t.Fatal(err)
		}
	}
	to(root.ID, 4)
	child, err := svc.ForkBranch(ctx, root.ID, 4, "alt", "")
	if err != nil {
		t.Fatal(err)
	}
	cmd := []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: 50}}
	sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: child.ID, WindowTick: 4, Commands: cmd})
	if err != nil || len(sr.Accepted) != 1 {
		t.Fatalf("submit: %+v %v", sr, err)
	}
	to(root.ID, 8)
	to(child.ID, 8)
	return svc, root, child
}

func TestDiffForkVersusParent(t *testing.T) {
	svc, root, child := diffFixture(t)
	ctx := context.Background()
	d, err := svc.Diff(ctx, root.ID, child.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.ComparisonTick != 8 {
		t.Fatalf("comparison tick = %d", d.ComparisonTick)
	}
	if len(d.DifferingInputs) != 1 {
		t.Fatalf("differing inputs = %+v", d.DifferingInputs)
	}
	in := d.DifferingInputs[0]
	if in.Side != "B" || in.Branch != child.ID || in.EffectiveTick != 5 || in.Command == nil || in.Command.Qty != 50 {
		t.Fatalf("input = %+v", in)
	}
	if d.FirstDivergenceTick == nil || *d.FirstDivergenceTick < in.EffectiveTick {
		t.Fatalf("first divergence = %v", d.FirstDivergenceTick)
	}
	if len(d.DifferingComponents) == 0 {
		t.Fatal("no differing components")
	}
	// A purchase order changes state before it changes any outcome, so both
	// journals may be equal at the divergence tick (FirstEventA/B nil).

	if len(d.Metrics) == 0 || len(d.SKUStock) == 0 {
		t.Fatalf("metrics %d stock %d", len(d.Metrics), len(d.SKUStock))
	}
	for _, m := range d.Metrics {
		if m.Delta != m.B-m.A {
			t.Fatalf("metric %+v", m)
		}
	}
	if d.Statement != app.DiffStatement || d.Disclaimer != sim.Disclaimer || d.Currency == "" {
		t.Fatalf("labels: %q %q %q", d.Statement, d.Disclaimer, d.Currency)
	}

	// Symmetry of divergence and inputs.
	r, err := svc.Diff(ctx, child.ID, root.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.FirstDivergenceTick == nil || *r.FirstDivergenceTick != *d.FirstDivergenceTick || len(r.DifferingInputs) != 1 {
		t.Fatalf("reverse diff = %+v", r)
	}
	for i, m := range r.Metrics {
		if m.Delta != -d.Metrics[i].Delta {
			t.Fatalf("metric %s not negated", m.Key)
		}
	}

	// Earlier comparison tick: before the input takes effect, nothing differs.
	four := sim.Tick(4)
	e, err := svc.Diff(ctx, root.ID, child.ID, &four)
	if err != nil {
		t.Fatal(err)
	}
	if e.FirstDivergenceTick != nil || len(e.DifferingInputs) != 0 {
		t.Fatalf("diff at fork tick = %+v", e)
	}
}

func TestDiffIdenticalBranches(t *testing.T) {
	svc, root, _ := diffFixture(t)
	d, err := svc.Diff(context.Background(), root.ID, root.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.FirstDivergenceTick != nil || len(d.DifferingInputs) != 0 || len(d.DifferingComponents) != 0 {
		t.Fatalf("self diff = %+v", d)
	}
	for _, m := range d.Metrics {
		if m.Delta != 0 {
			t.Fatalf("metric %+v", m)
		}
	}
}

func TestDiffRejections(t *testing.T) {
	svc, root, child := diffFixture(t)
	ctx := context.Background()
	over := sim.Tick(9)
	if _, err := svc.Diff(ctx, root.ID, child.ID, &over); !errors.Is(err, app.ErrNotYetSimulated) {
		t.Fatalf("over-head err = %v", err)
	}
	data, err := os.ReadFile("../../scenarios/black-friday.json")
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "w2", ScenarioJSON: data, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Diff(ctx, root.ID, other.ID, nil); !errors.Is(err, app.ErrIncomparableBranches) {
		t.Fatalf("cross-world err = %v", err)
	}
}
