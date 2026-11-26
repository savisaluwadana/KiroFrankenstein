package sim

import "testing"

// Reference vectors: SplitMix64 (Vigna, https://prng.di.unimi.it/splitmix64.c)
// with state initialised directly to seed 1234567. The five outputs equal the
// published vectors in the Rust rand_xoshiro SplitMix64 tests, and were
// recomputed independently with a Python implementation of the C reference.
// Seed 0 and the named-stream vector were computed with the same Python code;
// fnv64a("demand") = 4011733542463085790.
func TestStreamReferenceVectors(t *testing.T) {
	tests := []struct {
		name  string
		s     Stream
		wants []uint64
	}{
		{"seed 1234567", Stream{state: 1234567}, []uint64{
			6457827717110365317, 3203168211198807973, 9817491932198370423,
			4593380528125082431, 16408922859458223821}},
		{"seed 0", Stream{state: 0}, []uint64{
			16294208416658607535, 7960286522194355700, 487617019471545679}},
		{"NewStream(2026, demand)", NewStream(2026, "demand"), []uint64{
			13018537151380030465, 2068689411165676081}},
	}
	for _, tc := range tests {
		s := tc.s
		for i, want := range tc.wants {
			if got := s.Next(); got != want {
				t.Errorf("%s draw %d: got %d, want %d", tc.name, i, got, want)
			}
		}
	}
}

func TestStreamNamesDiverge(t *testing.T) {
	a, b := NewStream(7, "demand"), NewStream(7, "supplier")
	if a.Next() == b.Next() {
		t.Fatal("different names produced the same first draw")
	}
}

func TestStreamRestoreReproduces(t *testing.T) {
	s := NewStream(99, "workforce")
	for i := 0; i < 17; i++ {
		s.Next()
	}
	r := RestoreStream(s.State())
	for i := 0; i < 50; i++ {
		if x, y := s.IntN(1000), r.IntN(1000); x != y {
			t.Fatalf("draw %d diverged: %d vs %d", i, x, y)
		}
	}
	if s.State() != r.State() {
		t.Fatal("states differ after identical draws")
	}
}

func TestStreamIntNRange(t *testing.T) {
	s := NewStream(1, "demand")
	if s.IntN(0) != 0 {
		t.Fatal("IntN(0) should be 0")
	}
	for _, n := range []uint64{1, 2, 3, 7, 1 << 63, 1<<63 + 1, ^uint64(0)} {
		for i := 0; i < 200; i++ {
			if v := s.IntN(n); v >= n {
				t.Fatalf("IntN(%d) = %d out of range", n, v)
			}
		}
	}
}

func TestStreamIntNUnbiasedSmall(t *testing.T) {
	s := NewStream(5, "disruption")
	var counts [6]int
	const draws = 60000
	for i := 0; i < draws; i++ {
		counts[s.IntN(6)]++
	}
	for k, c := range counts {
		if c < 9000 || c > 11000 {
			t.Errorf("bucket %d count %d outside [9000,11000]", k, c)
		}
	}
}
