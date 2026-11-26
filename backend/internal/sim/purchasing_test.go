package sim

import (
	"errors"
	"reflect"
	"testing"
)

func TestPurchasingPlaceAndReceive(t *testing.T) {
	sc := fulfilScenario() // 50 units at 400, primary lead 12 ticks, unit cost 400
	st := mustInit(t, sc, 1)
	q, err := st.Queue()
	if err != nil {
		t.Fatal(err)
	}
	po, err := PlacePurchaseOrder(&sc, &st, q, 0, SupplierPrimary, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	st.SetQueue(q)
	if po.ID != 1 || st.POSeq != 2 || po.UnitCost != 400 || po.DueAt != 12*60 || po.PlacedAt != 0 {
		t.Fatalf("po = %+v seq %d", po, st.POSeq)
	}
	if len(st.PurchaseOs) != 1 {
		t.Fatalf("open POs = %d", len(st.PurchaseOs))
	}

	for i := 0; i < 12; i++ {
		st = stepNoErr(t, sc, st)
	}
	if len(st.PurchaseOs) != 1 || st.Inventory[0].OnHand != 50 {
		t.Fatalf("receipt happened early: %+v %+v", st.PurchaseOs, st.Inventory[0])
	}
	before := len(st.Ledger.Entries)
	cashBefore := st.Ledger.Balance(AccountCash)
	st = stepNoErr(t, sc, st) // tick 13 contains minute 720

	var rec *LedgerEntry
	for i := before; i < len(st.Ledger.Entries); i++ {
		if st.Ledger.Entries[i].Cause == CauseReceipt {
			rec = &st.Ledger.Entries[i]
		}
	}
	if rec == nil {
		t.Fatal("no receipt entry")
	}
	want := []LedgerLine{{AccountInventory, 4000, 0}, {AccountCash, 0, 4000}}
	if !reflect.DeepEqual(rec.Lines, want) || rec.CauseRef != 1 || rec.At != 720 {
		t.Errorf("receipt entry = %+v", *rec)
	}
	inv := st.Inventory[0]
	if inv.OnHand != 60 || inv.Received != 10 || inv.Value != 24000 || inv.WrittenOff != 0 {
		t.Errorf("inventory = %+v", inv)
	}
	if len(st.PurchaseOs) != 0 {
		t.Errorf("PO not removed: %+v", st.PurchaseOs)
	}
	if got := st.Ledger.Balance(AccountInventory); got != inv.Value {
		t.Errorf("inventory balance %d != value %d", got, inv.Value)
	}
	_ = cashBefore
	d, c := st.Ledger.Totals()
	if d != c {
		t.Errorf("unbalanced ledger %d vs %d", d, c)
	}
}

func TestPurchasingRejectsBadRequests(t *testing.T) {
	sc := fulfilScenario()
	st := mustInit(t, sc, 1)
	q, _ := st.Queue()
	if _, err := PlacePurchaseOrder(&sc, &st, q, 0, SupplierBackup, 5, 0); !errors.Is(err, ErrUnknownSupplier) {
		t.Errorf("backup err = %v", err)
	}
	if _, err := PlacePurchaseOrder(&sc, &st, q, 0, SupplierPrimary, 0, 0); !errors.Is(err, ErrBadPOQuantity) {
		t.Errorf("qty err = %v", err)
	}
	if st.POSeq != 1 || len(st.PurchaseOs) != 0 {
		t.Errorf("state changed on error: %+v", st)
	}
}

func stepNoErr(t *testing.T, sc Scenario, st State) State {
	t.Helper()
	next, _, _ := stepOne(t, sc, st)
	return next
}

func TestAccrualsWagesAreSeparateLinesPerTick(t *testing.T) {
	sc := fulfilScenario() // 10 workers, wages 2000/3000/2500, overhead 100
	st := mustInit(t, sc, 1)
	st.Workforce.OvertimeHours = 1
	st.Workforce.Temps = []TempCohort{{Count: 2, HiredAt: 0, ReadyAt: 600}}
	st.Prices[0].MarketingCents = 50
	before := len(st.Ledger.Entries)

	st = stepNoErr(t, sc, st)
	st = stepNoErr(t, sc, st)

	byTick := map[Tick]map[uint8]LedgerEntry{}
	for _, e := range st.Ledger.Entries[before:] {
		if byTick[e.Tick] == nil {
			byTick[e.Tick] = map[uint8]LedgerEntry{}
		}
		byTick[e.Tick][e.Cause] = e
		if err := e.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for tick := Tick(1); tick <= 2; tick++ {
		labor, ok := byTick[tick][CauseLabor]
		if !ok {
			t.Fatalf("tick %d has no labor entry", tick)
		}
		wantLabor := []LedgerLine{
			{AccountLaborExpense, 20000, 0}, // regular 10*2000
			{AccountLaborExpense, 30000, 0}, // overtime 10*1*3000
			{AccountLaborExpense, 5000, 0},  // temp 2*2500, still onboarding
			{AccountCash, 0, 55000},
		}
		if !reflect.DeepEqual(labor.Lines, wantLabor) || labor.CauseRef != int64(tick) || labor.At != SimMinute((int64(tick)-1)*60+58) {
			t.Errorf("tick %d labor = %+v", tick, labor)
		}
		if m := byTick[tick][CauseMarketing]; !reflect.DeepEqual(m.Lines, []LedgerLine{{AccountMarketingExpense, 50, 0}, {AccountCash, 0, 50}}) {
			t.Errorf("tick %d marketing = %+v", tick, m)
		}
		if o := byTick[tick][CauseOverhead]; !reflect.DeepEqual(o.Lines, []LedgerLine{{AccountOverheadExpense, 100, 0}, {AccountCash, 0, 100}}) {
			t.Errorf("tick %d overhead = %+v", tick, o)
		}
	}
}

func TestAccrualsZeroOvertimeAndTempOmitted(t *testing.T) {
	sc := fulfilScenario()
	st := mustInit(t, sc, 1)
	before := len(st.Ledger.Entries)
	st = stepNoErr(t, sc, st)
	for _, e := range st.Ledger.Entries[before:] {
		if e.Cause == CauseLabor {
			want := []LedgerLine{{AccountLaborExpense, 20000, 0}, {AccountCash, 0, 20000}}
			if !reflect.DeepEqual(e.Lines, want) {
				t.Errorf("labor lines = %+v", e.Lines)
			}
		}
		if e.Cause == CauseMarketing {
			t.Errorf("zero marketing posted an entry: %+v", e)
		}
	}
}
