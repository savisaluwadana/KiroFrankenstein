package sim

import "hash/fnv"

// Seed is the world seed from which every named random stream is derived.
type Seed uint64

// StreamName identifies an independent random stream ("demand", "supplier",
// "workforce", "disruption").
type StreamName string

// StreamState is the complete internal state of a Stream. It is what a
// snapshot stores so that restored streams reproduce the following draws.
type StreamState uint64

// Stream is a project-owned SplitMix64 PRNG derived from (seed, name).
type Stream struct{ state uint64 }

// NewStream returns the stream for name, seeded with seed XOR fnv64a(name).
func NewStream(seed Seed, name StreamName) Stream {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name)) // hash.Hash writes never fail
	return Stream{state: uint64(seed) ^ h.Sum64()}
}

// RestoreStream rebuilds a stream from a saved StreamState.
func RestoreStream(st StreamState) Stream { return Stream{state: uint64(st)} }

// Next advances the stream and returns the next 64-bit value.
func (s *Stream) Next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// IntN returns an unbiased value in [0, n) using rejection sampling.
// It returns 0 when n is 0. The number of underlying draws is usually one
// but is not fixed; exogenous callers must size their draws accordingly.
func (s *Stream) IntN(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	// Reject the low (2^64 mod n) values so the remaining range is a
	// whole multiple of n.
	threshold := (-n) % n
	for {
		r := s.Next()
		if r >= threshold {
			return r % n
		}
	}
}

// State returns the full internal state of the stream.
func (s Stream) State() StreamState { return StreamState(s.state) }

// Position returns the raw state for hashing and draw-count assertions.
func (s Stream) Position() uint64 { return s.state }
