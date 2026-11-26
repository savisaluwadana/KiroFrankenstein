package app

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// GetWorld returns a World and its parsed Scenario (with overrides applied,
// as stored). The Scenario includes the Disruption_Script; callers that
// expose it must not forward that part.
func (s *BranchService) GetWorld(ctx context.Context, id WorldID) (World, sim.Scenario, error) {
	w, err := s.store.GetWorld(ctx, id)
	if err != nil {
		return World{}, sim.Scenario{}, fmt.Errorf("world %s: %w", id, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc, ok := s.scenarios[id]; ok {
		return w, sc, nil
	}
	p, err := sim.ParseScenario(w.ScenarioJSON)
	if err != nil {
		return World{}, sim.Scenario{}, fmt.Errorf("stored scenario of %s: %w", id, err)
	}
	s.scenarios[id] = *p
	return w, *p, nil
}
