package httpapi_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

func journalFixture(t *testing.T) (http.Handler, string, int64) {
	t.Helper()
	h := newHandler(t, nil)
	d := demo(t, h)
	seed := int64(d["world"].(obj)["seed"].(float64))
	b := branchID(d)
	advanceTo(t, h, b, 30)
	return h, b, seed
}

func TestJournalCountMatchesEngineOutcomes(t *testing.T) {
	h, b, seed := journalFixture(t)

	sc, err := sim.ParseScenario(scenarios.BlackFriday)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sim.Init(*sc, sim.Seed(seed))
	if err != nil {
		t.Fatal(err)
	}
	const from, to = 7, 30
	want := map[string]int{}
	for st.Tick() < to {
		step, err := sim.Advance(*sc, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		st = step.State
		if st.Tick() < from {
			continue
		}
		for _, o := range step.Outcomes {
			switch o.Kind {
			case sim.OutcomeDemand:
				want["demand"]++
			case sim.OutcomeOrderCancelled:
				want["order_cancelled"]++
			case sim.OutcomeShipment:
				want["shipment"]++
			}
		}
	}

	m := mustStatus(t, do(t, h, "GET", "/branches/"+b+"/journal?from=7&to=30&limit=1000", nil), http.StatusOK)
	total := 0
	for _, n := range want {
		total += n
	}
	if want["demand"] == 0 || int(num(t, m, "total")) != total {
		t.Fatalf("journal total %v, engine %v", m["total"], want)
	}
	got := map[string]int{}
	for off := 0; off < total; off += 1000 {
		pg := mustStatus(t, do(t, h, "GET", "/branches/"+b+"/journal?from=7&to=30&limit=1000&offset="+strconv.Itoa(off), nil), http.StatusOK)
		for _, e := range pg["entries"].([]any) {
			got[e.(obj)["kind"].(string)]++
		}
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("kind %s: journal %d, engine %d", k, got[k], n)
		}
	}
	if m["disclaimer"] != sim.Disclaimer || m["currency"] != httpapiCurrency {
		t.Errorf("disclaimer/currency = %v %v", m["disclaimer"], m["currency"])
	}

	// Paging.
	p := mustStatus(t, do(t, h, "GET", "/branches/"+b+"/journal?from=7&to=30&limit=3&offset=2", nil), http.StatusOK)
	if len(p["entries"].([]any)) != 3 || int(num(t, p, "total")) != total {
		t.Errorf("paged = %v", p)
	}
}

const httpapiCurrency = "USD"

func TestJournalRangeBounds(t *testing.T) {
	h, b, _ := journalFixture(t)
	wantProblem(t, do(t, h, "GET", "/branches/"+b+"/journal?from=1&to=26", nil), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "GET", "/branches/"+b+"/journal?from=1", nil), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "GET", "/branches/"+b+"/journal?from=25&to=40", nil), http.StatusConflict, "not-yet-simulated")
}

func TestEndOfHorizonIdentityAndDisclaimer(t *testing.T) {
	h, b, _ := journalFixture(t)
	m := mustStatus(t, do(t, h, "GET", "/branches/"+b+"/end-of-horizon", nil), http.StatusOK)
	st := m["statement"].(obj)
	if num(t, st, "tick") != 30 || st["atHorizon"] != false {
		t.Errorf("tick/atHorizon = %v %v", st["tick"], st["atHorizon"])
	}
	if st["balanceIdentityHolds"] != true || st["inventoryMatch"] != true {
		t.Errorf("identity flags = %v %v", st["balanceIdentityHolds"], st["inventoryMatch"])
	}
	if m["disclaimer"] != sim.Disclaimer || m["currency"] != httpapiCurrency {
		t.Errorf("disclaimer/currency = %v %v", m["disclaimer"], m["currency"])
	}

	// An earlier tick is a rewind read and does not change the head.
	e := mustStatus(t, do(t, h, "GET", "/branches/"+b+"/end-of-horizon?tick=10", nil), http.StatusOK)
	if num(t, e["statement"].(obj), "tick") != 10 || e["statement"].(obj)["balanceIdentityHolds"] != true {
		t.Errorf("tick 10 statement = %v", e["statement"])
	}
	wantProblem(t, do(t, h, "GET", "/branches/"+b+"/end-of-horizon?tick=99", nil), http.StatusConflict, "not-yet-simulated")
}

func TestLedgerEntriesCarryCauseRefs(t *testing.T) {
	h, b, _ := journalFixture(t)
	m := mustStatus(t, do(t, h, "GET", "/branches/"+b+"/ledger?from=0&to=30&limit=1000", nil), http.StatusOK)
	entries := m["entries"].([]any)
	if len(entries) < 2 {
		t.Fatalf("ledger has %d entries", len(entries))
	}
	for _, x := range entries {
		e := x.(obj)
		c, ok := e["cause"].(obj)
		if !ok || c["kind"] == "" || c["kind"] == nil {
			t.Fatalf("entry without cause: %v", e)
		}
		if _, ok := c["ref"].(float64); !ok {
			t.Fatalf("cause without ref: %v", e)
		}
		if d, _ := e["description"].(string); d == "" {
			t.Fatalf("entry without description: %v", e)
		}
	}
	if m["disclaimer"] != sim.Disclaimer || m["currency"] != httpapiCurrency {
		t.Errorf("disclaimer/currency = %v %v", m["disclaimer"], m["currency"])
	}
}
