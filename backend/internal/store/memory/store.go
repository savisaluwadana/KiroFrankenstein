package memory

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Store is the thread-safe in-memory implementation of app.Store. It has no
// method that updates or deletes a recorded input.
type Store struct {
	mu        sync.Mutex
	worlds    map[app.WorldID]app.World
	branches  map[app.BranchID]app.Branch
	order     []app.BranchID // creation order
	inputs    map[app.BranchID][]app.InputRecord
	keys      map[app.BranchID]map[string]int // key -> index in inputs
	snapshots map[app.BranchID][]app.Snapshot // sorted by tick
	summaries map[app.BranchID]map[sim.Tick]app.TickSummary
	decisions map[app.BranchID][]app.DecisionRecord
	duels     map[app.DuelID]app.Duel
}

var _ app.Store = (*Store)(nil)
var _ app.ProjectionRepo = (*Store)(nil)

// New returns an empty Store.
func New() *Store {
	return &Store{
		worlds:    map[app.WorldID]app.World{},
		branches:  map[app.BranchID]app.Branch{},
		inputs:    map[app.BranchID][]app.InputRecord{},
		keys:      map[app.BranchID]map[string]int{},
		snapshots: map[app.BranchID][]app.Snapshot{},
		summaries: map[app.BranchID]map[sim.Tick]app.TickSummary{},
		decisions: map[app.BranchID][]app.DecisionRecord{},
		duels:     map[app.DuelID]app.Duel{},
	}
}

func cloneBytes(b []byte) []byte { return append([]byte(nil), b...) }

func cloneRecord(r app.InputRecord) app.InputRecord {
	r.Payload = cloneBytes(r.Payload)
	return r
}

// CreateWorld implements app.WorldRepo.
func (s *Store) CreateWorld(_ context.Context, w app.World) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.worlds[w.ID]; ok {
		return fmt.Errorf("world %s: %w", w.ID, app.ErrAlreadyExists)
	}
	w.ScenarioJSON = cloneBytes(w.ScenarioJSON)
	s.worlds[w.ID] = w
	return nil
}

// GetWorld implements app.WorldRepo.
func (s *Store) GetWorld(_ context.Context, id app.WorldID) (app.World, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.worlds[id]
	if !ok {
		return app.World{}, fmt.Errorf("world %s: %w", id, app.ErrNotFound)
	}
	w.ScenarioJSON = cloneBytes(w.ScenarioJSON)
	return w, nil
}

// CreateBranch implements app.BranchRepo.
func (s *Store) CreateBranch(_ context.Context, b app.Branch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.branches[b.ID]; ok {
		return fmt.Errorf("branch %s: %w", b.ID, app.ErrAlreadyExists)
	}
	if _, ok := s.worlds[b.WorldID]; !ok {
		return fmt.Errorf("world %s: %w", b.WorldID, app.ErrNotFound)
	}
	if b.ParentBranchID != "" {
		if _, ok := s.branches[b.ParentBranchID]; !ok {
			return fmt.Errorf("parent branch %s: %w", b.ParentBranchID, app.ErrNotFound)
		}
	}
	s.branches[b.ID] = b
	s.order = append(s.order, b.ID)
	return nil
}

// GetBranch implements app.BranchRepo.
func (s *Store) GetBranch(_ context.Context, id app.BranchID) (app.Branch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.branches[id]
	if !ok {
		return app.Branch{}, fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
	}
	return b, nil
}

// ListBranches implements app.BranchRepo, in creation order.
func (s *Store) ListBranches(_ context.Context, w app.WorldID) ([]app.Branch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []app.Branch
	for _, id := range s.order {
		if b := s.branches[id]; b.WorldID == w {
			out = append(out, b)
		}
	}
	return out, nil
}

// CountBranches implements app.BranchRepo.
func (s *Store) CountBranches(ctx context.Context, w app.WorldID) (int, error) {
	l, err := s.ListBranches(ctx, w)
	return len(l), err
}

// SetHeadTick implements app.BranchRepo.
func (s *Store) SetHeadTick(_ context.Context, id app.BranchID, t sim.Tick) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.branches[id]
	if !ok {
		return fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
	}
	b.HeadTick = t
	s.branches[id] = b
	return nil
}

// AppendInput implements app.InputRepo.
func (s *Store) AppendInput(_ context.Context, id app.BranchID, in app.NewInput) (app.InputRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.branches[id]; !ok {
		return app.InputRecord{}, fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
	}
	key := in.Metadata.IdempotencyKey
	if key != "" {
		if i, ok := s.keys[id][key]; ok {
			return cloneRecord(s.inputs[id][i]), fmt.Errorf("branch %s key %q: %w", id, key, app.ErrDuplicateIdempotencyKey)
		}
	}
	rec := app.InputRecord{
		BranchID:      id,
		Seq:           uint64(len(s.inputs[id])) + 1,
		EffectiveTick: in.EffectiveTick,
		Kind:          in.Kind,
		Payload:       cloneBytes(in.Payload),
		Metadata:      in.Metadata,
	}
	s.inputs[id] = append(s.inputs[id], rec)
	if key != "" {
		if s.keys[id] == nil {
			s.keys[id] = map[string]int{}
		}
		s.keys[id][key] = len(s.inputs[id]) - 1
	}
	return cloneRecord(rec), nil
}

// FindByIdempotencyKey implements app.InputRepo.
func (s *Store) FindByIdempotencyKey(_ context.Context, id app.BranchID, key string) (app.InputRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.keys[id][key]; ok && key != "" {
		return cloneRecord(s.inputs[id][i]), true, nil
	}
	return app.InputRecord{}, false, nil
}

// InputsForBranch implements app.InputRepo by walking the ancestor chain.
func (s *Store) InputsForBranch(_ context.Context, id app.BranchID, upTo sim.Tick) ([]app.InputRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.branches[id]; !ok {
		return nil, fmt.Errorf("branch %s: %w", id, app.ErrNotFound)
	}
	type item struct {
		rec   app.InputRecord
		depth int // 0 = root-most ancestor
	}
	var items []item
	// Collect the chain from the branch up to the root.
	var chain []app.Branch
	for cur := id; cur != ""; {
		b := s.branches[cur]
		chain = append(chain, b)
		cur = b.ParentBranchID
	}
	limit := upTo
	for i, b := range chain {
		depth := len(chain) - 1 - i
		for _, r := range s.inputs[b.ID] {
			if r.EffectiveTick <= limit {
				items = append(items, item{cloneRecord(r), depth})
			}
		}
		if b.ParentBranchID != "" && b.ForkTick < limit {
			limit = b.ForkTick
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.rec.EffectiveTick != b.rec.EffectiveTick {
			return a.rec.EffectiveTick < b.rec.EffectiveTick
		}
		if a.depth != b.depth {
			return a.depth < b.depth
		}
		return a.rec.Seq < b.rec.Seq
	})
	out := make([]app.InputRecord, len(items))
	for i, it := range items {
		out[i] = it.rec
	}
	return out, nil
}

// SaveSnapshot implements app.SnapshotRepo.
func (s *Store) SaveSnapshot(_ context.Context, snap app.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.branches[snap.BranchID]; !ok {
		return fmt.Errorf("branch %s: %w", snap.BranchID, app.ErrNotFound)
	}
	list := s.snapshots[snap.BranchID]
	for _, e := range list {
		if e.Tick == snap.Tick {
			if bytes.Equal(e.Hash[:], snap.Hash[:]) {
				return nil
			}
			return fmt.Errorf("branch %s tick %d: %w", snap.BranchID, snap.Tick, app.ErrSnapshotConflict)
		}
	}
	snap.Blob = cloneBytes(snap.Blob)
	list = append(list, snap)
	sort.Slice(list, func(i, j int) bool { return list[i].Tick < list[j].Tick })
	s.snapshots[snap.BranchID] = list
	return nil
}

// LoadLatestSnapshot implements app.SnapshotRepo.
func (s *Store) LoadLatestSnapshot(_ context.Context, id app.BranchID, atOrBefore sim.Tick) (app.Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.snapshots[id]
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Tick <= atOrBefore {
			out := list[i]
			out.Blob = cloneBytes(out.Blob)
			return out, true, nil
		}
	}
	return app.Snapshot{}, false, nil
}
