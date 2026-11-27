package postgres

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
)

// CreateWorld implements app.WorldRepo.
func (s *Store) CreateWorld(ctx context.Context, w app.World) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO worlds (id, name, scenario_hash, scenario_json, seed, engine_version)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		string(w.ID), w.Name, w.ScenarioHash[:], nonNil(w.ScenarioJSON), seedToDB(w.Seed), w.EngineVersion)
	if err != nil {
		if pgCode(err) == codeUniqueViolation {
			return fmt.Errorf("world %s: %w", w.ID, app.ErrAlreadyExists)
		}
		return fmt.Errorf("postgres: create world: %w", err)
	}
	return nil
}

// GetWorld implements app.WorldRepo.
func (s *Store) GetWorld(ctx context.Context, id app.WorldID) (app.World, error) {
	var (
		w    app.World
		hash []byte
		seed int64
		wid  string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, name, scenario_hash, scenario_json, seed, engine_version, created_at
		 FROM worlds WHERE id = $1`, string(id)).
		Scan(&wid, &w.Name, &hash, &w.ScenarioJSON, &seed, &w.EngineVersion, &w.CreatedAt)
	if err != nil {
		if isNoRows(err) || pgCode(err) == codeInvalidTextRepr {
			return app.World{}, fmt.Errorf("world %s: %w", id, app.ErrNotFound)
		}
		return app.World{}, fmt.Errorf("postgres: get world: %w", err)
	}
	w.ID = app.WorldID(wid)
	copy(w.ScenarioHash[:], hash)
	w.Seed = seedFromDB(seed)
	return w, nil
}

// nonNil keeps bytea NOT NULL columns from receiving a NULL for a nil slice.
func nonNil(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}
