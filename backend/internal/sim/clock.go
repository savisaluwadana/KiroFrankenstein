package sim

// MinutesPerTick is the number of simulated minutes in one Tick (Req 5.1).
const MinutesPerTick = 60

// SimMinute is an integer count of simulated minutes since the Scenario start.
type SimMinute int64

// Tick is one simulated hour. Tick 0 is the initial State; Tick N (N >= 1)
// covers sim minutes [(N-1)*60, N*60).
type Tick int64

// Boundary returns the Tick_Boundary of tick t: the sim minute t*60 at which
// the State "at Tick t" is defined (Req 5.4).
func (t Tick) Boundary() SimMinute { return SimMinute(int64(t) * MinutesPerTick) }

// Boundary is a function form of Tick.Boundary.
func Boundary(t Tick) SimMinute { return t.Boundary() }

// TickOf returns the Tick whose interval contains sim minute m (m >= 0):
// minute 0..59 belongs to Tick 1.
func TickOf(m SimMinute) Tick { return Tick(int64(m)/MinutesPerTick + 1) }

// IsBoundary reports whether m is a Tick_Boundary.
func (m SimMinute) IsBoundary() bool { return m%MinutesPerTick == 0 }
