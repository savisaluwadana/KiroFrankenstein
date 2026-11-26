package sim

import "math/bits"

// Cents is a signed amount in minor units.
type Cents int64

// Bps is a rate in basis points (1% == 100 bps).
type Bps int64

// BpsScale is the denominator for basis-point rates (100% == 10000 bps).
const BpsScale = 10000

// RoundHalfEven is the only integer-division rounding helper. It returns
// num/den rounded half to even. It panics if den <= 0.
func RoundHalfEven(num, den int64) int64 {
	if den <= 0 {
		panic("sim: RoundHalfEven requires den > 0")
	}
	return MulDiv(num, 1, den)
}

// MulDiv computes a*b/c exactly using a 128-bit intermediate, rounded half to
// even. c must be non-zero. It panics if the rounded result does not fit in
// an int64.
func MulDiv(a, b, c int64) int64 {
	if c == 0 {
		panic("sim: MulDiv division by zero")
	}
	neg := false
	ua, n := absU64(a)
	neg = neg != n
	ub, n := absU64(b)
	neg = neg != n
	uc, n := absU64(c)
	neg = neg != n

	hi, lo := bits.Mul64(ua, ub)
	if hi >= uc {
		panic("sim: MulDiv result overflows int64")
	}
	q, r := bits.Div64(hi, lo, uc)
	// r < uc <= 2^63, so 2r cannot overflow uint64.
	twice := r * 2
	if twice > uc || (twice == uc && q&1 == 1) {
		q++
		if q == 0 {
			panic("sim: MulDiv result overflows int64")
		}
	}
	if neg {
		if q > 1<<63 {
			panic("sim: MulDiv result overflows int64")
		}
		return int64(-q)
	}
	if q > 1<<63-1 {
		panic("sim: MulDiv result overflows int64")
	}
	return int64(q)
}

// absU64 returns |x| as uint64 and whether x was negative.
func absU64(x int64) (uint64, bool) {
	if x < 0 {
		return -uint64(x), true
	}
	return uint64(x), false
}
