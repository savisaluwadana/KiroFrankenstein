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
		case sim.InputDisruption:
			d, err := sim.DecodeDisruptionInput(r.Payload)
			if err != nil {
				return nil, fmt.Errorf("input %d of branch %s: %w", r.Seq, r.BranchID, err)
			}
			in.Disruption = d
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

// replayed is a State at a Tick together with the frames of Ticks base..Tick
// (frames[i] is the frame of tick base+i). A replay that resumed from a
// snapshot has no frames before the snapshot; they are recomputed on demand.
type replayed struct {
	state  sim.State
	base   sim.Tick
	frames []sim.TickFrame
}

// frame returns the cached frame of tick t, if held.
func (r replayed) frame(t sim.Tick) (sim.TickFrame, bool) {
	i := int64(t - r.base)
	if i < 0 || i >= int64(len(r.frames)) {
		return sim.TickFrame{}, false
	}
	return r.frames[i], true
}
