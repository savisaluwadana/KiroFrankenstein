package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

const inputCols = `branch_id::text, seq, effective_tick, kind, payload, provenance,
	COALESCE(idempotency_key, ''), COALESCE(rationale, ''), COALESCE(model_name, '')`

func scanInput(row pgx.Row) (app.InputRecord, error) {
	var (
		r    app.InputRecord
		bid  string
		seq  int64
		tick int32
		kind int16
		prov string
	)
	if err := row.Scan(&bid, &seq, &tick, &kind, &r.Payload, &prov,
		&r.Metadata.IdempotencyKey, &r.Metadata.Rationale, &r.Metadata.ModelName); err != nil {
		return app.InputRecord{}, err
	}
	r.BranchID, r.Seq, r.EffectiveTick = app.BranchID(bid), uint64(seq), sim.Tick(tick)
	r.Kind, r.Metadata.Provenance = sim.InputKind(kind), app.Provenance(prov)
	return r, nil
}

// AppendInput implements app.InputRepo.
//
// Seq is MAX(seq)+1 computed inside a transaction that first takes a
// transaction-scoped advisory lock keyed by the branch. Concurrent appends to
// the same branch therefore serialize and get gapless numbers; appends to
// different branches do not block each other. A counter row was rejected
// because the runtime role has no UPDATE on branches beyond head_tick, and
// input_log rows can never be updated. A hashtext collision between two
// branches only causes harmless extra serialization. The INSERT also uses
// ON CONFLICT on the partial unique index (branch_id, idempotency_key) so a
// duplicate key can never create a row even without the lock.
func (s *Store) AppendInput(ctx context.Context, id app.BranchID, in app.NewInput) (app.InputRecord, error) {
	tick, err := tickToDB(in.EffectiveTick)
	if err != nil {
		return app.InputRecord{}, err
	}
	var key *string
	if k := in.Metadata.IdempotencyKey; k != "" {
		key = &k
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.InputRecord{}, fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, string(id)); err != nil {
		if pgCode(err) == codeInvalidTextRepr {
			return app.InputRecord{}, fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
		}
		return app.InputRecord{}, fmt.Errorf("postgres: lock: %w", err)
	}

	rec, err := scanInput(tx.QueryRow(ctx,
		`INSERT INTO input_log (branch_id, seq, effective_tick, kind, payload, provenance, idempotency_key, rationale, model_name)
		 SELECT $1::uuid, COALESCE(MAX(seq), 0) + 1, $2::integer, $3::smallint, $4::bytea, $5::text, $6::text, $7::text, $8::text
		 FROM input_log WHERE branch_id = $1::uuid
		 ON CONFLICT (branch_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		 RETURNING `+inputCols,
		string(id), tick, int16(in.Kind), nonNil(in.Payload), string(in.Metadata.Provenance), key,
		nullable(in.Metadata.Rationale), nullable(in.Metadata.ModelName)))
	switch {
	case err == nil:
		if err := tx.Commit(ctx); err != nil {
			return app.InputRecord{}, fmt.Errorf("postgres: commit: %w", err)
		}
		return rec, nil
	case isNoRows(err) && key != nil:
		existing, err := scanInput(tx.QueryRow(ctx,
			`SELECT `+inputCols+` FROM input_log WHERE branch_id = $1::uuid AND idempotency_key = $2`, string(id), *key))
		if err != nil {
			return app.InputRecord{}, fmt.Errorf("postgres: load duplicate: %w", err)
		}
		return existing, fmt.Errorf("branch %s key %q: %w", id, *key, app.ErrDuplicateIdempotencyKey)
	case pgCode(err) == codeForeignKey:
		return app.InputRecord{}, fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
	default:
		return app.InputRecord{}, fmt.Errorf("postgres: append input: %w", err)
	}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// FindByIdempotencyKey implements app.InputRepo.
func (s *Store) FindByIdempotencyKey(ctx context.Context, id app.BranchID, key string) (app.InputRecord, bool, error) {
	if key == "" {
		return app.InputRecord{}, false, nil
	}
	r, err := scanInput(s.pool.QueryRow(ctx,
		`SELECT `+inputCols+` FROM input_log WHERE branch_id = $1 AND idempotency_key = $2`, string(id), key))
	if err != nil {
		if isNoRows(err) || pgCode(err) == codeInvalidTextRepr {
			return app.InputRecord{}, false, nil
		}
		return app.InputRecord{}, false, fmt.Errorf("postgres: find idempotency key: %w", err)
	}
	return r, true, nil
}

// InputsForBranch implements app.InputRepo with one recursive CTE.
//
// chain walks from the branch up to the root. depth is 0 for the branch itself
// and grows toward the root; lim is the highest effective tick that level may
// contribute: the caller's upTo for the branch, then the minimum of that and
// the fork tick of the child below for each ancestor. The final ordering is
// effective tick, then ancestor-first (depth descending), then seq, the same as
// the memory store.
func (s *Store) InputsForBranch(ctx context.Context, id app.BranchID, upTo sim.Tick) ([]app.InputRecord, error) {
	if _, err := s.GetBranch(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
WITH RECURSIVE chain(id, parent_id, depth, lim) AS (
  SELECT b.id, b.parent_id, 0, $2::integer
  FROM branches b WHERE b.id = $1::uuid
  UNION ALL
  SELECT p.id, p.parent_id, c.depth + 1, LEAST(c.lim, child.fork_tick)
  FROM chain c
  JOIN branches child ON child.id = c.id
  JOIN branches p ON p.id = c.parent_id
)
SELECT l.branch_id::text, l.seq, l.effective_tick, l.kind, l.payload, l.provenance,
       COALESCE(l.idempotency_key, ''), COALESCE(l.rationale, ''), COALESCE(l.model_name, '')
FROM chain
JOIN input_log l ON l.branch_id = chain.id AND l.effective_tick <= chain.lim
ORDER BY l.effective_tick, chain.depth DESC, l.seq`, string(id), tickLimitToDB(upTo))
	if err != nil {
		return nil, fmt.Errorf("postgres: inputs for branch: %w", err)
	}
	defer rows.Close()
	var out []app.InputRecord
	for rows.Next() {
		r, err := scanInput(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan input: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
