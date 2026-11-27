package httpapi

import (
	"net/http"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

// exportBranch serves GET /branches/{branchId}/export: the canonical
// Inputs_File of the Branch, including inherited ancestor inputs up to each
// fork Tick.
func (s *Server) exportBranch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ex, err := s.svc.ExportInputs(r.Context(), app.BranchID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	meta := make([]inputsfile.Meta, len(ex.Metadata))
	for i, m := range ex.Metadata {
		meta[i] = inputsfile.Meta{Provenance: string(m.Provenance), Rationale: m.Rationale}
	}
	inputs, err := inputsfile.FromInputs(ex.Inputs, meta)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	body := inputsfile.Print(inputsfile.File{
		ScenarioHash:  hashHex(ex.World.ScenarioHash),
		Seed:          ex.World.Seed,
		EngineVersion: ex.World.EngineVersion,
		Inputs:        inputs,
	})
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
