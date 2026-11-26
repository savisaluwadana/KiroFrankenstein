package sim

import (
	"bytes"
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

func genMinute() *rapid.Generator[SimMinute] {
	return rapid.Custom(func(t *rapid.T) SimMinute {
		return SimMinute(rapid.Int64Range(-100, 10000).Draw(t, "minute"))
	})
}

func genCents() *rapid.Generator[Cents] {
	return rapid.Custom(func(t *rapid.T) Cents {
		return Cents(rapid.Int64().Draw(t, "cents"))
	})
}

func genLines() *rapid.Generator[[]LedgerLine] {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) LedgerLine {
		return LedgerLine{
			Account: AccountID(rapid.Uint8().Draw(t, "account")),
			Debit:   genCents().Draw(t, "debit"),
			Credit:  genCents().Draw(t, "credit"),
		}
	}), 0, 4)
}

// genState draws a State whose sortable collections have unique sort keys, in
// arbitrary (non-canonical) order, so Encode's sorting is a total order.
func genState() *rapid.Generator[State] {
	return rapid.Custom(func(t *rapid.T) State {
		var s State
		s.Clock = SimMinute(rapid.Int64Range(0, 100000).Draw(t, "clock"))

		s.Inventory = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) SKUInventory {
			return SKUInventory{
				SKU:          rapid.Int64Range(0, 50).Draw(t, "sku"),
				OnHand:       rapid.Int64().Draw(t, "onhand"),
				Value:        genCents().Draw(t, "value"),
				OpeningUnits: rapid.Int64().Draw(t, "opening"),
				Received:     rapid.Int64().Draw(t, "received"),
				Shipped:      rapid.Int64().Draw(t, "shipped"),
				WrittenOff:   rapid.Int64().Draw(t, "writtenoff"),
			}
		}), 0, 6, func(v SKUInventory) int64 { return v.SKU }).Draw(t, "inventory")

		s.Orders.Open = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) Order {
			return Order{
				ID:         rapid.Uint64Range(0, 200).Draw(t, "oid"),
				SKU:        rapid.Int64Range(0, 50).Draw(t, "osku"),
				Qty:        rapid.Int64().Draw(t, "qty"),
				UnitPrice:  genCents().Draw(t, "price"),
				CreatedAt:  genMinute().Draw(t, "created"),
				PromisedBy: genMinute().Draw(t, "promised"),
			}
		}), 0, 8, func(o Order) uint64 { return o.ID }).Draw(t, "orders")
		s.Orders.Settled = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) SettledOrder {
			return SettledOrder{
				ID:         rapid.Uint64Range(0, 200).Draw(t, "sid"),
				Qty:        rapid.Int64().Draw(t, "sqty"),
				PromisedBy: genMinute().Draw(t, "spromised"),
				ShippedAt:  genMinute().Draw(t, "sshipped"),
			}
		}), 0, 8, func(o SettledOrder) uint64 { return o.ID }).Draw(t, "settled")
		s.Orders.Created = rapid.Int64().Draw(t, "created")
		s.Orders.Shipped = rapid.Int64().Draw(t, "shipped")
		s.Orders.ShippedLate = rapid.Int64().Draw(t, "shippedLate")
		s.Orders.Cancelled = rapid.Int64().Draw(t, "cancelled")
		s.Orders.UnitsCreated = rapid.Int64().Draw(t, "uc")
		s.Orders.UnitsShipped = rapid.Int64().Draw(t, "us")
		s.Orders.UnitsCancelled = rapid.Int64().Draw(t, "ucan")
		s.OrderSeq = rapid.Uint64().Draw(t, "orderSeq")

		s.PurchaseOs = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) PurchaseOrder {
			return PurchaseOrder{
				ID:        rapid.Uint64Range(0, 200).Draw(t, "poid"),
				SKU:       rapid.Int64Range(0, 50).Draw(t, "posku"),
				Supplier:  rapid.Uint8Range(0, 1).Draw(t, "supplier"),
				Qty:       rapid.Int64().Draw(t, "poqty"),
				UnitCost:  genCents().Draw(t, "unitcost"),
				PlacedAt:  genMinute().Draw(t, "placed"),
				DueAt:     genMinute().Draw(t, "due"),
				Expedited: rapid.Uint8Range(0, 1).Draw(t, "exp"),
			}
		}), 0, 6, func(p PurchaseOrder) uint64 { return p.ID }).Draw(t, "pos")
		s.POSeq = rapid.Uint64().Draw(t, "poSeq")

		s.Workforce.Regular = rapid.Int64().Draw(t, "regular")
		s.Workforce.OvertimeHours = rapid.Int64().Draw(t, "ot")
		// Temp cohorts may repeat; they are totally ordered by all fields.
		s.Workforce.Temps = rapid.SliceOfN(rapid.Custom(func(t *rapid.T) TempCohort {
			return TempCohort{
				Count:   rapid.Int64Range(0, 5).Draw(t, "count"),
				HiredAt: genMinute().Draw(t, "hired"),
				ReadyAt: genMinute().Draw(t, "ready"),
			}
		}), 0, 5).Draw(t, "temps")

		s.Ledger.OpeningCash = genCents().Draw(t, "cash")
		s.Ledger.OpeningEquity = genCents().Draw(t, "equity")
		s.Ledger.Entries = rapid.SliceOfN(rapid.Custom(func(t *rapid.T) LedgerEntry {
			return LedgerEntry{
				Tick:     Tick(rapid.Int64Range(0, 200).Draw(t, "etick")),
				At:       genMinute().Draw(t, "eat"),
				Cause:    rapid.Uint8().Draw(t, "cause"),
				CauseRef: rapid.Int64().Draw(t, "ref"),
				Lines:    genLines().Draw(t, "lines"),
			}
		}), 0, 6).Draw(t, "entries")

		s.Disruptions = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) ActiveDisruption {
			return ActiveDisruption{
				Kind:         rapid.Uint8().Draw(t, "kind"),
				Origin:       rapid.Uint8Range(0, 1).Draw(t, "origin"),
				InputSeq:     rapid.Uint64Range(0, 100).Draw(t, "iseq"),
				Onset:        genMinute().Draw(t, "onset"),
				End:          genMinute().Draw(t, "end"),
				MagnitudeBps: Bps(rapid.Int64().Draw(t, "mag")),
				Target:       rapid.Int64Range(-1, 50).Draw(t, "target"),
			}
		}), 0, 6, func(d ActiveDisruption) string {
			return fmt.Sprintf("%d/%d/%d", d.Onset, d.Origin, d.InputSeq)
		}).Draw(t, "disruptions")

		s.Prices = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) SKUPrice {
			return SKUPrice{
				SKU:            rapid.Int64Range(0, 50).Draw(t, "psku"),
				PriceCents:     genCents().Draw(t, "pc"),
				MarketingCents: genCents().Draw(t, "mc"),
			}
		}), 0, 6, func(p SKUPrice) int64 { return p.SKU }).Draw(t, "prices")

		s.Streams = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) NamedStream {
			return NamedStream{
				Name:  StreamName(rapid.StringN(0, 12, -1).Draw(t, "sname")),
				State: StreamState(rapid.Uint64().Draw(t, "sstate")),
			}
		}), 0, 5, func(n NamedStream) string { return string(n.Name) }).Draw(t, "streams")

		s.Pending = rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) Event {
			return Event{
				At:       genMinute().Draw(t, "eat"),
				Priority: rapid.Uint8().Draw(t, "prio"),
				Seq:      rapid.Uint64Range(0, 500).Draw(t, "seq"),
				Kind:     EventKind(rapid.Uint8Range(0, uint8(EvDisruptionEnd)).Draw(t, "ekind")),
				Payload: EventPayload{
					Ref: rapid.Int64().Draw(t, "ref"),
					SKU: rapid.Int64().Draw(t, "esku"),
					Qty: rapid.Int64().Draw(t, "eqty"),
					Aux: rapid.Int64().Draw(t, "aux"),
				},
			}
		}), 0, 8, func(e Event) string {
			return fmt.Sprintf("%d/%d/%d", e.At, e.Priority, e.Seq)
		}).Draw(t, "pending")
		s.EventSeq = rapid.Uint64().Draw(t, "eventSeq")
		return s
	})
}

// permuted returns a copy of s with every reorderable slice shuffled. Ledger
// entries keep posting order and are not shuffled.
func permuted(t *rapid.T, s State) State {
	p := s
	p.Inventory = rapid.Permutation(s.Inventory).Draw(t, "pInv")
	p.Orders.Open = rapid.Permutation(s.Orders.Open).Draw(t, "pOrders")
	p.PurchaseOs = rapid.Permutation(s.PurchaseOs).Draw(t, "pPO")
	p.Workforce.Temps = rapid.Permutation(s.Workforce.Temps).Draw(t, "pTemps")
	p.Disruptions = rapid.Permutation(s.Disruptions).Draw(t, "pDis")
	p.Prices = rapid.Permutation(s.Prices).Draw(t, "pPrices")
	p.Streams = rapid.Permutation(s.Streams).Draw(t, "pStreams")
	p.Pending = rapid.Permutation(s.Pending).Draw(t, "pPending")
	return p
}

// Feature: parallel-simulation-mvp, Property 4: State canonical encoding round trip
//
// **Validates: Requirements 12.1, 14.6, 28.6**
func TestPropStateEncodingRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genState().Draw(t, "state")
		enc := Encode(s)

		dec, err := Decode(enc)
		if err != nil {
			t.Fatalf("Decode(Encode(s)) failed: %v", err)
		}
		if re := Encode(dec); !bytes.Equal(re, enc) {
			t.Fatalf("re-encoding differs from original encoding")
		}
		if Hash(dec) != Hash(s) {
			t.Fatalf("hash differs after round trip")
		}

		// Collections Encode sorts yield the same encoding in any input order.
		p := permuted(t, s)
		if penc := Encode(p); !bytes.Equal(penc, enc) {
			t.Fatalf("permuted slices changed the encoding")
		}
		if Hash(p) != Hash(s) {
			t.Fatalf("permuted slices changed the hash")
		}
	})
}
