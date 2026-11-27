package sim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sort"
)

// ErrBadEncoding is returned by Decode for truncated, oversized, trailing or
// non-canonical input.
var ErrBadEncoding = errors.New("sim: invalid state encoding")

// Canonical encoding: fields in struct declaration order; every integer is a
// fixed-width big-endian value (int64/uint64 = 8 bytes, uint8 = 1 byte);
// every slice is a 4-byte element count followed by its elements; a stream
// name is a 4-byte length followed by its bytes. Collections are written in
// the canonical order named on State. There is no version, magic, identifier,
// label or timestamp.

type encoder struct{ b []byte }

func (e *encoder) u8(v uint8)   { e.b = append(e.b, v) }
func (e *encoder) u64(v uint64) { e.b = binary.BigEndian.AppendUint64(e.b, v) }
func (e *encoder) i64(v int64)  { e.u64(uint64(v)) }
func (e *encoder) n(v int)      { e.b = binary.BigEndian.AppendUint32(e.b, uint32(v)) }

// Encode returns the canonical encoding of s. It does not modify s.
func Encode(s State) []byte {
	e := &encoder{}
	e.i64(int64(s.Clock))

	inv := append([]SKUInventory(nil), s.Inventory...)
	sort.SliceStable(inv, func(i, j int) bool { return inv[i].SKU < inv[j].SKU })
	e.n(len(inv))
	for _, v := range inv {
		e.i64(v.SKU)
		e.i64(v.OnHand)
		e.i64(int64(v.Value))
		e.i64(v.OpeningUnits)
		e.i64(v.Received)
		e.i64(v.Shipped)
		e.i64(v.WrittenOff)
	}

	open := append([]Order(nil), s.Orders.Open...)
	sort.SliceStable(open, func(i, j int) bool { return open[i].ID < open[j].ID })
	e.n(len(open))
	for _, o := range open {
		e.u64(o.ID)
		e.i64(o.SKU)
		e.i64(o.Qty)
		e.i64(int64(o.UnitPrice))
		e.i64(int64(o.CreatedAt))
		e.i64(int64(o.PromisedBy))
	}
	settled := append([]SettledOrder(nil), s.Orders.Settled...)
	sort.SliceStable(settled, func(i, j int) bool { return settled[i].ID < settled[j].ID })
	e.n(len(settled))
	for _, o := range settled {
		e.u64(o.ID)
		e.i64(o.Qty)
		e.i64(int64(o.PromisedBy))
		e.i64(int64(o.ShippedAt))
	}
	e.i64(s.Orders.Created)
	e.i64(s.Orders.Shipped)
	e.i64(s.Orders.ShippedLate)
	e.i64(s.Orders.Cancelled)
	e.i64(s.Orders.UnitsCreated)
	e.i64(s.Orders.UnitsShipped)
	e.i64(s.Orders.UnitsCancelled)
	e.u64(s.OrderSeq)

	pos := append([]PurchaseOrder(nil), s.PurchaseOs...)
	sort.SliceStable(pos, func(i, j int) bool { return pos[i].ID < pos[j].ID })
	e.n(len(pos))
	for _, p := range pos {
		e.u64(p.ID)
		e.i64(p.SKU)
		e.u8(p.Supplier)
		e.i64(p.Qty)
		e.i64(int64(p.UnitCost))
		e.i64(int64(p.PlacedAt))
		e.i64(int64(p.DueAt))
		e.u8(p.Expedited)
	}
	e.u64(s.POSeq)

	e.i64(s.Workforce.Regular)
	e.i64(s.Workforce.OvertimeHours)
	temps := append([]TempCohort(nil), s.Workforce.Temps...)
	sort.SliceStable(temps, func(i, j int) bool {
		a, b := temps[i], temps[j]
		if a.ReadyAt != b.ReadyAt {
			return a.ReadyAt < b.ReadyAt
		}
		if a.HiredAt != b.HiredAt {
			return a.HiredAt < b.HiredAt
		}
		return a.Count < b.Count
	})
	e.n(len(temps))
	for _, t := range temps {
		e.i64(t.Count)
		e.i64(int64(t.HiredAt))
		e.i64(int64(t.ReadyAt))
	}

	// Ledger entries keep posting order; they are never reordered.
	e.i64(int64(s.Ledger.OpeningCash))
	e.i64(int64(s.Ledger.OpeningEquity))
	e.n(len(s.Ledger.Entries))
	for _, le := range s.Ledger.Entries {
		e.i64(int64(le.Tick))
		e.i64(int64(le.At))
		e.u8(le.Cause)
		e.i64(le.CauseRef)
		e.n(len(le.Lines))
		for _, l := range le.Lines {
			e.u8(uint8(l.Account))
			e.i64(int64(l.Debit))
			e.i64(int64(l.Credit))
		}
	}

	ds := append([]ActiveDisruption(nil), s.Disruptions...)
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Onset != b.Onset {
			return a.Onset < b.Onset
		}
		if a.Origin != b.Origin {
			return a.Origin < b.Origin
		}
		return a.InputSeq < b.InputSeq
	})
	e.n(len(ds))
	for _, d := range ds {
		e.u8(d.Kind)
		e.u8(d.Origin)
		e.u64(d.InputSeq)
		e.i64(int64(d.Onset))
		e.i64(int64(d.End))
		e.i64(int64(d.MagnitudeBps))
		e.i64(d.Target)
		e.u8(d.Role)
	}

	prices := append([]SKUPrice(nil), s.Prices...)
	sort.SliceStable(prices, func(i, j int) bool { return prices[i].SKU < prices[j].SKU })
	e.n(len(prices))
	for _, p := range prices {
		e.i64(p.SKU)
		e.i64(int64(p.PriceCents))
		e.i64(int64(p.MarketingCents))
	}

	streams := append([]NamedStream(nil), s.Streams...)
	sort.SliceStable(streams, func(i, j int) bool { return streams[i].Name < streams[j].Name })
	e.n(len(streams))
	for _, st := range streams {
		e.n(len(st.Name))
		e.b = append(e.b, st.Name...)
		e.u64(uint64(st.State))
	}

	pend := append([]Event(nil), s.Pending...)
	sort.SliceStable(pend, func(i, j int) bool { return eventLess(pend[i], pend[j]) })
	e.n(len(pend))
	for _, ev := range pend {
		e.i64(int64(ev.At))
		e.u8(ev.Priority)
		e.u64(ev.Seq)
		e.u8(uint8(ev.Kind))
		e.i64(ev.Payload.Ref)
		e.i64(ev.Payload.SKU)
		e.i64(ev.Payload.Qty)
		e.i64(ev.Payload.Aux)
	}
	e.u64(s.EventSeq)
	return e.b
}

type decoder struct {
	b   []byte
	err error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n < 0 || n > len(d.b) {
		d.err = ErrBadEncoding
		return nil
	}
	out := d.b[:n]
	d.b = d.b[n:]
	return out
}

func (d *decoder) u8() uint8 {
	if b := d.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (d *decoder) u64() uint64 {
	if b := d.take(8); b != nil {
		return binary.BigEndian.Uint64(b)
	}
	return 0
}

func (d *decoder) i64() int64 { return int64(d.u64()) }

// count reads an element count and rejects counts that cannot fit in the
// remaining bytes, given the minimum encoded size of one element.
func (d *decoder) count(minElem int) int {
	b := d.take(4)
	if b == nil {
		return 0
	}
	n := int(binary.BigEndian.Uint32(b))
	if n > len(d.b)/minElem {
		d.err = ErrBadEncoding
		return 0
	}
	return n
}

// Decode is the inverse of Encode. It rejects truncated input, trailing
// bytes and any input that is not byte-identical to the canonical encoding of
// the decoded State. Empty collections decode as nil.
func Decode(b []byte) (State, error) {
	d := &decoder{b: b}
	var s State
	s.Clock = SimMinute(d.i64())

	if n := d.count(56); n > 0 {
		s.Inventory = make([]SKUInventory, n)
		for i := range s.Inventory {
			v := &s.Inventory[i]
			v.SKU = d.i64()
			v.OnHand = d.i64()
			v.Value = Cents(d.i64())
			v.OpeningUnits = d.i64()
			v.Received = d.i64()
			v.Shipped = d.i64()
			v.WrittenOff = d.i64()
		}
	}

	if n := d.count(48); n > 0 {
		s.Orders.Open = make([]Order, n)
		for i := range s.Orders.Open {
			o := &s.Orders.Open[i]
			o.ID = d.u64()
			o.SKU = d.i64()
			o.Qty = d.i64()
			o.UnitPrice = Cents(d.i64())
			o.CreatedAt = SimMinute(d.i64())
			o.PromisedBy = SimMinute(d.i64())
		}
	}
	if n := d.count(32); n > 0 {
		s.Orders.Settled = make([]SettledOrder, n)
		for i := range s.Orders.Settled {
			o := &s.Orders.Settled[i]
			o.ID = d.u64()
			o.Qty = d.i64()
			o.PromisedBy = SimMinute(d.i64())
			o.ShippedAt = SimMinute(d.i64())
		}
	}
	s.Orders.Created = d.i64()
	s.Orders.Shipped = d.i64()
	s.Orders.ShippedLate = d.i64()
	s.Orders.Cancelled = d.i64()
	s.Orders.UnitsCreated = d.i64()
	s.Orders.UnitsShipped = d.i64()
	s.Orders.UnitsCancelled = d.i64()
	s.OrderSeq = d.u64()

	if n := d.count(50); n > 0 {
		s.PurchaseOs = make([]PurchaseOrder, n)
		for i := range s.PurchaseOs {
			p := &s.PurchaseOs[i]
			p.ID = d.u64()
			p.SKU = d.i64()
			p.Supplier = d.u8()
			p.Qty = d.i64()
			p.UnitCost = Cents(d.i64())
			p.PlacedAt = SimMinute(d.i64())
			p.DueAt = SimMinute(d.i64())
			p.Expedited = d.u8()
		}
	}
	s.POSeq = d.u64()

	s.Workforce.Regular = d.i64()
	s.Workforce.OvertimeHours = d.i64()
	if n := d.count(24); n > 0 {
		s.Workforce.Temps = make([]TempCohort, n)
		for i := range s.Workforce.Temps {
			t := &s.Workforce.Temps[i]
			t.Count = d.i64()
			t.HiredAt = SimMinute(d.i64())
			t.ReadyAt = SimMinute(d.i64())
		}
	}

	s.Ledger.OpeningCash = Cents(d.i64())
	s.Ledger.OpeningEquity = Cents(d.i64())
	if n := d.count(29); n > 0 {
		s.Ledger.Entries = make([]LedgerEntry, n)
		for i := range s.Ledger.Entries {
			le := &s.Ledger.Entries[i]
			le.Tick = Tick(d.i64())
			le.At = SimMinute(d.i64())
			le.Cause = d.u8()
			le.CauseRef = d.i64()
			if m := d.count(17); m > 0 {
				le.Lines = make([]LedgerLine, m)
				for j := range le.Lines {
					l := &le.Lines[j]
					l.Account = AccountID(d.u8())
					l.Debit = Cents(d.i64())
					l.Credit = Cents(d.i64())
				}
			}
		}
	}

	if n := d.count(43); n > 0 {
		s.Disruptions = make([]ActiveDisruption, n)
		for i := range s.Disruptions {
			a := &s.Disruptions[i]
			a.Kind = d.u8()
			a.Origin = d.u8()
			a.InputSeq = d.u64()
			a.Onset = SimMinute(d.i64())
			a.End = SimMinute(d.i64())
			a.MagnitudeBps = Bps(d.i64())
			a.Target = d.i64()
			a.Role = d.u8()
		}
	}

	if n := d.count(24); n > 0 {
		s.Prices = make([]SKUPrice, n)
		for i := range s.Prices {
			p := &s.Prices[i]
			p.SKU = d.i64()
			p.PriceCents = Cents(d.i64())
			p.MarketingCents = Cents(d.i64())
		}
	}

	if n := d.count(12); n > 0 {
		s.Streams = make([]NamedStream, n)
		for i := range s.Streams {
			l := d.count(1)
			name := d.take(l)
			s.Streams[i].Name = StreamName(name)
			s.Streams[i].State = StreamState(d.u64())
		}
	}

	if n := d.count(50); n > 0 {
		s.Pending = make([]Event, n)
		for i := range s.Pending {
			ev := &s.Pending[i]
			ev.At = SimMinute(d.i64())
			ev.Priority = d.u8()
			ev.Seq = d.u64()
			ev.Kind = EventKind(d.u8())
			ev.Payload.Ref = d.i64()
			ev.Payload.SKU = d.i64()
			ev.Payload.Qty = d.i64()
			ev.Payload.Aux = d.i64()
		}
	}
	s.EventSeq = d.u64()

	if d.err != nil || len(d.b) != 0 {
		return State{}, ErrBadEncoding
	}
	if !bytes.Equal(Encode(s), b) {
		return State{}, ErrBadEncoding
	}
	return s, nil
}
