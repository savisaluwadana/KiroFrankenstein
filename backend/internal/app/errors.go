package app

import "errors"

// Domain errors of the Branch_Service. The HTTP layer maps them to
// problem+json; callers match with errors.Is.
var (
	// ErrHistoryImmutable: an Input would take effect at a Tick that has
	// already been computed (effective Tick <= Head_Tick). Changing the past
	// is a fork (Req 13.3).
	ErrHistoryImmutable = errors.New("app: history is immutable; fork to change the past")
	// ErrNotYetSimulated: a read or window refers to a Tick above Head_Tick.
	ErrNotYetSimulated = errors.New("app: tick is not yet simulated")
	// ErrInvalidTick: a negative Tick or a Run_To_Tick that is not ahead of the head.
	ErrInvalidTick = errors.New("app: invalid tick")
	// ErrHorizonReached: the branch is at the scenario horizon.
	ErrHorizonReached = errors.New("app: horizon reached")
	// ErrWindowNotOpen: no Decision_Window is open at the head Tick.
	ErrWindowNotOpen = errors.New("app: no decision window is open")
	// ErrBudgetExceeded: the window's Command_Budget is already spent.
	ErrBudgetExceeded = errors.New("app: command budget exceeded")
	// ErrStaleHead: the caller's expected Head_Tick differs from the actual one.
	ErrStaleHead = errors.New("app: stale head tick")
	// ErrInvalidScenario: the scenario or an override failed validation.
	ErrInvalidScenario = errors.New("app: invalid scenario")
	// ErrInvalidName: a name is empty-after-trim, too long or not printable.
	ErrInvalidName = errors.New("app: invalid name")
	// ErrBranchLimit: the World already has the maximum number of Branches.
	ErrBranchLimit = errors.New("app: branch limit reached")
	// ErrUnsupportedInput: a stored input kind the engine cannot apply yet.
	ErrUnsupportedInput = errors.New("app: unsupported input kind")
)
