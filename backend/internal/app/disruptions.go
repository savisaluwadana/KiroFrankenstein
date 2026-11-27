package app

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// InjectRequest injects a Disruption into a Branch's future. StartTick is the
// Tick in which the Disruption takes effect and must be above the Head_Tick.
// Target, Role and MagnitudeBps follow sim.DisruptionInput.
type InjectRequest struct {
	Branch           BranchID
	Kind             uint8
	StartTick        sim.Tick
	DurationTicks    int64
	MagnitudeBps     sim.Bps
	Target           int64
	Role             uint8
	ExpectedHeadTick *sim.Tick
	IdempotencyKey   string
}

// InjectResult is the stored Input. Duplicate is true when IdempotencyKey was
// already used on the Branch; then nothing was appended.
type InjectResult struct {
	Input     InputRecord
	Duplicate bool
}

func injectIdemKey(key string) string { return "inject:" + key }

// InjectDisruption validates the Disruption with sim.ValidateDisruptionInput
// and appends exactly one Input of kind disruption with provenance "injected"
// and EffectiveTick = StartTick. A start Tick at or below the Head_Tick is
// ErrHistoryImmutable. The Disruption is invisible in Observations until its
// onset, because the engine only creates it when the onset Tick is computed.
// Its onset opens a Decision_Window, so Advance pauses there.
func (s *BranchService) InjectDisruption(ctx context.Context, req InjectRequest) (InjectResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.loadMutable(ctx, req.Branch)
	if err != nil {
		return InjectResult{}, err
	}
	head := c.head.state.Tick()
	if err := checkExpected(req.ExpectedHeadTick, head); err != nil {
		return InjectResult{}, err
	}
	if req.IdempotencyKey != "" {
		rec, ok, err := s.store.FindByIdempotencyKey(ctx, req.Branch, injectIdemKey(req.IdempotencyKey))
		if err != nil {
			return InjectResult{}, fmt.Errorf("idempotency lookup: %w", err)
		}
		if ok {
			return InjectResult{Input: rec, Duplicate: true}, nil
		}
	}
	if req.StartTick <= head {
		return InjectResult{}, fmt.Errorf("start tick %d <= head %d: %w", req.StartTick, head, ErrHistoryImmutable)
	}
	in := sim.DisruptionInput{
		Kind: req.Kind, Target: req.Target, Role: req.Role,
		DurationTicks: req.DurationTicks, MagnitudeBps: req.MagnitudeBps,
	}
	if err := sim.ValidateDisruptionInput(&c.sc, in, req.StartTick); err != nil {
		return InjectResult{}, err
	}
	md := Metadata{Provenance: ProvInjectedDisruptionLog}
	if req.IdempotencyKey != "" {
		md.IdempotencyKey = injectIdemKey(req.IdempotencyKey)
	}
	rec, err := s.store.AppendInput(ctx, req.Branch, NewInput{
		EffectiveTick: req.StartTick, Kind: sim.InputDisruption,
		Payload: sim.EncodeDisruptionInput(in), Metadata: md,
	})
	if err != nil {
		return InjectResult{}, fmt.Errorf("append input: %w", err)
	}
	return InjectResult{Input: rec}, nil
}
