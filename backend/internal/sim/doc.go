// Package sim is the pure, deterministic simulation core. It imports only an
// allowlisted subset of the Go standard library: no I/O, no wall clock, no
// global RNG, no goroutines, no floating point in state transitions.
package sim
