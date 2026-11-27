package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
)

var _ app.DecisionRepo = (*Store)(nil)

func cloneDecision(d app.DecisionRecord) app.DecisionRecord {
	d.Proposed = append(d.Proposed[:0:0], d.Proposed...)
	d.Rejected = append(d.Rejected[:0:0], d.Rejected...)
	return d
}

// AppendDecision implements app.DecisionRepo. There is no update or delete.
func (s *Store) AppendDecision(_ context.Context, d app.DecisionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.branches[d.BranchID]; !ok {
		return fmt.Errorf("branch %s: %w", d.BranchID, app.ErrNotFound)
	}
	for _, e := range s.decisions[d.BranchID] {
		if e.WindowTick == d.WindowTick {
			return fmt.Errorf("decision of branch %s tick %d: %w", d.BranchID, d.WindowTick, app.ErrAlreadyExists)
		}
	}
	list := append(s.decisions[d.BranchID], cloneDecision(d))
	sort.Slice(list, func(i, j int) bool { return list[i].WindowTick < list[j].WindowTick })
	s.decisions[d.BranchID] = list
	return nil
}

// DecisionsForBranch implements app.DecisionRepo.
func (s *Store) DecisionsForBranch(_ context.Context, id app.BranchID) ([]app.DecisionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]app.DecisionRecord, 0, len(s.decisions[id]))
	for _, d := range s.decisions[id] {
		out = append(out, cloneDecision(d))
	}
	return out, nil
}

// LLMUsageSince implements app.DecisionRepo.
func (s *Store) LLMUsageSince(_ context.Context, since time.Time) (app.LLMUsageSum, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var u app.LLMUsageSum
	for _, recs := range s.decisions {
		for _, r := range recs {
			if !r.CreatedAt.Before(since) {
				u.Calls += r.LLMCallsCharged
				u.PromptTokens += r.PromptTokens
				u.CompletionTokens += r.CompletionTokens
			}
		}
	}
	return u, nil
}
