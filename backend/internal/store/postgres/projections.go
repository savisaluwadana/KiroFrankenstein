package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

var _ app.ProjectionRepo = (*Store)(nil)

const upsertSummarySQL = `INSERT INTO tick_summaries (branch_id, tick, state_hash, metrics, fcr)
 VALUES ($1, $2, $3, $4, $5)
 ON CONFLICT (branch_id, tick) DO UPDATE
 SET state_hash = EXCLUDED.state_hash, metrics = EXCLUDED.metrics, fcr = EXCLUDED.fcr`

// UpsertTickSummaries implements app.ProjectionRepo with one batched round
// trip in one transaction.
func (s *Store) UpsertTickSummaries(ctx context.Context, rows []app.TickSummary) error {
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		tick, err := tickToDB(r.Tick)
		if err != nil {
			return err
		}
		batch.Queue(upsertSummarySQL, string(r.BranchID), tick, r.Hash[:], r.Metrics, r.FCR)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	br := tx.SendBatch(ctx, batch)
	for range rows {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			switch pgCode(err) {
			case codeForeignKey, codeInvalidTextRepr:
				return fmt.Errorf("tick summaries: %w", app.ErrNotFound)
			}
			return fmt.Errorf("postgres: upsert tick summary: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("postgres: close batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	return nil
}

// TickSummaries implements app.ProjectionRepo.
func (s *Store) TickSummaries(ctx context.Context, id app.BranchID, from, to sim.Tick) ([]app.TickSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT tick, state_hash, metrics::text, fcr::text FROM tick_summaries
		 WHERE branch_id = $1 AND tick >= $2 AND tick <= $3 ORDER BY tick`,
		string(id), tickLimitToDB(from), tickLimitToDB(to))
	if err != nil {
		if pgCode(err) == codeInvalidTextRepr {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: tick summaries: %w", err)
	}
	defer rows.Close()
	var out []app.TickSummary
	for rows.Next() {
		var (
			tick int32
			hash []byte
			m, f string
		)
		if err := rows.Scan(&tick, &hash, &m, &f); err != nil {
			return nil, fmt.Errorf("postgres: scan tick summary: %w", err)
		}
		r := app.TickSummary{BranchID: id, Tick: sim.Tick(tick), Metrics: []byte(m), FCR: []byte(f)}
		copy(r.Hash[:], hash)
		out = append(out, r)
	}
	return out, rows.Err()
}
