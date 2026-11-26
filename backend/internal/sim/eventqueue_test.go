package sim

import (
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

func TestClockBoundaries(t *testing.T) {
	cases := []struct {
		tick Tick
		want SimMinute
	}{{0, 0}, {1, 60}, {120, 7200}}
	for _, c := range cases {
		if got := c.tick.Boundary(); got != c.want || Boundary(c.tick) != c.want {
			t.Errorf("Boundary(%d)=%d want %d", c.tick, got, c.want)
		}
	}
	for m, want := range map[SimMinute]Tick{0: 1, 59: 1, 60: 2, 7199: 120} {
		if got := TickOf(m); got != want {
			t.Errorf("TickOf(%d)=%d want %d", m, got, want)
		}
	}
	if !SimMinute(120).IsBoundary() || SimMinute(121).IsBoundary() {
		t.Error("IsBoundary wrong")
	}
}

func TestEventQueueEqualKeysPopInInsertionOrder(t *testing.T) {
	var q EventQueue
	for i := int64(0); i < 5; i++ {
		q.Push(Event{At: 10, Priority: 3, Kind: EvFulfilment, Payload: EventPayload{Ref: i}})
	}
	for i := int64(0); i < 5; i++ {
		e, ok := q.Pop()
		if !ok || e.Payload.Ref != i || e.Seq != uint64(i) {
			t.Fatalf("pop %d: %+v ok=%v", i, e, ok)
		}
	}
	if _, ok := q.Pop(); ok {
		t.Fatal("expected empty")
	}
}

func eventGen() *rapid.Generator[Event] {
	return rapid.Custom(func(t *rapid.T) Event {
		return Event{
			At:       SimMinute(rapid.IntRange(0, 20).Draw(t, "at")),
			Priority: uint8(rapid.IntRange(0, 3).Draw(t, "prio")),
			Kind:     EventKind(rapid.IntRange(0, 9).Draw(t, "kind")),
			Payload:  EventPayload{Ref: rapid.Int64Range(0, 1000).Draw(t, "ref")},
		}
	})
}

func TestEventQueuePropOrderedPops(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := rapid.SliceOfN(eventGen(), 0, 60).Draw(t, "evs")
		var q EventQueue
		for _, e := range evs {
			q.Push(e)
		}
		var prev *Event
		n := 0
		for {
			e, ok := q.Pop()
			if !ok {
				break
			}
			n++
			if prev != nil && !eventLess(*prev, e) {
				t.Fatalf("out of order: %+v then %+v", *prev, e)
			}
			p := e
			prev = &p
		}
		if n != len(evs) {
			t.Fatalf("popped %d of %d", n, len(evs))
		}
	})
}

func TestEventQueuePropSnapshotRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := rapid.SliceOfN(eventGen(), 0, 40).Draw(t, "evs")
		pops := rapid.IntRange(0, len(evs)).Draw(t, "pops")
		var q EventQueue
		for _, e := range evs {
			q.Push(e)
		}
		for i := 0; i < pops; i++ {
			q.Pop()
		}
		r, err := RestoreEventQueue(q.Pending(), q.SeqCounter())
		if err != nil {
			t.Fatal(err)
		}
		// Push more to both; they must stay identical.
		extra := Event{At: 5, Priority: 1, Kind: EvOrderCancel}
		q.Push(extra)
		r.Push(extra)
		if !reflect.DeepEqual(q.Pending(), r.Pending()) || q.SeqCounter() != r.SeqCounter() {
			t.Fatal("restored queue diverged")
		}
		for {
			a, aok := q.Pop()
			b, bok := r.Pop()
			if aok != bok || a != b {
				t.Fatalf("pop mismatch %+v vs %+v", a, b)
			}
			if !aok {
				break
			}
		}
	})
}

func TestEventQueueRestoreRejectsBadInput(t *testing.T) {
	if _, err := RestoreEventQueue([]Event{{Seq: 3}}, 3); err == nil {
		t.Error("expected error for seq >= counter")
	}
	if _, err := RestoreEventQueue([]Event{{Seq: 1}, {Seq: 1}}, 5); err == nil {
		t.Error("expected error for duplicate seq")
	}
}
