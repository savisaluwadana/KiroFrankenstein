package app

import (
	"context"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// LLMDeciderForTest returns the registered LLM decider of the service, so a
// test can call Decide from many goroutines without the service mutex.
func (s *BranchService) LLMDeciderForTest(sc sim.Scenario) AIDecider { return s.llm.factory(sc) }

// EnsureDecidedForTest makes the AI Branch decide at its head window, like the
// Duel does before it advances.
func (s *BranchService) EnsureDecidedForTest(ctx context.Context, id BranchID) error {
	return s.ensureDecided(ctx, id)
}

// InflightForTest is the number of provider requests in flight.
func (s *BranchService) InflightForTest() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inflight)
}
