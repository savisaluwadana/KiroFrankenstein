package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// RejectedProposal is one proposed Command the deterministic validator
// refused, with its machine-readable reason. Command is the proposal as made.
type RejectedProposal struct {
	Index   int
	Command sim.Command
	Code    string
	Message string
}

// DecisionRecord is what an AI manager did at one Decision_Window of one
// Branch. It exists even when no Command was proposed or accepted, so the
// window always reports its provenance. Rationale is untrusted display text.
// It is recorded metadata: replay never reads it (INV-13, INV-15).
type DecisionRecord struct {
	BranchID       BranchID
	WindowTick     sim.Tick
	ManagerKind    ManagerKind
	Provenance     Provenance
	Model          string // model name of an LLM proposal, else empty
	FallbackReason string // set with ProvAIRuleBasedFallback, else empty
	Rationale      string
	Proposed       []sim.Command
	AcceptedCount  int
	Rejected       []RejectedProposal
	CreatedAt      time.Time

	// LLM usage of the window, kept so the per-World counters survive a
	// restart (Req 20.14, 20.16). LLMCallsCharged is 1 when a request was
	// reserved against the World's call budget, even if it failed.
	LLMCallsCharged  int
	PromptTokens     int
	CompletionTokens int
}

// MaxRationaleRunes bounds the rationale kept with a decision and its inputs.
const MaxRationaleRunes = 2000

func truncateRunes(s string, n int) string {
	if n < 0 {
		return ""
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

// EncodeProposed is the stored JSON form of the proposed Commands, shared by
// the stores so every implementation round-trips identically.
func EncodeProposed(cmds []sim.Command) ([]byte, error) {
	if cmds == nil {
		cmds = []sim.Command{}
	}
	b, err := json.Marshal(cmds)
	if err != nil {
		return nil, fmt.Errorf("encode proposed commands: %w", err)
	}
	return b, nil
}

// DecodeProposed is the inverse of EncodeProposed.
func DecodeProposed(b []byte) ([]sim.Command, error) {
	var out []sim.Command
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode proposed commands: %w", err)
	}
	return out, nil
}

// EncodeRejected is the stored JSON form of the rejected proposals.
func EncodeRejected(r []RejectedProposal) ([]byte, error) {
	if r == nil {
		r = []RejectedProposal{}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode rejected proposals: %w", err)
	}
	return b, nil
}

// DecodeRejected is the inverse of EncodeRejected.
func DecodeRejected(b []byte) ([]RejectedProposal, error) {
	var out []RejectedProposal
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode rejected proposals: %w", err)
	}
	return out, nil
}

// LLMUsageSum is the LLM usage summed over decision records.
type LLMUsageSum struct {
	Calls            int
	PromptTokens     int
	CompletionTokens int
}
