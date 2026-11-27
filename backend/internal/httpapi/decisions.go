package httpapi

import (
	"net/http"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
)

type decisionRecordDTO struct {
	WindowTick     int64                 `json:"windowTick"`
	ManagerKind    string                `json:"managerKind"`
	Provenance     string                `json:"provenance"`
	Model          string                `json:"model,omitempty"`
	FallbackReason string                `json:"fallbackReason,omitempty"`
	Rationale      string                `json:"rationale"`
	Proposed       []commandDTO          `json:"proposed"`
	AcceptedCount  int                   `json:"acceptedCount"`
	Rejected       []validationResultDTO `json:"rejected"`
	CreatedAt      string                `json:"createdAt"`
}

type decisionsResponse struct {
	BranchID  string              `json:"branchId"`
	Decisions []decisionRecordDTO `json:"decisions"`
}

// listDecisions serves GET /branches/{branchId}/decisions: one record per
// Decision_Window an AI manager handled, including windows with no Commands.
func (s *Server) listDecisions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	recs, err := s.svc.DecisionRecords(r.Context(), app.BranchID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := decisionsResponse{BranchID: id, Decisions: make([]decisionRecordDTO, 0, len(recs))}
	for _, d := range recs {
		dto := decisionRecordDTO{
			WindowTick: int64(d.WindowTick), ManagerKind: string(d.ManagerKind), Provenance: string(d.Provenance),
			Model: d.Model, FallbackReason: d.FallbackReason, Rationale: d.Rationale,
			Proposed: make([]commandDTO, 0, len(d.Proposed)), AcceptedCount: d.AcceptedCount,
			Rejected: make([]validationResultDTO, 0, len(d.Rejected)), CreatedAt: timeString(d.CreatedAt),
		}
		for _, c := range d.Proposed {
			dto.Proposed = append(dto.Proposed, commandToDTO(c))
		}
		for _, v := range d.Rejected {
			dto.Rejected = append(dto.Rejected, validationResultDTO{Index: v.Index, Command: commandToDTO(v.Command), Code: v.Code, Message: v.Message})
		}
		out.Decisions = append(out.Decisions, dto)
	}
	writeJSON(w, http.StatusOK, out)
}
