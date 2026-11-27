package sim

// Score is the Composite_Score of one Branch at one Tick (Req 21.5, 21.10).
// It depends only on that Branch's own Metrics and the Scenario, never on an
// opponent's result. The raw Metrics are not altered and must be reported
// alongside the Score.
type Score struct {
	// Financial is the Financial_Component in bps, 0..10000:
	//   0 when NetProfit <= ProfitFloor, 10000 when NetProfit >= ProfitTarget,
	//   else MulDiv(NetProfit - ProfitFloor, 10000, ProfitTarget - ProfitFloor).
	Financial Bps
	// FulfilmentRate is the Fill_Rate in bps, used directly.
	FulfilmentRate Bps
	// OnTime is the Service_Level in bps, used directly.
	OnTime Bps
	// Composite is the score in hundredths of a point, 0..10000:
	//   RoundHalfEven(wF*Financial + wFill*FulfilmentRate + wOnTime*OnTime, 10000)
	// with the Scenario's ScoreWeights (bps summing to 10000).
	Composite int64
}

// ScoreOutcome is the result of comparing two Scores.
type ScoreOutcome uint8

// Score comparison outcomes, from the point of view of the first argument.
const (
	ScoreTie ScoreOutcome = iota
	ScoreWin
	ScoreLoss
)

// ComputeScore computes the Composite_Score for m under sc. The Scenario must
// have passed validation (ProfitTarget > ProfitFloor, weights sum to 10000);
// if ProfitTarget <= ProfitFloor the Financial_Component is 0 or 10000 by the
// clamp edges and no division occurs.
func ComputeScore(sc Scenario, m Metrics) Score {
	fin := financialComponent(sc.ProfitFloorCents, sc.ProfitTargetCents, m.NetProfit)
	fill := clampBps(m.FillRate)
	on := clampBps(m.ServiceLevel)
	w := sc.ScoreWeights
	sum := int64(w.FinancialWeightBps)*int64(fin) +
		int64(w.FulfilmentWeightBps)*int64(fill) +
		int64(w.OnTimeWeightBps)*int64(on)
	comp := RoundHalfEven(sum, BpsScale)
	if comp < 0 {
		comp = 0
	}
	if comp > BpsScale {
		comp = BpsScale
	}
	return Score{Financial: fin, FulfilmentRate: fill, OnTime: on, Composite: comp}
}

func financialComponent(floor, target, profit Cents) Bps {
	if profit <= floor {
		return 0
	}
	if profit >= target {
		return BpsScale
	}
	return Bps(MulDiv(int64(profit-floor), BpsScale, int64(target-floor)))
}

func clampBps(b Bps) Bps {
	if b < 0 {
		return 0
	}
	if b > BpsScale {
		return BpsScale
	}
	return b
}

// Compare compares a with b by Composite. If |a.Composite - b.Composite| is at
// most toleranceHundredths the result is ScoreTie; otherwise ScoreWin when a
// is higher and ScoreLoss when lower (Req 21.7, 21.8).
func Compare(a, b Score, toleranceHundredths int64) ScoreOutcome {
	d := a.Composite - b.Composite
	switch {
	case d >= -toleranceHundredths && d <= toleranceHundredths:
		return ScoreTie
	case d > 0:
		return ScoreWin
	default:
		return ScoreLoss
	}
}
