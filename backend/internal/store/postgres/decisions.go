package postgres

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

var _ app.DecisionRepo = (*Store)(nil)

const insertDecisionSQL = `INSERT INTO decision_records
 (branch_id, window_tick, manager_kind, provenance, model, fallback_reason, rationale,
  proposed, accepted_count, rejected, created_at, llm_calls_charged, prompt_tokens, completion_tokens)
 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

// AppendDecision implements app.DecisionRepo. The table is append-only: the
// runtime role has no UPDATE or DELETE and triggers reject both.
func (s *Store) AppendDecision(ctx context.Context, d app.DecisionRecord) error {
	tick, err := tickToDB(d.WindowTick)
	if err != nil {
		return err
	}
	proposed, err := app.EncodeProposed(d.Proposed)
	if err != nil {
		return err
	}
	rejected, err := app.EncodeRejected(d.Rejected)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, insertDecisionSQL, string(d.BranchID), tick, string(d.ManagerKind), string(d.Provenance),
		d.Model, d.FallbackReason, d.Rationale, proposed, int32(d.AcceptedCount), rejected, d.CreatedAt,
		int16(d.LLMCallsCharged), int32(d.PromptTokens), int32(d.CompletionTokens))
	if err != nil {
		switch pgCode(err) {
		case codeUniqueViolation:
			return fmt.Errorf("decision of branch %s tick %d: %w", d.BranchID, d.WindowTick, app.ErrAlreadyExists)
		case codeForeignKey, codeInvalidTextRepr:
			return fmt.Errorf("branch %s: %w", d.BranchID, app.ErrNotFound)
		}
		return fmt.Errorf("postgres: append decision: %w", err)
	}
	return nil
}

// DecisionsForBranch implements app.DecisionRepo.
func (s *Store) DecisionsForBranch(ctx context.Context, id app.BranchID) ([]app.DecisionRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT window_tick, manager_kind, provenance, model, fallback_reason, rationale,
		        proposed::text, accepted_count, rejected::text, created_at,
		        llm_calls_charged, prompt_tokens, completion_tokens
		 FROM decision_records WHERE branch_id = $1 ORDER BY window_tick`, string(id))
	if err != nil {
		if pgCode(err) == codeInvalidTextRepr {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: decision records: %w", err)
	}
	defer rows.Close()
	var out []app.DecisionRecord
	for rows.Next() {
		var (
			tick, accepted             int32
			calls                      int16
			ptok, ctok                 int32
			kind, prov, p, r, rat, mod string
			fb                         string
			d                          app.DecisionRecord
		)
		if err := rows.Scan(&tick, &kind, &prov, &mod, &fb, &rat, &p, &accepted, &r, &d.CreatedAt, &calls, &ptok, &ctok); err != nil {
			return nil, fmt.Errorf("postgres: scan decision record: %w", err)
		}
		d.BranchID, d.WindowTick, d.ManagerKind, d.Provenance = id, sim.Tick(tick), app.ManagerKind(kind), app.Provenance(prov)
		d.Model, d.FallbackReason, d.Rationale, d.AcceptedCount = mod, fb, rat, int(accepted)
		d.LLMCallsCharged, d.PromptTokens, d.CompletionTokens = int(calls), int(ptok), int(ctok)
		if d.Proposed, err = app.DecodeProposed([]byte(p)); err != nil {
			return nil, err
		}
		if d.Rejected, err = app.DecodeRejected([]byte(r)); err != nil {
			return nil, err
		}
		d.CreatedAt = d.CreatedAt.UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}
