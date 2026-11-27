package sim

import "testing"

func scoreScenario() Scenario {
	return Scenario{
		ProfitFloorCents:  -1000,
		ProfitTargetCents: 9000,
		ScoreWeights:      ScoreWeights{FinancialWeightBps: 5000, FulfilmentWeightBps: 3000, OnTimeWeightBps: 2000},
	}
}

func TestScoreClampEdges(t *testing.T) {
	sc := scoreScenario()
	for _, c := range []struct {
		name   string
		profit Cents
		fin    Bps
		comp   int64
	}{
		{"below floor", -5000, 0, 0},
		{"at floor", -1000, 0, 0},
		{"midpoint", 4000, 5000, 2500},
		{"at target", 9000, 10000, 5000},
		{"above target", 123456, 10000, 5000},
	} {
		s := ComputeScore(sc, Metrics{NetProfit: c.profit})
		if s.Financial != c.fin || s.Composite != c.comp {
			t.Errorf("%s: got fin=%d comp=%d, want %d %d", c.name, s.Financial, s.Composite, c.fin, c.comp)
		}
	}
}

func TestScoreWeightedRounding(t *testing.T) {
	sc := scoreScenario()
	// fin: (1234+1000)*10000/10000 = 2234. fill 9001, on 8003.
	// sum = 5000*2234 + 3000*9001 + 2000*8003 = 11170000+27003000+16006000 = 54179000
	// /10000 = 5417.9 -> 5418.
	s := ComputeScore(sc, Metrics{NetProfit: 1234, FillRate: 9001, ServiceLevel: 8003})
	if s.Financial != 2234 || s.FulfilmentRate != 9001 || s.OnTime != 8003 || s.Composite != 5418 {
		t.Errorf("got %+v", s)
	}
	// Half-even: sum = 25000 -> 2.5 -> 2; sum = 35000 -> 3.5 -> 4.
	sc.ScoreWeights = ScoreWeights{FinancialWeightBps: 0, FulfilmentWeightBps: 5000, OnTimeWeightBps: 5000}
	if got := ComputeScore(sc, Metrics{FillRate: 5, ServiceLevel: 0}).Composite; got != 2 {
		t.Errorf("2.5 rounded to %d, want 2", got)
	}
	if got := ComputeScore(sc, Metrics{FillRate: 7, ServiceLevel: 0}).Composite; got != 4 {
		t.Errorf("3.5 rounded to %d, want 4", got)
	}
}

func TestScoreFinancialHalfEven(t *testing.T) {
	sc := scoreScenario()
	sc.ProfitFloorCents, sc.ProfitTargetCents = 0, 20000
	// 1*10000/20000 = 0.5 -> 0; 3*10000/20000 = 1.5 -> 2.
	if got := ComputeScore(sc, Metrics{NetProfit: 1}).Financial; got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	if got := ComputeScore(sc, Metrics{NetProfit: 3}).Financial; got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}

func TestScoreCompareTolerance(t *testing.T) {
	a, b := Score{Composite: 5010}, Score{Composite: 5000}
	if got := Compare(a, b, 10); got != ScoreTie {
		t.Errorf("diff 10: %v, want tie", got)
	}
	if got := Compare(b, a, 10); got != ScoreTie {
		t.Errorf("diff -10: %v, want tie", got)
	}
	a.Composite = 5011
	if got := Compare(a, b, 10); got != ScoreWin {
		t.Errorf("diff 11: %v, want win", got)
	}
	if got := Compare(b, a, 10); got != ScoreLoss {
		t.Errorf("diff -11: %v, want loss", got)
	}
}
