package postgres

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

var _ app.DuelRepo = (*Store)(nil)

// CreateDuel implements app.DuelRepo. The table is append-only.
func (s *Store) CreateDuel(ctx context.Context, d app.Duel) error {
	tick, err := tickToDB(d.ForkTick)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO duels (id, world_id, parent_branch_id, human_branch_id, ai_branch_id,
		                    ai_kind, fork_tick, comparison_policy, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		string(d.ID), string(d.WorldID), string(d.ParentBranchID), string(d.HumanBranchID),
		string(d.AIBranchID), string(d.AIKind), tick, d.ComparisonPolicy, d.CreatedAt)
	if err != nil {
		switch pgCode(err) {
		case codeUniqueViolation:
			return fmt.Errorf("duel %s: %w", d.ID, app.ErrAlreadyExists)
		case codeForeignKey, codeInvalidTextRepr:
			return fmt.Errorf("duel %s references: %w", d.ID, app.ErrNotFound)
		}
		return fmt.Errorf("postgres: create duel: %w", err)
	}
	return nil
}

// GetDuel implements app.DuelRepo.
func (s *Store) GetDuel(ctx context.Context, id app.DuelID) (app.Duel, error) {
	var (
		d                                  app.Duel
		did, wid, pid, hid, aid, kind, pol string
		tick                               int32
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, world_id::text, parent_branch_id::text, human_branch_id::text, ai_branch_id::text,
		        ai_kind, fork_tick, comparison_policy, created_at
		 FROM duels WHERE id = $1`, string(id)).
		Scan(&did, &wid, &pid, &hid, &aid, &kind, &tick, &pol, &d.CreatedAt)
	if err != nil {
		if isNoRows(err) || pgCode(err) == codeInvalidTextRepr {
			return app.Duel{}, fmt.Errorf("duel %s: %w", id, app.ErrNotFound)
		}
		return app.Duel{}, fmt.Errorf("postgres: get duel: %w", err)
	}
	d.ID, d.WorldID, d.ParentBranchID = app.DuelID(did), app.WorldID(wid), app.BranchID(pid)
	d.HumanBranchID, d.AIBranchID, d.AIKind = app.BranchID(hid), app.BranchID(aid), app.ManagerKind(kind)
	d.ForkTick, d.ComparisonPolicy, d.CreatedAt = sim.Tick(tick), pol, d.CreatedAt.UTC()
	return d, nil
}

// DuelForBranch implements app.DuelBranchLookup.
func (s *Store) DuelForBranch(ctx context.Context, id app.BranchID) (app.Duel, bool, error) {
	var did string
	err := s.pool.QueryRow(ctx,
		`SELECT id::text FROM duels WHERE human_branch_id = $1 OR ai_branch_id = $1 LIMIT 1`, string(id)).Scan(&did)
	if err != nil {
		if isNoRows(err) || pgCode(err) == codeInvalidTextRepr {
			return app.Duel{}, false, nil
		}
		return app.Duel{}, false, fmt.Errorf("postgres: duel for branch: %w", err)
	}
	d, err := s.GetDuel(ctx, app.DuelID(did))
	if err != nil {
		return app.Duel{}, false, err
	}
	return d, true, nil
}
