package app

import (
	"context"
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// MaxJournalTicks is the longest tick range one journal read may span.
const MaxJournalTicks = 24

// JournalEntry is one outcome of the engine's outcome journal together with the
// Tick whose step produced it.
type JournalEntry struct {
	Tick    sim.Tick
	Outcome sim.Outcome
}

// JournalRange returns the outcome journal for Ticks from through to
// inclusive. The journal is derived output and is not stored in State, so it
// is rebuilt by replaying the branch's input prefix from Init and collecting
// the Outcomes of each step. Entries are in engine order.
func (s *BranchService) JournalRange(ctx context.Context, id BranchID, from, to sim.Tick) ([]JournalEntry, error) {
	if from < 0 || to < from {
		return nil, fmt.Errorf("range %d..%d: %w", from, to, ErrInvalidTick)
	}
	if to-from+1 > MaxJournalTicks {
		return nil, fmt.Errorf("range %d..%d spans more than %d ticks: %w", from, to, MaxJournalTicks, ErrInvalidTick)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.checkTick(to); err != nil {
		return nil, err
	}
	inputs, err := s.engineInputs(ctx, c.b.ID, to)
	if err != nil {
		return nil, err
	}
	st, err := sim.Init(c.sc, c.w.Seed)
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	var out []JournalEntry
	for st.Tick() < to {
		step, err := sim.Advance(c.sc, st, inputsAt(inputs, st.Tick()+1))
		if err != nil {
			return nil, fmt.Errorf("journal replay to tick %d: %w", st.Tick()+1, err)
		}
		st = step.State
		if t := st.Tick(); t >= from {
			for _, o := range step.Outcomes {
				out = append(out, JournalEntry{Tick: t, Outcome: o})
			}
		}
	}
	return out, nil
}

// EndOfHorizonAt returns the End_Of_Horizon_Statement computed over the State
// at tick. AtHorizon reports whether tick is the Scenario horizon.
func (s *BranchService) EndOfHorizonAt(ctx context.Context, id BranchID, t sim.Tick) (sim.EndOfHorizon, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load(ctx, id)
	if err != nil {
		return sim.EndOfHorizon{}, err
	}
	st, err := s.stateAt(ctx, c, t)
	if err != nil {
		return sim.EndOfHorizon{}, err
	}
	return sim.EndOfHorizonStatement(c.sc, st), nil
}
