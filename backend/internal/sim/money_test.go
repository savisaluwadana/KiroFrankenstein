package sim

import (
	"math"
	"testing"
)

func TestRoundHalfEven(t *testing.T) {
	tests := []struct {
		num, den, want int64
	}{
		{5, 2, 2}, {7, 2, 4}, {3, 2, 2}, {1, 2, 0},
		{-5, 2, -2}, {-7, 2, -4}, {-3, 2, -2}, {-1, 2, 0},
		{25, 10, 2}, {35, 10, 4}, {-25, 10, -2}, {-35, 10, -4},
		{26, 10, 3}, {24, 10, 2}, {-26, 10, -3}, {-24, 10, -2},
		{0, 7, 0}, {10, 5, 2}, {-10, 5, -2},
		{math.MaxInt64, 1, math.MaxInt64},
		{math.MinInt64, 1, math.MinInt64},
		{math.MaxInt64, 2, 1 << 62}, // (2^63-1)/2 = 2^62 - 0.5 -> tie, 2^62-1 odd -> 2^62
	}
	for _, tc := range tests {
		if got := RoundHalfEven(tc.num, tc.den); got != tc.want {
			t.Errorf("RoundHalfEven(%d,%d)=%d want %d", tc.num, tc.den, got, tc.want)
		}
	}
}

func TestRoundHalfEvenPanicsOnNonPositiveDen(t *testing.T) {
	for _, d := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("den %d: expected panic", d)
				}
			}()
			RoundHalfEven(1, d)
		}()
	}
}

func TestMulDiv(t *testing.T) {
	tests := []struct {
		a, b, c, want int64
	}{
		{10, 10, 4, 25},
		{1, 1, 2, 0}, {3, 1, 2, 2}, {5, 1, 2, 2},
		{-5, 1, 2, -2}, {5, -1, 2, -2}, {-5, -1, 2, 2}, {5, 1, -2, -2},
		{-7, 1, 2, -4},
		// Product exceeds int64: 10^12 * 10^12 / 10^12.
		{1_000_000_000_000, 1_000_000_000_000, 1_000_000_000_000, 1_000_000_000_000},
		// Large product with basis points.
		{math.MaxInt64, 10000, 10000, math.MaxInt64},
		{math.MaxInt64, 5000, 10000, 1 << 62}, // tie at 2^62-0.5 -> even 2^62
		{math.MinInt64, 1, 1, math.MinInt64},
		{math.MinInt64, -1, -1, math.MinInt64},
		{0, math.MaxInt64, 3, 0},
		{1 << 40, 1 << 40, 1 << 20, 1 << 60},
	}
	for _, tc := range tests {
		if got := MulDiv(tc.a, tc.b, tc.c); got != tc.want {
			t.Errorf("MulDiv(%d,%d,%d)=%d want %d", tc.a, tc.b, tc.c, got, tc.want)
		}
	}
}

func TestMulDivPanics(t *testing.T) {
	cases := [][3]int64{
		{1, 1, 0},
		{math.MaxInt64, 2, 1},
		{math.MinInt64, -1, 1},
		{math.MaxInt64, math.MaxInt64, 1},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("MulDiv(%v): expected panic", c)
				}
			}()
			MulDiv(c[0], c[1], c[2])
		}()
	}
}

func TestMulDivMatchesRoundHalfEvenSmall(t *testing.T) {
	for a := int64(-30); a <= 30; a++ {
		for b := int64(-6); b <= 6; b++ {
			for c := int64(1); c <= 8; c++ {
				if got, want := MulDiv(a, b, c), RoundHalfEven(a*b, c); got != want {
					t.Fatalf("a=%d b=%d c=%d: %d != %d", a, b, c, got, want)
				}
			}
		}
	}
}
