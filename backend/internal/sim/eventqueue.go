package sim

import (
	"container/heap"
	"errors"
	"sort"
)

// EventKind identifies an event type. Values are part of the canonical
// encoding; append only.
type EventKind uint8

const (
	EvDemandArrivals   EventKind = iota // generate potential arrivals + convert
	EvFulfilment                        // pick/pack ship
	EvLaborAccrual                      // wages
	EvMarketingAccrual                  // marketing spend accrual
	EvOverheadAccrual                   // overhead accrual
	EvOrderCancel                       // order cancellation
	EvPurchaseReceipt                   // purchase order receipt
	EvCommandApply                      // highest priority at its sim minute
	EvDisruptionOnset                   // disruption activation
	EvDisruptionEnd                     // disruption expiry
)

// PriorityCommandApply is the Priority of EvCommandApply: it runs before every
// other event at the same sim minute (Req 5.7).
const PriorityCommandApply uint8 = 0

// EventPayload is the closed, concrete payload of an event: fixed integer
// fields whose meaning is defined per EventKind. It holds no funcs or
// interfaces, so it can be canonically encoded and snapshotted.
type EventPayload struct {
	Ref int64 // primary reference (order id, PO id, input seq, disruption index)
	SKU int64 // SKU index or id where relevant
	Qty int64 // quantity or amount where relevant
	Aux int64 // kind-specific extra value
}

// Event is one scheduled state change. Ordering key is (At, Priority, Seq).
type Event struct {
	At       SimMinute
	Priority uint8  // lower runs first at equal At
	Seq      uint64 // monotonic insertion sequence, final tiebreak (assigned by Push)
	Kind     EventKind
	Payload  EventPayload
}

func eventLess(a, b Event) bool {
	if a.At != b.At {
		return a.At < b.At
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	return a.Seq < b.Seq
}

type eventHeap []Event

func (h eventHeap) Len() int           { return len(h) }
func (h eventHeap) Less(i, j int) bool { return eventLess(h[i], h[j]) }
func (h eventHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *eventHeap) Push(x any)        { *h = append(*h, x.(Event)) }
func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	*h = old[:n-1]
	return e
}

// EventQueue is a binary-heap priority queue ordered by (At, Priority, Seq).
// The zero value is an empty queue ready to use.
type EventQueue struct {
	h   eventHeap
	seq uint64 // next insertion sequence to assign
}

// ErrBadQueueRestore is returned when restore inputs are inconsistent.
var ErrBadQueueRestore = errors.New("sim: inconsistent event queue restore")

// Push schedules e, assigning it the next insertion sequence (any Seq on e is
// overwritten). It returns the assigned Seq.
func (q *EventQueue) Push(e Event) uint64 {
	e.Seq = q.seq
	q.seq++
	heap.Push(&q.h, e)
	return e.Seq
}

// Pop removes and returns the next event in (At, Priority, Seq) order.
func (q *EventQueue) Pop() (Event, bool) {
	if len(q.h) == 0 {
		return Event{}, false
	}
	return heap.Pop(&q.h).(Event), true
}

// Peek returns the next event without removing it.
func (q *EventQueue) Peek() (Event, bool) {
	if len(q.h) == 0 {
		return Event{}, false
	}
	return q.h[0], true
}

// Len returns the number of pending events.
func (q *EventQueue) Len() int { return len(q.h) }

// SeqCounter returns the next insertion sequence to be assigned.
func (q *EventQueue) SeqCounter() uint64 { return q.seq }

// Pending returns a copy of all pending events in canonical (At, Priority, Seq)
// order, suitable for snapshotting.
func (q *EventQueue) Pending() []Event {
	out := make([]Event, len(q.h))
	copy(out, q.h)
	sort.Slice(out, func(i, j int) bool { return eventLess(out[i], out[j]) })
	return out
}

// RestoreEventQueue rebuilds a queue from pending events and the insertion
// sequence counter. Seqs must be unique and below seqCounter.
func RestoreEventQueue(pending []Event, seqCounter uint64) (*EventQueue, error) {
	seen := make(map[uint64]struct{}, len(pending))
	for _, e := range pending {
		if e.Seq >= seqCounter {
			return nil, ErrBadQueueRestore
		}
		if _, dup := seen[e.Seq]; dup {
			return nil, ErrBadQueueRestore
		}
		seen[e.Seq] = struct{}{}
	}
	q := &EventQueue{h: make(eventHeap, len(pending)), seq: seqCounter}
	copy(q.h, pending)
	heap.Init(&q.h)
	return q, nil
}
