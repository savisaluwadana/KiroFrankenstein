package memory

import (
	"context"
	"fmt"
	"sort"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// UpsertTickSummaries implements app.ProjectionRepo.
func (s *Store) UpsertTickSummaries(_ context.Context, rows []app.TickSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range rows {
		if _, ok := s.branches[r.BranchID]; !ok {
			return fmt.Errorf("branch %s: %w", r.BranchID, app.ErrNotFound)
		}
	}
	for _, r := range rows {
		m := s.summaries[r.BranchID]
		if m == nil {
			m = map[sim.Tick]app.TickSummary{}
			s.summaries[r.BranchID] = m
		}
		r.Metrics, r.FCR = cloneBytes(r.Metrics), cloneBytes(r.FCR)
		m[r.Tick] = r
	}
	return nil
}

// TickSummaries implements app.ProjectionRepo.
func (s *Store) TickSummaries(_ context.Context, id app.BranchID, from, to sim.Tick) ([]app.TickSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []app.TickSummary
	for t, r := range s.summaries[id] {
		if t >= from && t <= to {
			r.Metrics, r.FCR = cloneBytes(r.Metrics), cloneBytes(r.FCR)
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tick < out[j].Tick })
	return out, nil
}
