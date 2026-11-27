package httpapi_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

const webOrigin = "http://localhost:3000"

func newHandler(t *testing.T, mod func(*httpapi.Options)) http.Handler {
	t.Helper()
	o := httpapi.Options{
		Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday,
		WebOrigin: webOrigin, RatePerSecond: 100000, RateBurst: 100000,
	}
	if mod != nil {
		mod(&o)
	}
	s, err := httpapi.New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s.Handler()
}

func do(t *testing.T, h http.Handler, method, path string, body any, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		rd = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, "/api/v1"+path, rd)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type obj = map[string]any

func decode(t *testing.T, rec *httptest.ResponseRecorder) obj {
	t.Helper()
	var m obj
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return m
}

func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, slug string) obj {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type = %q, want application/problem+json", ct)
	}
	p := decode(t, rec)
	if p["type"] != "urn:parallel:problem:"+slug {
		t.Fatalf("problem type = %v, want %s; body %s", p["type"], slug, rec.Body.String())
	}
	if int(p["status"].(float64)) != status {
		t.Fatalf("problem status field = %v, want %d", p["status"], status)
	}
	if strings.Contains(rec.Body.String(), "goroutine") || strings.Contains(rec.Body.String(), ".go:") {
		t.Fatalf("problem body looks like a stack trace: %s", rec.Body.String())
	}
	return p
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) obj {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, want, rec.Body.String())
	}
	if want == http.StatusNoContent {
		return nil
	}
	return decode(t, rec)
}

func num(t *testing.T, m obj, key string) int64 {
	t.Helper()
	f, ok := m[key].(float64)
	if !ok {
		t.Fatalf("field %q missing or not a number in %v", key, m)
	}
	return int64(f)
}

// demo creates the demo World and returns the whole response.
func demo(t *testing.T, h http.Handler) obj {
	t.Helper()
	return mustStatus(t, do(t, h, "POST", "/demo", nil), http.StatusCreated)
}

func branchID(m obj) string { return m["rootBranch"].(obj)["id"].(string) }

// advanceTo runs the branch to tick with runToTick, repeating when an onset
// window stops it early.
func advanceTo(t *testing.T, h http.Handler, branch string, tick int64) obj {
	t.Helper()
	var last obj
	for i := 0; i < 200; i++ {
		last = mustStatus(t, do(t, h, "POST", "/branches/"+branch+"/advance", obj{"runToTick": tick}), http.StatusOK)
		if num(t, last, "headTick") >= tick {
			return last
		}
	}
	t.Fatalf("branch did not reach tick %d", tick)
	return nil
}

func TestHealth(t *testing.T) {
	h := newHandler(t, nil)
	rec := do(t, h, "GET", "/health", nil)
	m := mustStatus(t, rec, http.StatusOK)
	if m["status"] != "ok" || m["engineVersion"] != strconv.Itoa(sim.EngineVersion) {
		t.Fatalf("health = %v", m)
	}
	for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer"} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp == "" || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
}

func TestStrictDecoding(t *testing.T) {
	h := newHandler(t, nil)
	t.Run("unknown field is 400", func(t *testing.T) {
		rec := do(t, h, "POST", "/worlds", `{"name":"w","seed":1,"bogus":true}`)
		p := wantProblem(t, rec, http.StatusBadRequest, "bad-request")
		if !strings.Contains(fmt.Sprint(p["detail"]), "bogus") {
			t.Errorf("detail should name the unknown field: %v", p["detail"])
		}
	})
	t.Run("unknown nested command field is 400", func(t *testing.T) {
		m := demo(t, h)
		rec := do(t, h, "POST", "/branches/"+branchID(m)+"/commands",
			`{"windowTick":0,"commands":[{"kind":"set_price","sku":0,"amountCents":100,"extra":1}]}`)
		wantProblem(t, rec, http.StatusBadRequest, "bad-request")
	})
	t.Run("trailing data is 400", func(t *testing.T) {
		wantProblem(t, do(t, h, "POST", "/worlds", `{"name":"w","seed":1} {}`), http.StatusBadRequest, "bad-request")
	})
	t.Run("257 KiB body is 413", func(t *testing.T) {
		body := `{"name":"` + strings.Repeat("a", 257*1024) + `","seed":1}`
		wantProblem(t, do(t, h, "POST", "/worlds", body), http.StatusRequestEntityTooLarge, "payload-too-large")
	})
	t.Run("257 KiB body of unknown length is 413", func(t *testing.T) {
		body := `{"name":"` + strings.Repeat("a", 257*1024) + `","seed":1}`
		req := httptest.NewRequest("POST", "/api/v1/worlds", io.NopCloser(strings.NewReader(body)))
		req.ContentLength = -1
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		wantProblem(t, rec, http.StatusRequestEntityTooLarge, "payload-too-large")
	})
	t.Run("seed above 2^53-1 is 400", func(t *testing.T) {
		wantProblem(t, do(t, h, "POST", "/worlds", `{"name":"w","seed":9007199254740992}`), http.StatusBadRequest, "bad-request")
	})
	t.Run("missing seed is 400", func(t *testing.T) {
		wantProblem(t, do(t, h, "POST", "/worlds", `{"name":"w"}`), http.StatusBadRequest, "bad-request")
	})
	t.Run("invalid name is 400 invalid-name", func(t *testing.T) {
		wantProblem(t, do(t, h, "POST", "/worlds", `{"name":"","seed":1}`), http.StatusBadRequest, "invalid-name")
	})
	t.Run("override outside bounds is 400 invalid-scenario with issues", func(t *testing.T) {
		rec := do(t, h, "POST", "/worlds", `{"name":"w","seed":1,"overrides":{"workforce.regularWorkers":-5}}`)
		p := wantProblem(t, rec, http.StatusBadRequest, "invalid-scenario")
		if is, _ := p["issues"].([]any); len(is) == 0 {
			t.Errorf("want issues, got %v", p)
		}
	})
}

func TestProblemRoutes(t *testing.T) {
	h := newHandler(t, nil)
	wantProblem(t, do(t, h, "GET", "/branches/00000000-0000-4000-8000-000000000000", nil), http.StatusNotFound, "not-found")
	wantProblem(t, do(t, h, "GET", "/branches/not-a-uuid", nil), http.StatusNotFound, "not-found")
	wantProblem(t, do(t, h, "GET", "/worlds/00000000-0000-4000-8000-000000000000/branches", nil), http.StatusNotFound, "not-found")
	wantProblem(t, do(t, h, "GET", "/nope", nil), http.StatusNotFound, "not-found")
	rec := do(t, h, "DELETE", "/worlds", nil)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("DELETE /worlds = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestStaleHeadAndHistoryImmutable(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	path := "/branches/" + b

	// Head is 0: expecting 3 is stale.
	p := wantProblem(t, do(t, h, "POST", path+"/advance", obj{"expectedHeadTick": 3}), http.StatusConflict, "stale-head")
	if num(t, p, "expectedHeadTick") != 3 || num(t, p, "actualHeadTick") != 0 {
		t.Errorf("stale-head ticks = %v", p)
	}
	wantProblem(t, do(t, h, "POST", path+"/commands", obj{
		"windowTick": 0, "expectedHeadTick": 2, "commands": []obj{{"kind": "set_price", "sku": 0, "amountCents": 5999}},
	}), http.StatusConflict, "stale-head")
	wantProblem(t, do(t, h, "POST", path+"/decision-window/end", obj{"windowTick": 0, "expectedHeadTick": 2}), http.StatusConflict, "stale-head")

	adv := mustStatus(t, do(t, h, "POST", path+"/advance", obj{"expectedHeadTick": 0}), http.StatusOK)
	if head := num(t, adv, "headTick"); head < 1 {
		t.Fatalf("advance did not move: %v", adv["headTick"])
	}

	// A command for the window at tick 0 would take effect at tick 1, which is computed.
	wantProblem(t, do(t, h, "POST", path+"/commands", obj{
		"windowTick": 0, "commands": []obj{{"kind": "set_price", "sku": 0, "amountCents": 5999}},
	}), http.StatusConflict, "history-immutable")
	// A window above the head is not yet simulated.
	wantProblem(t, do(t, h, "POST", path+"/commands", obj{
		"windowTick": 99, "commands": []obj{{"kind": "set_price", "sku": 0, "amountCents": 5999}},
	}), http.StatusConflict, "not-yet-simulated")
	wantProblem(t, do(t, h, "GET", path+"/metrics?tick=119", nil), http.StatusConflict, "not-yet-simulated")
	wantProblem(t, do(t, h, "GET", path+"/metrics?tick=-1", nil), http.StatusUnprocessableEntity, "invalid-tick")
	wantProblem(t, do(t, h, "GET", path+"/metrics", nil), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "GET", path+"/metrics?tick=abc", nil), http.StatusBadRequest, "bad-request")
}

func TestDisclaimerAndCurrencyOnMetricResponses(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	adv := advanceTo(t, h, b, 4)
	head := num(t, adv, "headTick")
	q := fmt.Sprintf("?tick=%d", head)
	responses := map[string]*httptest.ResponseRecorder{
		"advance":     do(t, h, "POST", "/branches/"+b+"/advance", obj{"runToTick": head + 1}),
		"metrics":     do(t, h, "GET", "/branches/"+b+"/metrics"+q, nil),
		"state":       do(t, h, "GET", "/branches/"+b+"/state"+q, nil),
		"tick-frames": do(t, h, "GET", "/branches/"+b+"/tick-frames?from=0&to="+fmt.Sprint(head), nil),
		"ledger":      do(t, h, "GET", "/branches/"+b+"/ledger?from=0&to="+fmt.Sprint(head), nil),
	}
	for name, rec := range responses {
		m := mustStatus(t, rec, http.StatusOK)
		if m["disclaimer"] != sim.Disclaimer {
			t.Errorf("%s: disclaimer = %v", name, m["disclaimer"])
		}
		if m["currency"] != "USD" {
			t.Errorf("%s: currency = %v", name, m["currency"])
		}
	}
	// The metrics identity holds and money is integer cents.
	m := decode(t, responses["metrics"])["metrics"].(obj)
	rec := m["reconciliation"].(obj)
	if rec["accrualToCashHolds"] != true || rec["balanceIdentityHolds"] != true {
		t.Errorf("reconciliation = %v", rec)
	}
	if num(t, m, "netProfitCents") != num(t, m, "contributionCents")-num(t, m, "laborExpenseCents")-num(t, m, "marketingExpenseCents")-num(t, m, "overheadExpenseCents")-num(t, m, "serviceCreditExpenseCents") {
		t.Errorf("net profit is not the documented formula: %v", m)
	}
}

func TestWorldReadOmitsScript(t *testing.T) {
	h := newHandler(t, nil)
	m := demo(t, h)
	for name, body := range map[string]string{
		"demo response": mustJSON(t, m),
		"world GET":     do(t, h, "GET", "/worlds/"+m["world"].(obj)["id"].(string), nil).Body.String(),
	} {
		low := strings.ToLower(body)
		for _, banned := range []string{"disruptionscript", "supplier_outage", "demand_surge", "workforce_absence", "onsettick"} {
			if strings.Contains(low, banned) {
				t.Errorf("%s leaks the disruption script (%q): %.200s", name, banned, body)
			}
		}
	}
	w := decode(t, do(t, h, "GET", "/worlds/"+m["world"].(obj)["id"].(string), nil))
	as := w["assumptions"].([]any)
	if len(as) == 0 {
		t.Fatal("no assumptions")
	}
	a := as[0].(obj)
	for _, k := range []string{"path", "value", "unit", "default", "min", "max", "rationale", "overridden"} {
		if _, ok := a[k]; !ok {
			t.Errorf("assumption lacks %q: %v", k, a)
		}
	}
	if w["currency"] != "USD" || w["engineVersion"] != strconv.Itoa(sim.EngineVersion) {
		t.Errorf("world = %v", w)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOverrideShowsInAssumptions(t *testing.T) {
	h := newHandler(t, nil)
	m := mustStatus(t, do(t, h, "POST", "/worlds", obj{"name": "w", "seed": 5, "overrides": obj{"workforce.regularWorkers": 12}}), http.StatusCreated)
	for _, a := range m["world"].(obj)["assumptions"].([]any) {
		if a.(obj)["path"] == "workforce.regularWorkers" {
			if num(t, a.(obj), "value") != 12 || a.(obj)["overridden"] != true {
				t.Fatalf("assumption = %v", a)
			}
			return
		}
	}
	t.Fatal("workforce.regularWorkers not listed")
}

func TestDemoResponseShape(t *testing.T) {
	h := newHandler(t, nil)
	m := demo(t, h)
	root := m["rootBranch"].(obj)
	if root["parentBranchId"] != nil || num(t, root, "headTick") != 0 {
		t.Errorf("root branch = %v", root)
	}
	plan := m["plan"].(obj)
	if num(t, plan, "seed") != 2026 || num(t, plan, "forkTick") < num(t, plan, "crisisTick") {
		t.Errorf("plan = %v", plan)
	}
	if cmds := plan["commands"].([]any); len(cmds) == 0 || cmds[0].(obj)["role"] != "backup" || cmds[0].(obj)["kind"] != "place_purchase_order" {
		t.Errorf("plan commands = %v", plan["commands"])
	}
	if len(plan["reasons"].([]any)) == 0 {
		t.Error("plan has no reasons")
	}
	mustStatus(t, do(t, h, "POST", "/demo", obj{"name": "Custom"}), http.StatusCreated)
	wantProblem(t, do(t, h, "POST", "/demo", obj{"nome": "x"}), http.StatusBadRequest, "bad-request")
}

func TestFullFlowOverHTTP(t *testing.T) {
	h := newHandler(t, nil)
	d := demo(t, h)
	world := d["world"].(obj)["id"].(string)
	root := branchID(d)
	plan := d["plan"].(obj)
	forkTick := num(t, plan, "forkTick")
	planCmd := plan["commands"].([]any)[0].(obj)

	// advance the root to the fork tick
	adv := advanceTo(t, h, root, forkTick)
	if num(t, adv, "headTick") != forkTick {
		t.Fatalf("root head = %d, want %d", num(t, adv, "headTick"), forkTick)
	}
	rootHash := adv["stateHash"].(string)

	// fork there
	child := mustStatus(t, do(t, h, "POST", "/branches/"+root+"/fork", obj{"atTick": forkTick, "label": "Backup supplier"}), http.StatusCreated)
	cid := child["id"].(string)
	if child["parentBranchId"] != root || num(t, child, "headTick") != forkTick || num(t, child, "forkTick") != forkTick {
		t.Fatalf("child = %v", child)
	}
	st := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/state?tick=%d", cid, forkTick), nil), http.StatusOK)
	if st["stateHash"] != rootHash {
		t.Errorf("fork fidelity: child hash %v != parent hash %v", st["stateHash"], rootHash)
	}
	reasons := st["observation"].(obj)["window"].(obj)["reasons"].([]any)
	if !contains(reasons, "fork") {
		t.Errorf("child window reasons = %v, want fork", reasons)
	}

	// submit the plan command on the child, twice with one idempotency key
	body := obj{"windowTick": forkTick, "commands": []obj{{
		"kind": "place_purchase_order", "sku": planCmd["skuIndex"], "role": planCmd["role"], "qty": planCmd["qty"],
	}}}
	sub := mustStatus(t, do(t, h, "POST", "/branches/"+cid+"/commands", body, "Idempotency-Key", "demo-1"), http.StatusOK)
	acc := sub["accepted"].([]any)
	if len(acc) != 1 || len(sub["rejected"].([]any)) != 0 || sub["duplicate"] != false {
		t.Fatalf("submit = %v", sub)
	}
	if a := acc[0].(obj); num(t, a, "effectiveTick") != forkTick+1 || a["provenance"] != "human" || a["command"].(obj)["role"] != "backup" {
		t.Errorf("accepted = %v", a)
	}
	dup := mustStatus(t, do(t, h, "POST", "/branches/"+cid+"/commands", body, "Idempotency-Key", "demo-1"), http.StatusOK)
	if dup["duplicate"] != true || len(dup["accepted"].([]any)) != 1 {
		t.Errorf("duplicate submit = %v", dup)
	}
	wantProblem(t, do(t, h, "POST", "/branches/"+cid+"/commands", body, "Idempotency-Key", "bad key"), http.StatusBadRequest, "bad-request")

	// advance the child past the backup lead time
	end := forkTick + 30
	adv = advanceTo(t, h, cid, end)
	if num(t, adv, "headTick") != end {
		t.Fatalf("child head = %v", adv["headTick"])
	}

	// the ledger shows the opening entry and the purchase receipt, each balanced
	kinds := map[string]bool{}
	var seen, total int64 = 0, -1
	for total < 0 || seen < total {
		led := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/ledger?from=0&to=%d&account=Inventory&limit=1000&offset=%d", cid, end, seen), nil), http.StatusOK)
		total = num(t, led, "total")
		entries := led["entries"].([]any)
		if len(entries) == 0 {
			t.Fatalf("empty page at offset %d of %d", seen, total)
		}
		seen += int64(len(entries))
		for _, e := range entries {
			e := e.(obj)
			kinds[e["cause"].(obj)["kind"].(string)] = true
			var dr, cr int64
			for _, l := range e["lines"].([]any) {
				dr += num(t, l.(obj), "debitCents")
				cr += num(t, l.(obj), "creditCents")
			}
			if dr != cr || e["description"] == "" {
				t.Errorf("entry %v: debits %d credits %d", e, dr, cr)
			}
		}
	}
	paged := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/ledger?from=0&to=%d&limit=2&offset=1", cid, end), nil), http.StatusOK)
	if len(paged["entries"].([]any)) != 2 || num(t, paged, "offset") != 1 || num(t, paged, "total") <= 3 {
		t.Errorf("paged ledger = total %v entries %d", paged["total"], len(paged["entries"].([]any)))
	}
	wantProblem(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/ledger?from=0&to=%d&account=Bogus", cid, end), nil), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/ledger?from=0&to=%d&limit=0", cid, end), nil), http.StatusUnprocessableEntity, "validation")

	// frames for the range are in order and each hash matches the state read
	fr := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/tick-frames?from=%d&to=%d", cid, forkTick, end), nil), http.StatusOK)
	frames := fr["frames"].([]any)
	if int64(len(frames)) != end-forkTick+1 || frames[0].(obj)["stateHash"] != rootHash {
		t.Errorf("frames: %d, first hash %v", len(frames), frames[0].(obj)["stateHash"])
	}

	// the parent did not move and its hash is unchanged
	pb := mustStatus(t, do(t, h, "GET", "/branches/"+root, nil), http.StatusOK)
	if num(t, pb, "headTick") != forkTick {
		t.Errorf("parent head moved to %v", pb["headTick"])
	}
	pm := mustStatus(t, do(t, h, "GET", fmt.Sprintf("/branches/%s/metrics?tick=%d", root, forkTick), nil), http.StatusOK)
	if pm["stateHash"] != rootHash {
		t.Errorf("parent hash changed: %v", pm["stateHash"])
	}

	// branch tree
	list := mustStatus(t, do(t, h, "GET", "/worlds/"+world+"/branches", nil), http.StatusOK)
	bs := list["branches"].([]any)
	if len(bs) != 2 || bs[0].(obj)["id"] != root || bs[1].(obj)["id"] != cid {
		t.Errorf("branches = %v", bs)
	}

	// end the child's window: a window is open at the new head only if scheduled
	wantProblem(t, do(t, h, "POST", "/branches/"+cid+"/decision-window/end", obj{"windowTick": 0}), http.StatusConflict, "history-immutable")
}

func contains(xs []any, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestEndWindowThenSubmitIsWindowNotOpen(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	rec := do(t, h, "POST", "/branches/"+b+"/decision-window/end", obj{"windowTick": 0, "expectedHeadTick": 0})
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("end window = %d %q", rec.Code, rec.Body.String())
	}
	wantProblem(t, do(t, h, "POST", "/branches/"+b+"/commands", obj{
		"windowTick": 0, "commands": []obj{{"kind": "set_price", "sku": 0, "amountCents": 5999}},
	}), http.StatusConflict, "window-not-open")
	wantProblem(t, do(t, h, "POST", "/branches/"+b+"/decision-window/end", obj{"windowTick": 0}), http.StatusConflict, "window-not-open")
}

func TestRejectedCommandsAndRequestValidation(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	path := "/branches/" + b + "/commands"
	res := mustStatus(t, do(t, h, "POST", path, obj{"windowTick": 0, "commands": []obj{
		{"kind": "set_price", "sku": 0, "amountCents": 1},
		{"kind": "set_marketing_spend", "sku": 0, "amountCents": 99999999},
		{"kind": "place_purchase_order", "sku": 99, "role": "primary", "qty": 5},
		{"kind": "place_purchase_order", "sku": 0, "qty": 5},
	}}), http.StatusOK)
	var codes []string
	for _, r := range res["rejected"].([]any) {
		codes = append(codes, r.(obj)["code"].(string))
	}
	if got := strings.Join(codes, ","); got != "price_out_of_bounds,marketing_exceeds_limit,unknown_sku,unknown_supplier" {
		t.Errorf("reject codes = %s", got)
	}
	if len(res["accepted"].([]any)) != 0 {
		t.Errorf("accepted = %v", res["accepted"])
	}
	wantProblem(t, do(t, h, "POST", path, obj{"windowTick": 0, "commands": []obj{}}), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "POST", path, obj{"windowTick": 0, "commands": []obj{{"kind": "teleport"}}}), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "POST", path, obj{"commands": []obj{{"kind": "set_price"}}}), http.StatusBadRequest, "bad-request")

	// the budget: 5 per window; a sixth accepted command is over budget
	var cmds []obj
	for i := 0; i < 5; i++ {
		cmds = append(cmds, obj{"kind": "set_price", "sku": 0, "amountCents": 5999})
	}
	ok := mustStatus(t, do(t, h, "POST", path, obj{"windowTick": 0, "commands": cmds}), http.StatusOK)
	if len(ok["accepted"].([]any)) != 5 || num(t, ok, "budgetRemaining") != 0 {
		t.Fatalf("submit 5 = %v", ok)
	}
	wantProblem(t, do(t, h, "POST", path, obj{"windowTick": 0, "commands": cmds[:1]}), http.StatusUnprocessableEntity, "budget-exceeded")
}

func TestReadRangesAreBounded(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	p := "/branches/" + b + "/tick-frames"
	wantProblem(t, do(t, h, "GET", p+"?from=0&to=240", nil), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "GET", p+"?from=5&to=2", nil), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "GET", p+"?from=0&to=3", nil), http.StatusConflict, "not-yet-simulated")
	wantProblem(t, do(t, h, "GET", p+"?from=0", nil), http.StatusBadRequest, "bad-request")
	fr := mustStatus(t, do(t, h, "GET", p+"?from=0&to=0", nil), http.StatusOK)
	if len(fr["frames"].([]any)) != 1 {
		t.Errorf("frames = %v", fr["frames"])
	}
}

func TestBranchLimit(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	for i := 1; i < app.MaxBranchesPerWorld; i++ {
		mustStatus(t, do(t, h, "POST", "/branches/"+b+"/fork", obj{"atTick": 0, "label": fmt.Sprintf("b%d", i)}), http.StatusCreated)
	}
	wantProblem(t, do(t, h, "POST", "/branches/"+b+"/fork", obj{"atTick": 0, "label": "one too many"}), http.StatusConflict, "branch-limit")
	wantProblem(t, do(t, h, "POST", "/branches/"+b+"/fork", obj{"atTick": 0, "label": ""}), http.StatusBadRequest, "invalid-name")
}

func TestAdvanceToHorizon(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	res := advanceTo(t, h, b, 120)
	if res["paused"] != "horizon" || num(t, res, "headTick") != 120 {
		t.Fatalf("advance to horizon = %v", res["paused"])
	}
	wantProblem(t, do(t, h, "POST", "/branches/"+b+"/advance", nil), http.StatusConflict, "horizon-reached")
	fresh := branchID(demo(t, h))
	wantProblem(t, do(t, h, "POST", "/branches/"+fresh+"/advance", obj{"runToTick": 0}), http.StatusUnprocessableEntity, "invalid-tick")
}

func TestCORS(t *testing.T) {
	h := newHandler(t, nil)
	pre := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("OPTIONS", "/api/v1/worlds", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	ok := pre(webOrigin)
	if ok.Code != http.StatusNoContent || ok.Header().Get("Access-Control-Allow-Origin") != webOrigin ||
		!strings.Contains(ok.Header().Get("Access-Control-Allow-Methods"), "POST") ||
		!strings.Contains(ok.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key") {
		t.Errorf("allowed preflight = %d %v", ok.Code, ok.Header())
	}
	bad := pre("http://evil.example")
	if got := bad.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("foreign origin got Access-Control-Allow-Origin %q", got)
	}
	if bad.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("foreign origin got allow-methods")
	}
	// a normal request from the web origin carries the header, a foreign one does not
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	req.Header.Set("Origin", webOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != webOrigin {
		t.Errorf("simple request lacks allow-origin: %v", rec.Header())
	}
	req = httptest.NewRequest("GET", "/api/v1/health", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("foreign simple request got allow-origin")
	}
}

func TestRateLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := newHandler(t, func(o *httpapi.Options) {
		o.RatePerSecond, o.RateBurst = 1, 3
		o.Now = func() time.Time { return now }
	})
	for i := 0; i < 3; i++ {
		mustStatus(t, do(t, h, "GET", "/health", nil), http.StatusOK)
	}
	rec := do(t, h, "GET", "/health", nil)
	wantProblem(t, rec, http.StatusTooManyRequests, "rate-limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	now = now.Add(2 * time.Second)
	mustStatus(t, do(t, h, "GET", "/health", nil), http.StatusOK)

	// another client address has its own bucket
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	req.RemoteAddr = "198.51.100.7:4000"
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Errorf("second client = %d", out.Code)
	}
}

// The same inputs over HTTP give the same hashes: two worlds with one seed.
func TestSameSeedSameHashesOverHTTP(t *testing.T) {
	h := newHandler(t, nil)
	a, b := branchID(demo(t, h)), branchID(demo(t, h))
	ha := advanceTo(t, h, a, 24)["stateHash"]
	hb := advanceTo(t, h, b, 24)["stateHash"]
	if ha != hb {
		t.Fatalf("hashes differ: %v vs %v", ha, hb)
	}
	// stepping size does not change the hash
	c := branchID(demo(t, h))
	var last any
	for tick := int64(1); tick <= 24; tick++ {
		last = advanceTo(t, h, c, tick)["stateHash"]
	}
	if last != ha {
		t.Fatalf("hash after single steps %v != %v", last, ha)
	}
}

func TestInjectDisruptionEndpoint(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	path := "/branches/" + b + "/disruptions"
	body := obj{"kind": "workforce_absence", "startTick": 10, "durationTicks": 6, "magnitudeBps": 3000}

	res := mustStatus(t, do(t, h, "POST", path, body, "Idempotency-Key", "inj-1"), http.StatusOK)
	if res["kind"] != "workforce_absence" || num(t, res, "effectiveTick") != 10 || res["provenance"] != "injected" || res["duplicate"] != false {
		t.Fatalf("result = %v", res)
	}
	dup := mustStatus(t, do(t, h, "POST", path, body, "Idempotency-Key", "inj-1"), http.StatusOK)
	if dup["duplicate"] != true || num(t, dup, "seq") != num(t, res, "seq") {
		t.Fatalf("duplicate = %v", dup)
	}

	// Out of range: 422 validation naming the allowed range.
	p := wantProblem(t, do(t, h, "POST", path, obj{"kind": "workforce_absence", "startTick": 12, "durationTicks": 6, "magnitudeBps": 20000}), http.StatusUnprocessableEntity, "validation")
	if !strings.Contains(fmt.Sprint(p["issues"]), "0..10000") {
		t.Errorf("issues do not name the range: %v", p["issues"])
	}
	wantProblem(t, do(t, h, "POST", path, obj{"kind": "nope", "startTick": 12, "durationTicks": 6}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", path, obj{"kind": "supplier_outage", "startTick": 12, "durationTicks": 6}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", path, obj{"kind": "workforce_absence", "durationTicks": 6}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", path, obj{"kind": "workforce_absence", "startTick": 12, "durationTicks": 6, "bogus": 1}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", path, obj{"kind": "workforce_absence", "startTick": 12, "durationTicks": 6, "expectedHeadTick": 5}), http.StatusConflict, "stale-head")

	// Advance pauses at the injected onset (tick 10 is not a cadence tick).
	var head int64
	for i := 0; i < 5 && head < 10; i++ {
		adv := mustStatus(t, do(t, h, "POST", "/branches/"+b+"/advance", obj{}), http.StatusOK)
		head = num(t, adv, "headTick")
		if head == 10 && fmt.Sprint(adv["windowReasons"]) != "[disruption_onset]" {
			t.Fatalf("onset reasons = %v", adv["windowReasons"])
		}
	}
	if head != 10 {
		t.Fatalf("advance stopped at %d, want 10", head)
	}
	// History is immutable: the start tick is now computed.
	wantProblem(t, do(t, h, "POST", path, obj{"kind": "workforce_absence", "startTick": 10, "durationTicks": 2, "magnitudeBps": 100}), http.StatusConflict, "history-immutable")

	// A supplier outage needs a role.
	mustStatus(t, do(t, h, "POST", path, obj{"kind": "supplier_outage", "role": "backup", "startTick": 20, "durationTicks": 4}), http.StatusOK)
}

func TestEndWindowStaleHeadIsCheckedByService(t *testing.T) {
	h := newHandler(t, nil)
	b := branchID(demo(t, h))
	p := wantProblem(t, do(t, h, "POST", "/branches/"+b+"/decision-window/end", obj{"windowTick": 0, "expectedHeadTick": 7}), http.StatusConflict, "stale-head")
	if num(t, p, "expectedHeadTick") != 7 || num(t, p, "actualHeadTick") != 0 {
		t.Errorf("stale ticks = %v", p)
	}
	if rec := do(t, h, "POST", "/branches/"+b+"/decision-window/end", obj{"windowTick": 0, "expectedHeadTick": 0}); rec.Code != http.StatusNoContent {
		t.Fatalf("matching end status = %d %s", rec.Code, rec.Body.String())
	}
}
