package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

const branchCols = `id::text, world_id::text, COALESCE(parent_id::text, ''), fork_tick, head_tick, label, manager_kind, created_at`

func scanBranch(row pgx.Row) (app.Branch, error) {
	var (
		b          app.Branch
		id, wid, p string
		fork, head int32
		kind       string
	)
	if err := row.Scan(&id, &wid, &p, &fork, &head, &b.Label, &kind, &b.CreatedAt); err != nil {
		return app.Branch{}, err
	}
	b.ID, b.WorldID, b.ParentBranchID = app.BranchID(id), app.WorldID(wid), app.BranchID(p)
	b.ForkTick, b.HeadTick, b.ManagerKind = sim.Tick(fork), sim.Tick(head), app.ManagerKind(kind)
	return b, nil
}

// CreateBranch implements app.BranchRepo.
func (s *Store) CreateBranch(ctx context.Context, b app.Branch) error {
	fork, err := tickToDB(b.ForkTick)
	if err != nil {
		return err
	}
	head, err := tickToDB(b.HeadTick)
	if err != nil {
		return err
	}
	var parent *string
	if b.ParentBranchID != "" {
		p := string(b.ParentBranchID)
		parent = &p
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO branches (id, world_id, parent_id, fork_tick, head_tick, label, manager_kind)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		string(b.ID), string(b.WorldID), parent, fork, head, b.Label, string(b.ManagerKind))
	if err != nil {
		switch pgCode(err) {
		case codeUniqueViolation:
			return fmt.Errorf("branch %s: %w", b.ID, app.ErrAlreadyExists)
		case codeForeignKey:
			return fmt.Errorf("branch %s: world or parent: %w", b.ID, app.ErrNotFound)
		}
		return fmt.Errorf("postgres: create branch: %w", err)
	}
	return nil
}

// GetBranch implements app.BranchRepo.
func (s *Store) GetBranch(ctx context.Context, id app.BranchID) (app.Branch, error) {
	b, err := scanBranch(s.pool.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id = $1`, string(id)))
	if err != nil {
		if isNoRows(err) || pgCode(err) == codeInvalidTextRepr {
			return app.Branch{}, fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
		}
		return app.Branch{}, fmt.Errorf("postgres: get branch: %w", err)
	}
	return b, nil
}

// ListBranches implements app.BranchRepo, in creation order.
func (s *Store) ListBranches(ctx context.Context, w app.WorldID) ([]app.Branch, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+branchCols+` FROM branches WHERE world_id = $1 ORDER BY created_at, id`, string(w))
	if err != nil {
		if pgCode(err) == codeInvalidTextRepr {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: list branches: %w", err)
	}
	defer rows.Close()
	var out []app.Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan branch: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CountBranches implements app.BranchRepo.
func (s *Store) CountBranches(ctx context.Context, w app.WorldID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM branches WHERE world_id = $1`, string(w)).Scan(&n)
	if err != nil {
		if pgCode(err) == codeInvalidTextRepr {
			return 0, nil
		}
		return 0, fmt.Errorf("postgres: count branches: %w", err)
	}
	return n, nil
}

// SetHeadTick implements app.BranchRepo (the one column the runtime role may update).
func (s *Store) SetHeadTick(ctx context.Context, id app.BranchID, t sim.Tick) error {
	tick, err := tickToDB(t)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE branches SET head_tick = $2 WHERE id = $1`, string(id), tick)
	if err != nil {
		if pgCode(err) == codeInvalidTextRepr {
			return fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
		}
		return fmt.Errorf("postgres: set head tick: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
	}
	return nil
}
