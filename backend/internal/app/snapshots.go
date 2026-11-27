package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// SnapshotHooks lets callers (tests, operators) observe snapshot behaviour.
// Every field may be nil.
type SnapshotHooks struct {
	// Warn is called when a snapshot is discarded or cannot be saved. A
	// discarded snapshot never changes a served result: the service replays
	// from Tick 0 instead.
	Warn func(branch BranchID, tick sim.Tick, reason string)
	// ReplayStart is called once per replay with the tick it resumes from
	// (0 for a replay from Init).
	ReplayStart func(branch BranchID, from sim.Tick)
}

// SetSnapshotHooks installs hooks. A nil Warn logs through slog.
func (s *BranchService) SetSnapshotHooks(h SnapshotHooks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = h
}

func (s *BranchService) warnSnapshot(b BranchID, t sim.Tick, reason string) {
	if s.hooks.Warn != nil {
		s.hooks.Warn(b, t, reason)
		return
	}
	slog.Warn("snapshot-mismatch", "branch", string(b), "tick", int64(t), "reason", reason)
}

// saveSnapshot stores the complete canonical State of st for the branch when
// st.Tick() is a positive multiple of the scenario's snapshot interval. A
// snapshot is saved only on the branch whose own inputs produced the tick
// (tick > fork tick); ticks at or before a fork are shared history and are
// found on the ancestor chain. Saving is idempotent and best effort: failures
// are warned about and never fail the Advance. Caller holds s.mu.
func (s *BranchService) saveSnapshot(ctx context.Context, sc sim.Scenario, b Branch, st sim.State) {
	iv := sc.SnapshotIntervalTicks
	t := st.Tick()
	if iv <= 0 || t <= 0 || int64(t)%iv != 0 || t <= b.ForkTick {
		return
	}
	blob := sim.Encode(st)
	err := s.store.SaveSnapshot(ctx, Snapshot{BranchID: b.ID, Tick: t, Hash: sim.Hash(st), Blob: blob})
	if err != nil {
		s.warnSnapshot(b.ID, t, "save failed: "+err.Error())
	}
}

// loadSnapshot returns the latest trusted snapshot at or before limit along
// the branch's ancestor chain. A child's ticks <= its fork tick are the
// ancestor's, so each ancestor is searched only up to the fork tick of the
// child below it. A snapshot that fails to decode, whose tick differs from its
// key, or whose re-hash differs from the stored hash is skipped with a warning
// (rows are never deleted) and the caller replays from Tick 0.
func (s *BranchService) loadSnapshot(ctx context.Context, b Branch, limit sim.Tick) (sim.State, bool) {
	if limit < 1 {
		return sim.State{}, false
	}
	var best Snapshot
	found := false
	cur, cap := b, limit
	for {
		snap, ok, err := s.store.LoadLatestSnapshot(ctx, cur.ID, cap)
		if err != nil {
			s.warnSnapshot(cur.ID, cap, "load failed: "+err.Error())
			return sim.State{}, false
		}
		if ok && (!found || snap.Tick > best.Tick) {
			best, found = snap, true
		}
		if cur.ParentBranchID == "" {
			break
		}
		if cur.ForkTick < cap {
			cap = cur.ForkTick
		}
		parent, err := s.store.GetBranch(ctx, cur.ParentBranchID)
		if err != nil {
			s.warnSnapshot(cur.ID, cap, "ancestor lookup failed: "+err.Error())
			return sim.State{}, false
		}
		cur = parent
	}
	if !found {
		return sim.State{}, false
	}
	st, err := sim.Decode(best.Blob)
	switch {
	case err != nil:
		s.warnSnapshot(best.BranchID, best.Tick, "decode failed: "+err.Error())
	case st.Tick() != best.Tick:
		s.warnSnapshot(best.BranchID, best.Tick, fmt.Sprintf("tick mismatch: blob is tick %d", st.Tick()))
	case sim.Hash(st) != best.Hash:
		s.warnSnapshot(best.BranchID, best.Tick, "hash mismatch")
	default:
		return st, true
	}
	return sim.State{}, false
}

// rebuild reconstructs the branch's state at tick to, resuming from the latest
// trusted snapshot when there is one, else from Init. The result holds the
// frames of the ticks it computed (replayed.base is the first). With
// needFrame the state at to is not taken from a snapshot, so the Frame of
// tick `to` is among the computed frames.
func (s *BranchService) rebuild(ctx context.Context, b Branch, w World, sc sim.Scenario, to sim.Tick, needFrame bool) (replayed, error) {
	limit := to
	if needFrame {
		limit = to - 1
	}
	inputs, err := s.engineInputs(ctx, b.ID, to)
	if err != nil {
		return replayed{}, err
	}
	var r replayed
	if st, ok := s.loadSnapshot(ctx, b, limit); ok {
		r = replayed{state: st, base: st.Tick() + 1}
	} else {
		st, err := sim.Init(sc, w.Seed)
		if err != nil {
			return replayed{}, fmt.Errorf("init: %w", err)
		}
		f0, _, err := sim.InitialFrame(sc, st)
		if err != nil {
			return replayed{}, fmt.Errorf("initial frame: %w", err)
		}
		r = replayed{state: st, frames: []sim.TickFrame{f0}}
	}
	if s.hooks.ReplayStart != nil {
		start := r.state.Tick()
		s.hooks.ReplayStart(b.ID, start)
	}
	st := r.state
	for st.Tick() < to {
		step, err := sim.Advance(sc, st, inputsAt(inputs, st.Tick()+1))
		if err != nil {
			if errors.Is(err, sim.ErrHorizonReached) {
				return replayed{}, ErrHorizonReached
			}
			return replayed{}, fmt.Errorf("replay to tick %d: %w", st.Tick()+1, err)
		}
		st = step.State
		r.state = st
		r.frames = append(r.frames, step.Frame)
	}
	return r, nil
}
