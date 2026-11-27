package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// WarningEngineVersionMismatch is the code of the warning attached to every
// read of a World recorded under another engine version (Req 22.4).
const WarningEngineVersionMismatch = "engine-version-mismatch"

// ErrEngineVersionMismatch is wrapped by every mutation of a World whose
// stored engine version differs from the running one: the engine cannot
// guarantee identical outcomes, so it refuses to extend the history.
var ErrEngineVersionMismatch = errors.New("app: engine version mismatch")

// Warning is a machine-readable caveat attached to a read response.
type Warning struct {
	Code    string
	Message string
}

// EngineVersionMismatchError names both versions. It matches
// ErrEngineVersionMismatch with errors.Is.
type EngineVersionMismatchError struct {
	World   WorldID
	Stored  string
	Running string
}

func (e *EngineVersionMismatchError) Error() string {
	return fmt.Sprintf("world %s was recorded under engine version %s, this engine is version %s: %v",
		e.World, e.Stored, e.Running, ErrEngineVersionMismatch)
}

// Unwrap lets errors.Is match ErrEngineVersionMismatch.
func (e *EngineVersionMismatchError) Unwrap() error { return ErrEngineVersionMismatch }

// Option configures a BranchService.
type Option func(*BranchService)

// WithEngineVersion overrides the running engine version (default
// sim.EngineVersion). It is meant for tests that need a world recorded under
// another version.
func WithEngineVersion(v int) Option {
	return func(s *BranchService) { s.engineVersion = strconv.Itoa(v) }
}

// engineMismatch returns the mismatch error for w, or nil when the stored
// version equals the running one.
func (s *BranchService) engineMismatch(w World) *EngineVersionMismatchError {
	if w.EngineVersion == s.engineVersion {
		return nil
	}
	return &EngineVersionMismatchError{World: w.ID, Stored: w.EngineVersion, Running: s.engineVersion}
}

// checkMutable rejects a mutation of a mismatched World.
func (s *BranchService) checkMutable(w World) error {
	if e := s.engineMismatch(w); e != nil {
		return e
	}
	return nil
}

func (s *BranchService) worldWarnings(w World) []Warning {
	e := s.engineMismatch(w)
	if e == nil {
		return nil
	}
	return []Warning{{
		Code: WarningEngineVersionMismatch,
		Message: fmt.Sprintf("this world was recorded under engine version %s but the running engine is version %s; "+
			"results may not be reproducible or comparable, and the world is read-only", e.Stored, e.Running),
	}}
}

// WorldEngineWarnings returns the warnings of a World (nil when none).
func (s *BranchService) WorldEngineWarnings(ctx context.Context, id WorldID) ([]Warning, error) {
	w, err := s.store.GetWorld(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("world %s: %w", id, err)
	}
	return s.worldWarnings(w), nil
}

// EngineWarnings returns the warnings of the World that owns the Branch (nil
// when none). Every read response of a mismatched World carries them.
func (s *BranchService) EngineWarnings(ctx context.Context, id BranchID) ([]Warning, error) {
	b, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("branch %s: %w", id, err)
	}
	return s.WorldEngineWarnings(ctx, b.WorldID)
}

// loadMutable is load for operations that extend history. It refuses a World
// recorded under another engine version before any replay is attempted.
// Caller holds s.mu.
func (s *BranchService) loadMutable(ctx context.Context, id BranchID) (branchCtx, error) {
	b, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return branchCtx{}, fmt.Errorf("branch %s: %w", id, err)
	}
	w, err := s.store.GetWorld(ctx, b.WorldID)
	if err != nil {
		return branchCtx{}, fmt.Errorf("world %s: %w", b.WorldID, err)
	}
	if err := s.checkMutable(w); err != nil {
		return branchCtx{}, err
	}
	return s.load(ctx, id)
}
