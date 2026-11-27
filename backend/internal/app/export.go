package app

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// InputsExport is a Branch's resolved input prefix for export as an
// Inputs_File: the same inputs, in the same order and with the same
// renumbering that replay feeds the engine, plus their metadata.
type InputsExport struct {
	World  World
	Branch Branch
	// Inputs is the ancestor-resolved input list: each ancestor contributes
	// only inputs with effective tick <= the fork tick of the child below it.
	// Seq is renumbered 1..n in order.
	Inputs []sim.Input
	// Metadata[i] is the provenance and rationale of Inputs[i]. Idempotency
	// keys and model names are not exported.
	Metadata []Metadata
}

// ExportInputs resolves the inputs that govern a Branch up to the Scenario
// horizon with the same ancestor walk that replay uses. It is a pure read.
func (s *BranchService) ExportInputs(ctx context.Context, id BranchID) (InputsExport, error) {
	b, err := s.GetBranch(ctx, id)
	if err != nil {
		return InputsExport{}, err
	}
	w, sc, err := s.GetWorld(ctx, b.WorldID)
	if err != nil {
		return InputsExport{}, err
	}
	recs, err := s.store.InputsForBranch(ctx, id, sim.Tick(sc.HorizonTicks))
	if err != nil {
		return InputsExport{}, fmt.Errorf("inputs of branch %s: %w", id, err)
	}
	ins, err := buildEngineInputs(recs)
	if err != nil {
		return InputsExport{}, err
	}
	md := make([]Metadata, len(recs))
	for i, r := range recs {
		md[i] = Metadata{Provenance: r.Metadata.Provenance, Rationale: r.Metadata.Rationale}
	}
	return InputsExport{World: w, Branch: b, Inputs: ins, Metadata: md}, nil
}
