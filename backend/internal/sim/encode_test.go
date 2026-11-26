package sim

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixedState() State {
	return State{
		Clock: Tick(5).Boundary(),
		Inventory: []SKUInventory{
			{SKU: 0, OnHand: 100, Value: 250000, OpeningUnits: 120, Received: 30, Shipped: 50, WrittenOff: 0},
			{SKU: 1, OnHand: 0, Value: 0, OpeningUnits: 10, Received: 0, Shipped: 10},
		},
		Orders: OrderBook{
			Open: []Order{{ID: 7, SKU: 0, Qty: 2, UnitPrice: 4999, CreatedAt: 280, PromisedBy: 1720}},
			Settled: []SettledOrder{
				{ID: 1, Qty: 1, PromisedBy: 100, ShippedAt: 90},
				{ID: 2, Qty: 2, PromisedBy: 120, ShippedAt: NotShipped},
			},
			Created:      8,
			Shipped:      6,
			ShippedLate:  1,
			Cancelled:    1,
			UnitsCreated: 15, UnitsShipped: 11, UnitsCancelled: 2,
		},
		OrderSeq:   8,
		PurchaseOs: []PurchaseOrder{{ID: 1, SKU: 0, Supplier: 1, Qty: 40, UnitCost: 2500, PlacedAt: 240, DueAt: 1680, Expedited: 1}},
		POSeq:      2,
		Workforce: Workforce{
			Regular: 12, OvertimeHours: 2,
			Temps: []TempCohort{{Count: 4, HiredAt: 240, ReadyAt: 1680}},
		},
		Ledger: Ledger{
			OpeningCash: 1000000, OpeningEquity: 1250000,
			Entries: []LedgerEntry{{Tick: 0, At: 0, Cause: 1, CauseRef: 0, Lines: []LedgerLine{
				{Account: 1, Debit: 1000000},
				{Account: 2, Debit: 250000},
				{Account: 3, Credit: 1250000},
			}}},
		},
		Disruptions: []ActiveDisruption{{Kind: 2, Origin: 0, InputSeq: 0, Onset: 1440, End: 2880, MagnitudeBps: 5000, Target: -1}},
		Prices:      []SKUPrice{{SKU: 0, PriceCents: 4999, MarketingCents: 100}, {SKU: 1, PriceCents: 1999}},
		Streams: []NamedStream{
			{Name: "demand", State: 11}, {Name: "supplier", State: 22}, {Name: "workforce", State: 33},
		},
		Pending: []Event{
			{At: 301, Priority: 1, Seq: 4, Kind: EvDemandArrivals},
			{At: 360, Priority: 2, Seq: 5, Kind: EvPurchaseReceipt, Payload: EventPayload{Ref: 1, SKU: 0, Qty: 40}},
		},
		EventSeq: 6,
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	for name, s := range map[string]State{"fixed": fixedState(), "zero": {}} {
		got, err := Decode(Encode(s))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, s) {
			t.Fatalf("%s: round trip mismatch\n got %+v\nwant %+v", name, got, s)
		}
	}
}

func TestEncodeCanonicalOrderAndNoMutation(t *testing.T) {
	a := fixedState()
	b := fixedState()
	b.Inventory[0], b.Inventory[1] = b.Inventory[1], b.Inventory[0]
	b.Streams[0], b.Streams[2] = b.Streams[2], b.Streams[0]
	b.Pending[0], b.Pending[1] = b.Pending[1], b.Pending[0]
	before := append([]SKUInventory(nil), b.Inventory...)
	if !bytes.Equal(Encode(a), Encode(b)) {
		t.Fatal("encoding depends on input slice order")
	}
	if !reflect.DeepEqual(before, b.Inventory) {
		t.Fatal("Encode mutated its input")
	}
}

func TestEncodeDecodeRejectsBadInput(t *testing.T) {
	enc := Encode(fixedState())
	cases := map[string][]byte{
		"empty":     {},
		"truncated": enc[:len(enc)-1],
		"trailing":  append(append([]byte(nil), enc...), 0),
		"huge count": func() []byte {
			c := append([]byte(nil), enc...)
			c[8], c[9], c[10], c[11] = 0xff, 0xff, 0xff, 0xff
			return c
		}(),
	}
	for name, in := range cases {
		if _, err := Decode(in); !errors.Is(err, ErrBadEncoding) {
			t.Errorf("%s: err = %v, want ErrBadEncoding", name, err)
		}
	}
}

func TestEncodeContainsNoMetadata(t *testing.T) {
	// The only text in the encoding is stream names.
	enc := string(Encode(fixedState()))
	for _, bad := range []string{"engine", "version", "world", "branch", "provenance", "idempotency", "rationale", "model", "label"} {
		if strings.Contains(enc, bad) {
			t.Errorf("encoding contains %q", bad)
		}
	}
}

func TestEncodeQueueRoundTrip(t *testing.T) {
	var q EventQueue
	q.Push(Event{At: 10, Priority: 3, Kind: EvFulfilment})
	q.Push(Event{At: 5, Priority: 0, Kind: EvCommandApply})
	var s State
	s.SetQueue(&q)
	got, err := Decode(Encode(s))
	if err != nil {
		t.Fatal(err)
	}
	q2, err := got.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if q2.SeqCounter() != 2 || !reflect.DeepEqual(q2.Pending(), q.Pending()) {
		t.Fatal("queue did not survive State encoding")
	}
}

func TestEncodeGolden(t *testing.T) {
	const path = "testdata/state_golden.hex"
	got := hex.EncodeToString(Encode(fixedState())) + "\n"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden encoding mismatch\n got %s\nwant %s", got, want)
	}
}

func TestHashGoldenAndSensitivity(t *testing.T) {
	s := fixedState()
	h := Hash(s)
	if h != Hash(fixedState()) {
		t.Fatal("hash not deterministic")
	}
	got := hex.EncodeToString(h[:]) + "\n"
	const path = "testdata/state_golden.sha256"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(raw) {
		t.Fatalf("golden hash mismatch: got %s want %s", got, raw)
	}
	s.Streams[0].State++
	if Hash(s) == h {
		t.Fatal("hash ignores stream state")
	}
	s = fixedState()
	s.EventSeq++
	if Hash(s) == h {
		t.Fatal("hash ignores event sequence counter")
	}
}
