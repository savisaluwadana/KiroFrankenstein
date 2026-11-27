package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// allFigureEntries pages through every entry of a figure up to tick.
func allFigureEntries(t *testing.T, h http.Handler, b, figure string, tick int64, page int) (entries []obj, sum, total int64) {
	t.Helper()
	total = -1
	for off := int64(0); total < 0 || off < total; off += int64(page) {
		m := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/ledger?from=0&to=%d&figure=%s&limit=%d&offset=%d", b, tick, figure, page, off), nil), http.StatusOK)
		total = num(t, m, "total")
		sum = num(t, m, "sumCents")
		if m["figure"] != figure || m["disclaimer"] != sim.Disclaimer || m["currency"] != httpapiCurrency {
			t.Fatalf("figure/disclaimer/currency = %v %v %v", m["figure"], m["disclaimer"], m["currency"])
		}
		for _, x := range m["entries"].([]any) {
			entries = append(entries, x.(obj))
		}
	}
	return entries, sum, total
}

func TestLedgerSummaryFiguresSumToMetrics(t *testing.T) {
	h, root, _ := journalFixture(t)
	child := mustStatus(t, do(t, h, "POST", "/branches/"+root+"/fork", obj{"atTick": 12, "label": "Fork"}), http.StatusCreated)["id"].(string)
	advanceTo(t, h, child, 20)

	metricKey := map[string]string{
		"cash": "cashCents", "net_profit": "netProfitCents", "net_revenue": "netRevenueCents",
		"cost_of_goods_sold": "costOfGoodsSoldCents", "freight": "freightExpenseCents", "labor": "laborExpenseCents",
		"marketing": "marketingExpenseCents", "overhead": "overheadExpenseCents", "service_credits": "serviceCreditExpenseCents",
	}
	for _, c := range []struct {
		branch string
		ticks  []int64
	}{{root, []int64{0, 5, 17, 30}}, {child, []int64{12, 16, 20}}} {
		for _, tick := range c.ticks {
			sum := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/ledger/summary?tick=%d", c.branch, tick), nil), http.StatusOK)
			if sum["disclaimer"] != sim.Disclaimer || sum["currency"] != httpapiCurrency {
				t.Fatalf("disclaimer/currency = %v %v", sum["disclaimer"], sum["currency"])
			}
			met := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/metrics?tick=%d", c.branch, tick), nil), http.StatusOK)["metrics"].(obj)
			figs := sum["figures"].([]any)
			if len(figs) != len(metricKey) {
				t.Fatalf("figures = %d", len(figs))
			}
			for _, x := range figs {
				f := x.(obj)
				name := f["figure"].(string)
				want := num(t, met, metricKey[name])
				if got := num(t, f, "sumCents"); got != want {
					t.Errorf("%s tick %d: summary %d, metrics %d", name, tick, got, want)
				}
				entries, listSum, total := allFigureEntries(t, h, c.branch, name, int64(tick), 7)
				var lines int64
				for _, e := range entries {
					lines += num(t, e, "contributionCents")
				}
				if lines != want || listSum != want || total != num(t, f, "entryCount") || int64(len(entries)) != total {
					t.Errorf("%s tick %d: entries sum %d, list sum %d, metrics %d, entries %d, total %d, count %v",
						name, tick, lines, listSum, want, len(entries), total, f["entryCount"])
				}
			}
		}
	}

	// Per-account totals: the balance is on the normal side and the chart balances.
	sum := mustStatus(t, do(t, h, "GET", "/branches/"+root+"/ledger/summary?tick=30", nil), http.StatusOK)
	var dr, cr int64
	for _, x := range sum["accounts"].([]any) {
		a := x.(obj)
		d, c := num(t, a, "debitCents"), num(t, a, "creditCents")
		dr += d
		cr += c
		want := d - c
		if a["normalSide"] == "credit" {
			want = c - d
		}
		if num(t, a, "balanceCents") != want {
			t.Errorf("%v balance %v, want %d", a["account"], a["balanceCents"], want)
		}
	}
	if dr != cr || len(sum["accounts"].([]any)) != sim.NumAccounts {
		t.Errorf("debits %d credits %d accounts %d", dr, cr, len(sum["accounts"].([]any)))
	}
}

func TestLedgerFiltersAndStablePaging(t *testing.T) {
	h, b, _ := journalFixture(t)
	base := "/branches/" + b + "/ledger?"

	// Tick range filter.
	m := mustStatus(t, do(t, h, "GET", base+"from=10&to=12&limit=1000", nil), http.StatusOK)
	for _, x := range m["entries"].([]any) {
		if tk := num(t, x.(obj), "tick"); tk < 10 || tk > 12 {
			t.Errorf("entry tick %d outside 10..12", tk)
		}
	}
	// Account filter.
	m = mustStatus(t, do(t, h, "GET", base+"from=0&to=30&account=Labor_Expense&limit=1000", nil), http.StatusOK)
	if len(m["entries"].([]any)) == 0 {
		t.Fatal("no labor entries")
	}
	for _, x := range m["entries"].([]any) {
		found := false
		for _, l := range x.(obj)["lines"].([]any) {
			found = found || l.(obj)["account"] == "Labor_Expense"
		}
		if !found {
			t.Errorf("entry without a labor line: %v", x)
		}
	}
	// Paging is stable: pages of 3 concatenate to the single full read.
	full := mustStatus(t, do(t, h, "GET", base+"from=0&to=8&limit=1000", nil), http.StatusOK)["entries"].([]any)
	var paged []any
	for off := 0; off < len(full); off += 3 {
		paged = append(paged, mustStatus(t, do(t, h, "GET", fmt.Sprintf("%sfrom=0&to=8&limit=3&offset=%d", base, off), nil), http.StatusOK)["entries"].([]any)...)
	}
	if len(paged) != len(full) {
		t.Fatalf("paged %d, full %d", len(paged), len(full))
	}
	for i := range full {
		if num(t, paged[i].(obj), "index") != num(t, full[i].(obj), "index") {
			t.Fatalf("page order differs at %d", i)
		}
	}
	// Bad inputs.
	wantProblem(t, do(t, h, "GET", base+"from=0&to=30&figure=bogus", nil), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "GET", base+"from=0&to=30&figure=cash&account=Cash", nil), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "GET", "/branches/"+b+"/ledger/summary", nil), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "GET", "/branches/"+b+"/ledger/summary?tick=99", nil), http.StatusConflict, "not-yet-simulated")
}
