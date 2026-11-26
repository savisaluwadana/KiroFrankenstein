package sim

import (
	"errors"
	"reflect"
	"testing"
)

func TestBoundaryWindowsFlagship(t *testing.T) {
	sc := loadFlagship(t)
	var got []Tick
	for tick := Tick(0); tick <= Tick(sc.HorizonTicks)+2; tick++ {
		open, reasons := DecisionWindowOpen(sc, tick)
		if open != (len(reasons) > 0) {
			t.Fatalf("tick %d: open=%v reasons=%v", tick, open, reasons)
		}
		if open {
			got = append(got, tick)
		}
	}
	var want []Tick
	for tick := Tick(0); tick < 120; tick++ {
		if tick%4 == 0 || tick == 48 || tick == 54 || tick == 60 {
			want = append(want, tick)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("windows = %v, want %v", got, want)
	}
	if len(got) != 31 { // 30 cadence windows (0..116) plus the Tick 54 onset
		t.Errorf("window count = %d, want 31", len(got))
	}
	if _, r := DecisionWindowOpen(sc, 54); !reflect.DeepEqual(r, []WindowReason{WindowDisruptionOnset}) {
		t.Errorf("tick 54 reasons = %v", r)
	}
	if _, r := DecisionWindowOpen(sc, 60); !reflect.DeepEqual(r, []WindowReason{WindowCadence, WindowDisruptionOnset}) {
		t.Errorf("tick 60 reasons = %v", r)
	}
	if open, _ := DecisionWindowOpen(sc, 120); open {
		t.Error("window opened at the horizon")
	}
}

func TestBoundaryAdvance(t *testing.T) {
	sc := loadFlagship(t)
	st, err := Init(sc, 2026)
	if err != nil {
		t.Fatal(err)
	}
	f0, open0, err := InitialFrame(sc, st)
	if err != nil || !open0 || f0.Tick != 0 || f0.Hash != Hash(st) {
		t.Fatalf("initial frame: %+v open=%v err=%v", f0, open0, err)
	}
	for i := 1; i <= 64; i++ {
		step, err := Advance(sc, st, nil)
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		n := Tick(i)
		if step.State.Tick() != n || step.Frame.Tick != n || step.Frame.Hash != Hash(step.State) {
			t.Fatalf("tick %d: frame/state mismatch", i)
		}
		// The State is fixed at the boundary: no pending event is due before it.
		for _, ev := range step.State.Pending {
			if ev.At < n.Boundary() {
				t.Fatalf("tick %d: event at minute %d ran late", i, ev.At)
			}
		}
		want, _ := DecisionWindowOpen(sc, n)
		if step.Window != want {
			t.Fatalf("tick %d: window = %v, want %v", i, step.Window, want)
		}
		if int64(len(step.Frame.SKUs)) != int64(len(sc.SKUs)) {
			t.Fatalf("tick %d: %d sku frames", i, len(step.Frame.SKUs))
		}
		st = step.State
	}
	// The supplier outage (script onset Tick 60) is visible with 0 ticks since onset.
	found := false
	f := mustFrame(t, sc, st, FulfilmentConstraintReport{})
	for _, d := range f.Disruptions {
		if d.Kind == DisruptionSupplierOutage && d.TicksSinceOnset == 4 {
			found = true
		}
	}
	if !found {
		t.Errorf("tick 64 frame lacks the supplier outage: %+v", f.Disruptions)
	}
}

func mustFrame(t *testing.T, sc Scenario, st State, r FulfilmentConstraintReport) TickFrame {
	t.Helper()
	f, err := BuildTickFrame(sc, st, r)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBoundaryFrameDeterminism(t *testing.T) {
	sc := loadFlagship(t)
	run := func() []TickFrame {
		st, err := Init(sc, 7)
		if err != nil {
			t.Fatal(err)
		}
		var frames []TickFrame
		for i := 0; i < 62; i++ {
			step, err := Advance(sc, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			frames = append(frames, step.Frame)
			st = step.State
		}
		return frames
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("frames differ between identical runs")
	}
}

func TestBoundaryAdvanceDoesNotModifyInput(t *testing.T) {
	sc := loadFlagship(t)
	st, _ := Init(sc, 1)
	before := Hash(st)
	if _, err := Advance(sc, st, nil); err != nil {
		t.Fatal(err)
	}
	if Hash(st) != before {
		t.Fatal("Advance modified its input State")
	}
}

func TestBoundaryBadClock(t *testing.T) {
	sc := loadFlagship(t)
	st, _ := Init(sc, 1)
	st.Clock = 17
	if _, err := BuildTickFrame(sc, st, FulfilmentConstraintReport{}); !errors.Is(err, ErrBadClock) {
		t.Fatalf("err = %v, want ErrBadClock", err)
	}
}

func TestHorizonReached(t *testing.T) {
	sc := loadFlagship(t)
	sc.HorizonTicks = 24
	sc.DisruptionScript = nil
	st, err := Init(sc, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 24; i++ {
		step, err := Advance(sc, st, nil)
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		st = step.State
	}
	if st.Tick() != 24 {
		t.Fatalf("tick = %d", st.Tick())
	}
	if _, err := Advance(sc, st, nil); !errors.Is(err, ErrHorizonReached) {
		t.Fatalf("err = %v, want ErrHorizonReached", err)
	}
}

func TestHorizonFlagshipStopsAt120(t *testing.T) {
	sc := loadFlagship(t)
	st, err := Init(sc, 2026)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 120; i++ {
		step, err := Advance(sc, st, nil)
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		st = step.State
	}
	if _, err := Advance(sc, st, nil); !errors.Is(err, ErrHorizonReached) {
		t.Fatalf("err = %v, want ErrHorizonReached", err)
	}
}
