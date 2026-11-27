package app_test

import (
	"context"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func TestExportInputsIncludesAncestorsOnlyUpToForkTick(t *testing.T) {
	svc, _, root := newSvc(t)
	ctx := context.Background()
	po := func(qty int64) []sim.Command {
		return []sim.Command{{Kind: sim.CmdPlacePurchaseOrder, SKU: 0, Role: sim.SupplierPrimary, Qty: qty}}
	}
	step := func(id app.BranchID, to sim.Tick) {
		t.Helper()
		if _, err := svc.Advance(ctx, app.AdvanceRequest{Branch: id, RunToTick: &to}); err != nil {
			t.Fatal(err)
		}
	}
	submit := func(id app.BranchID, window sim.Tick, qty int64, md app.Metadata) {
		t.Helper()
		sr, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: id, WindowTick: window, Commands: po(qty), Metadata: md})
		if err != nil || len(sr.Accepted) != 1 {
			t.Fatalf("submit at %d: %+v %v", window, sr, err)
		}
	}
	step(root.ID, 4)
	submit(root.ID, 4, 10, app.Metadata{Provenance: app.ProvAIRuleBased, Rationale: "reorder point"}) // effective 5
	step(root.ID, 8)
	child, err := svc.ForkBranch(ctx, root.ID, 8, "alt", "")
	if err != nil {
		t.Fatal(err)
	}
	submit(root.ID, 8, 20, app.Metadata{}) // parent future: effective 9
	submit(child.ID, 8, 30, app.Metadata{Provenance: app.ProvAILLM})

	ex, err := svc.ExportInputs(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Inputs) != 2 || len(ex.Metadata) != 2 {
		t.Fatalf("child export has %d inputs, want 2: %+v", len(ex.Inputs), ex.Inputs)
	}
	if a, b := ex.Inputs[0], ex.Inputs[1]; a.EffectiveTick != 5 || a.Command.Qty != 10 || a.Seq != 1 ||
		b.EffectiveTick != 9 || b.Command.Qty != 30 || b.Seq != 2 {
		t.Fatalf("inputs = %+v", ex.Inputs)
	}
	if m := ex.Metadata[0]; m.Provenance != app.ProvAIRuleBased || m.Rationale != "reorder point" {
		t.Fatalf("inherited metadata = %+v", m)
	}
	if ex.Metadata[1].Provenance != app.ProvAILLM || ex.Branch.ID != child.ID {
		t.Fatalf("metadata/branch = %+v %+v", ex.Metadata, ex.Branch)
	}

	pe, err := svc.ExportInputs(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pe.Inputs) != 2 || pe.Inputs[1].Command.Qty != 20 {
		t.Fatalf("parent export = %+v", pe.Inputs)
	}
	if _, err := svc.ExportInputs(ctx, "00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatal("export of an unknown branch succeeded")
	}
}
