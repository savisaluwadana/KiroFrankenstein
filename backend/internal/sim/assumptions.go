package sim

import "sort"

// EffectiveAssumption is one numeric Assumption of a Scenario with its
// declared metadata and effective value (Req 1.7, 26.2). Overridden is true
// when Value differs from Default.
type EffectiveAssumption struct {
	Path       string
	Value      int64
	Unit       string
	Default    int64
	Min        int64
	Max        int64
	Rationale  string
	Overridden bool
}

// EffectiveAssumptions lists every scalar numeric Assumption of s in ascending
// path order, with its metadata from s.AssumptionMeta. An Assumption without
// metadata is skipped (a validated Scenario has none). The demand.hourlyProfile
// vector is not a scalar and is not listed. It does not modify s.
func EffectiveAssumptions(s *Scenario) []EffectiveAssumption {
	refs := assumptionRefs(s)
	out := make([]EffectiveAssumption, 0, len(refs))
	for _, r := range refs {
		m, ok := s.AssumptionMeta[r.path]
		if !ok {
			continue
		}
		v := r.get()
		out = append(out, EffectiveAssumption{
			Path: r.path, Value: v, Unit: m.Unit, Default: m.Default,
			Min: m.Min, Max: m.Max, Rationale: m.Rationale, Overridden: v != m.Default,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
