package app

import (
	"fmt"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// buildEngineInputs is the single place that turns stored InputRecords into
// sim.Inputs. records must be in InputsForBranch order (effective tick, then
// ancestor-first, then Seq). Seq is renumbered 1..n in that order, so the
// engine-visible Seq does not depend on which branch row an input lives in
// (Req 13.5). Commands are decoded from the canonical payload.
func buildEngineInputs(records []InputRecord) ([]sim.Input, error) {
	out := make([]sim.Input, 0, len(records))
	for i, r := range records {
		in := sim.Input{EffectiveTick: r.EffectiveTick, Seq: uint64(i) + 1, Kind: r.Kind}
		switch r.Kind {
		case sim.InputCommand:
			c, err := sim.DecodeCommand(r.Payload)
			if err != nil {
				return nil, fmt.Errorf("input %d of branch %s: %w", r.Seq, r.BranchID, err)
			}
			in.Command = c
		default:
			return nil, fmt.Errorf("input %d of branch %s kind %d: %w", r.Seq, r.BranchID, r.Kind, ErrUnsupportedInput)
		}
		out = append(out, in)
	}
	return out, nil
}

// inputsAt returns the inputs that take effect at tick, in order.
func inputsAt(all []sim.Input, tick sim.Tick) []sim.Input {
	var out []sim.Input
	for _, in := range all {
		if in.EffectiveTick == tick {
			out = append(out, in)
		}
	}
	return out
}

// replayed is a State at a Tick together with the frames of Ticks 0..Tick.
type replayed struct {
	state  sim.State
	frames []sim.TickFrame
}

// replay rebuilds a branch's state and frames from Init up to tick using the
// already renumbered inputs. It is the cache-miss path and the past-read path.
func replay(sc sim.Scenario, seed sim.Seed, inputs []sim.Input, tick sim.Tick) (replayed, error) {
	st, err := sim.Init(sc, seed)
	if err != nil {
		return replayed{}, fmt.Errorf("init: %w", err)
	}
	f0, _, err := sim.InitialFrame(sc, st)
	if err != nil {
		return replayed{}, fmt.Errorf("initial frame: %w", err)
	}
	r := replayed{state: st, frames: []sim.TickFrame{f0}}
	for st.Tick() < tick {
		step, err := sim.Advance(sc, st, inputsAt(inputs, st.Tick()+1))
		if err != nil {
			return replayed{}, fmt.Errorf("replay to tick %d: %w", st.Tick()+1, err)
		}
		st = step.State
		r.state = st
		r.frames = append(r.frames, step.Frame)
	}
	return r, nil
}
