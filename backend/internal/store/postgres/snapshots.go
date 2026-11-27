package postgres

import (
	"bytes"
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// SaveSnapshot implements app.SnapshotRepo: a no-op for the same hash, and
// app.ErrSnapshotConflict for a different hash at the same (branch, tick).
func (s *Store) SaveSnapshot(ctx context.Context, snap app.Snapshot) error {
	tick, err := tickToDB(snap.Tick)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO snapshots (branch_id, tick, state_hash, blob) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (branch_id, tick) DO NOTHING`,
		string(snap.BranchID), tick, snap.Hash[:], nonNil(snap.Blob))
	if err != nil {
		switch pgCode(err) {
		case codeForeignKey, codeInvalidTextRepr:
			return fmt.Errorf("branch %s: %w", snap.BranchID, app.ErrNotFound)
		}
		return fmt.Errorf("postgres: save snapshot: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var existing []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT state_hash FROM snapshots WHERE branch_id = $1 AND tick = $2`, string(snap.BranchID), tick).
		Scan(&existing); err != nil {
		return fmt.Errorf("postgres: load existing snapshot: %w", err)
	}
	if bytes.Equal(existing, snap.Hash[:]) {
		return nil
	}
	return fmt.Errorf("branch %s tick %d: %w", snap.BranchID, snap.Tick, app.ErrSnapshotConflict)
}

// LoadLatestSnapshot implements app.SnapshotRepo.
func (s *Store) LoadLatestSnapshot(ctx context.Context, id app.BranchID, atOrBefore sim.Tick) (app.Snapshot, bool, error) {
	var (
		out  app.Snapshot
		tick int32
		hash []byte
	)
	err := s.pool.QueryRow(ctx,
		`SELECT tick, state_hash, blob FROM snapshots
		 WHERE branch_id = $1 AND tick <= $2 ORDER BY tick DESC LIMIT 1`,
		string(id), tickLimitToDB(atOrBefore)).Scan(&tick, &hash, &out.Blob)
	if err != nil {
		if isNoRows(err) || pgCode(err) == codeInvalidTextRepr {
			return app.Snapshot{}, false, nil
		}
		return app.Snapshot{}, false, fmt.Errorf("postgres: load snapshot: %w", err)
	}
	out.BranchID, out.Tick = id, sim.Tick(tick)
	copy(out.Hash[:], hash)
	return out, true, nil
}
