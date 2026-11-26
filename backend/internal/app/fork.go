package app

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// MaxBranchesPerWorld is the per-World Branch cap (security steering).
const MaxBranchesPerWorld = 32

// ForkBranch creates a child Branch of parent at atTick. No parent Input is
// copied: the child's history is the parent's Input prefix with effective Tick
// <= atTick, resolved by the store's ancestor walk. Commands the parent
// submitted in its window at atTick (effective atTick+1) belong to the
// parent's future and are not visible to the child. The child's Head_Tick is
// atTick and a fork Decision_Window is open there. The parent is never
// modified. An empty manager defaults to ManagerHuman.
func (s *BranchService) ForkBranch(ctx context.Context, parent BranchID, atTick sim.Tick, label string, manager ManagerKind) (Branch, error) {
	if !validName(label) {
		return Branch{}, fmt.Errorf("branch label: %w", ErrInvalidName)
	}
	if manager == "" {
		manager = ManagerHuman
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pc, err := s.load(ctx, parent)
	if err != nil {
		return Branch{}, err
	}
	if atTick < 0 {
		return Branch{}, fmt.Errorf("fork tick %d: %w", atTick, ErrInvalidTick)
	}
	if head := pc.head.state.Tick(); atTick > head {
		return Branch{}, fmt.Errorf("fork tick %d > head %d: %w", atTick, head, ErrNotYetSimulated)
	}
	n, err := s.store.CountBranches(ctx, pc.b.WorldID)
	if err != nil {
		return Branch{}, fmt.Errorf("count branches: %w", err)
	}
	if n >= MaxBranchesPerWorld {
		return Branch{}, fmt.Errorf("world %s has %d branches: %w", pc.b.WorldID, n, ErrBranchLimit)
	}
	child := Branch{
		ID: BranchID(s.newID()), WorldID: pc.b.WorldID, ParentBranchID: parent,
		ForkTick: atTick, HeadTick: atTick, Label: label,
		ManagerKind: manager, CreatedAt: s.now(),
	}
	if err := s.store.CreateBranch(ctx, child); err != nil {
		return Branch{}, fmt.Errorf("create child branch: %w", err)
	}
	return child, nil
}

// GetBranch returns one Branch.
func (s *BranchService) GetBranch(ctx context.Context, id BranchID) (Branch, error) {
	b, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return Branch{}, fmt.Errorf("branch %s: %w", id, err)
	}
	return b, nil
}

// ListBranches returns the World's Branches in creation order.
func (s *BranchService) ListBranches(ctx context.Context, w WorldID) ([]Branch, error) {
	if _, err := s.store.GetWorld(ctx, w); err != nil {
		return nil, fmt.Errorf("world %s: %w", w, err)
	}
	bs, err := s.store.ListBranches(ctx, w)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	return bs, nil
}
