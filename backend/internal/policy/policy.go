// Package policy defines the AI manager abstraction shared by the rule-based
// policy (internal/policy/rulebased) and the LLM adapter
// (internal/policy/llm). A Manager only proposes Commands from an Observation;
// the deterministic validator (sim.Validate) remains the gate and a Manager
// never mutates state.
package policy

import (
	"context"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Proposal is what a Manager returns for one Decision_Window. Commands are in
// priority order and never exceed the budget passed to Propose. Rationale is
// untrusted display text.
type Proposal struct {
	Commands  []sim.Command
	Rationale string
}

// Manager proposes up to budget Commands from the Observation alone. The
// rule-based implementation is pure and ignores ctx; the LLM adapter uses it
// for timeouts and cancellation.
type Manager interface {
	// Name is the display label, for example "Rule-based AI".
	Name() string
	Propose(ctx context.Context, obs sim.Observation, budget int) (Proposal, error)
}
