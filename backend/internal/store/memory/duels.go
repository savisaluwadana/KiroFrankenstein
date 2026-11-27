package memory

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
)

var _ app.DuelRepo = (*Store)(nil)

// CreateDuel implements app.DuelRepo. There is no update or delete.
func (s *Store) CreateDuel(_ context.Context, d app.Duel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.duels[d.ID]; ok {
		return fmt.Errorf("duel %s: %w", d.ID, app.ErrAlreadyExists)
	}
	if _, ok := s.worlds[d.WorldID]; !ok {
		return fmt.Errorf("world %s: %w", d.WorldID, app.ErrNotFound)
	}
	for _, b := range []app.BranchID{d.ParentBranchID, d.HumanBranchID, d.AIBranchID} {
		if _, ok := s.branches[b]; !ok {
			return fmt.Errorf("branch %s: %w", b, app.ErrNotFound)
		}
	}
	s.duels[d.ID] = d
	return nil
}

// GetDuel implements app.DuelRepo.
func (s *Store) GetDuel(_ context.Context, id app.DuelID) (app.Duel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.duels[id]
	if !ok {
		return app.Duel{}, fmt.Errorf("duel %s: %w", id, app.ErrNotFound)
	}
	return d, nil
}

// DuelForBranch implements app.DuelBranchLookup.
func (s *Store) DuelForBranch(_ context.Context, id app.BranchID) (app.Duel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.duels {
		if d.HumanBranchID == id || d.AIBranchID == id {
			return d, true, nil
		}
	}
	return app.Duel{}, false, nil
}
