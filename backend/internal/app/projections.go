package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// ErrProjectionMismatch is returned by VerifyProjections when a stored
// tick_summaries hash differs from the replayed hash.
var ErrProjectionMismatch = errors.New("app: tick summary hash differs from replayed hash")

// writeProjections stores one tick_summaries row per newly computed frame in a
// single batch. The projection is a derived cache: a failure is logged and
// never fails the Advance. Caller holds s.mu.
func (s *BranchService) writeProjections(ctx context.Context, id BranchID, frames []sim.TickFrame) {
	if s.proj == nil || len(frames) == 0 {
		return
	}
	rows := make([]TickSummary, 0, len(frames))
	for _, f := range frames {
		m, err := json.Marshal(f.Metrics)
		if err != nil {
			slog.Warn("tick-summary-encode", "branch", string(id), "tick", int64(f.Tick), "err", err)
			return
		}
		r, err := json.Marshal(f.Report)
		if err != nil {
			slog.Warn("tick-summary-encode", "branch", string(id), "tick", int64(f.Tick), "err", err)
			return
		}
		rows = append(rows, TickSummary{BranchID: id, Tick: f.Tick, Hash: f.Hash, Metrics: m, FCR: r})
	}
	if err := s.proj.UpsertTickSummaries(ctx, rows); err != nil {
		slog.Warn("tick-summary-write", "branch", string(id), "err", err)
	}
}

// checkProjections compares the stored hashes with the frames of a fresh
// replay and warns about every difference. Stored rows are never trusted over
// the replay. Caller holds s.mu.
func (s *BranchService) checkProjections(ctx context.Context, id BranchID, r replayed) {
	if s.proj == nil || len(r.frames) == 0 {
		return
	}
	rows, err := s.proj.TickSummaries(ctx, id, r.base, r.state.Tick())
	if err != nil {
		s.warnSnapshot(id, r.state.Tick(), "tick summaries load failed: "+err.Error())
		return
	}
	for _, row := range rows {
		if f, ok := r.frame(row.Tick); ok && f.Hash != row.Hash {
			s.warnSnapshot(id, row.Tick, "tick summary hash mismatch")
		}
	}
}

// VerifyProjections replays the branch from Tick 0 and checks that every
// stored tick_summaries hash equals the replayed hash. It returns the number
// of rows checked, or an error wrapping ErrProjectionMismatch.
func (s *BranchService) VerifyProjections(ctx context.Context, id BranchID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proj == nil {
		return 0, nil
	}
	c, err := s.load(ctx, id)
	if err != nil {
		return 0, err
	}
	head := c.head.state.Tick()
	inputs, err := s.engineInputs(ctx, id, head)
	if err != nil {
		return 0, err
	}
	st, err := sim.Init(c.sc, c.w.Seed)
	if err != nil {
		return 0, fmt.Errorf("init: %w", err)
	}
	hashes := make(map[sim.Tick][32]byte, int(head)+1)
	hashes[0] = sim.Hash(st)
	for st.Tick() < head {
		step, err := sim.Advance(c.sc, st, inputsAt(inputs, st.Tick()+1))
		if err != nil {
			return 0, fmt.Errorf("replay to tick %d: %w", st.Tick()+1, err)
		}
		st = step.State
		hashes[st.Tick()] = step.Frame.Hash
	}
	rows, err := s.proj.TickSummaries(ctx, id, 0, head)
	if err != nil {
		return 0, fmt.Errorf("load tick summaries: %w", err)
	}
	for _, row := range rows {
		if want, ok := hashes[row.Tick]; !ok || want != row.Hash {
			return 0, fmt.Errorf("branch %s tick %d: %w", id, row.Tick, ErrProjectionMismatch)
		}
	}
	return len(rows), nil
}
