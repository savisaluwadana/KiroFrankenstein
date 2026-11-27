package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// diffFixture creates the demo world, forks the root at the plan tick, buys
// from the backup supplier on the child and advances both 10 hours.
func diffFixture(t *testing.T, h http.Handler) (root, child string, forkTick int64) {
	t.Helper()
	d := demo(t, h)
	root = branchID(d)
	plan := d["plan"].(obj)
	forkTick = num(t, plan, "forkTick")
	cmd := plan["commands"].([]any)[0].(obj)
	advanceTo(t, h, root, forkTick)
	c := mustStatus(t, do(t, h, "POST", "/branches/"+root+"/fork", obj{"atTick": forkTick, "label": "Backup supplier"}), http.StatusCreated)
	child = c["id"].(string)
	mustStatus(t, do(t, h, "POST", "/branches/"+child+"/commands", obj{"windowTick": forkTick, "commands": []obj{{
		"kind": "place_purchase_order", "sku": cmd["skuIndex"], "role": cmd["role"], "qty": cmd["qty"],
	}}}), http.StatusOK)
	advanceTo(t, h, child, forkTick+10)
	advanceTo(t, h, root, forkTick+10)
	return root, child, forkTick
}

func TestDiffs(t *testing.T) {
	t.Run("demo fork versus parent", func(t *testing.T) {
		h := newHandler(t, nil)
		root, child, forkTick := diffFixture(t, h)
		res := mustStatus(t, do(t, h, "POST", "/diffs", obj{"branchA": root, "branchB": child}), http.StatusOK)
		if res["branchA"] != root || res["branchB"] != child {
			t.Fatalf("branches = %v / %v", res["branchA"], res["branchB"])
		}
		if num(t, res, "comparisonTick") != forkTick+10 {
			t.Errorf("comparisonTick = %v", res["comparisonTick"])
		}
		dv, ok := res["divergence"].(obj)
		if !ok {
			t.Fatalf("divergence = %v", res["divergence"])
		}
		if got := num(t, dv, "tick"); got <= forkTick {
			t.Errorf("divergence tick %d is not after the fork tick %d", got, forkTick)
		}
		ha, hb := dv["stateHashA"].(string), dv["stateHashB"].(string)
		if len(ha) != 64 || len(hb) != 64 || ha == hb {
			t.Errorf("hashes %q %q", ha, hb)
		}
		if comps := dv["components"].([]any); len(comps) == 0 {
			t.Error("no differing components")
		} else if _, isStr := comps[0].(obj)["name"].(string); !isStr {
			t.Errorf("component name is not a plain string: %v", comps[0])
		}
		ins := res["differingInputs"].([]any)
		if len(ins) != 1 {
			t.Fatalf("differing inputs = %v", ins)
		}
		in := ins[0].(obj)
		if in["side"] != "B" || in["branchId"] != child || in["kind"] != "command" || in["provenance"] != "human" {
			t.Errorf("input = %v", in)
		}
		if in["command"].(obj)["kind"] != "place_purchase_order" || num(t, in, "effectiveTick") != forkTick+1 {
			t.Errorf("input command = %v", in)
		}
		if _, has := in["seq"]; !has {
			t.Error("input has no seq")
		}
		metrics := res["metrics"].([]any)
		if len(metrics) == 0 {
			t.Fatal("no metrics")
		}
		for _, m := range metrics {
			m := m.(obj)
			if num(t, m, "b")-num(t, m, "a") != num(t, m, "delta") {
				t.Errorf("metric %v: delta is not b minus a", m)
			}
		}
		if len(res["skuStock"].([]any)) == 0 {
			t.Error("no sku stock")
		}
	})

	t.Run("identical branches", func(t *testing.T) {
		h := newHandler(t, nil)
		root, _, _ := diffFixture(t, h)
		res := mustStatus(t, do(t, h, "POST", "/diffs", obj{"branchA": root, "branchB": root}), http.StatusOK)
		if res["divergence"] != nil {
			t.Errorf("divergence = %v, want null", res["divergence"])
		}
		if n := len(res["differingInputs"].([]any)); n != 0 {
			t.Errorf("differing inputs = %d", n)
		}
		for _, m := range res["metrics"].([]any) {
			if num(t, m.(obj), "delta") != 0 {
				t.Errorf("metric delta = %v", m)
			}
		}
	})

	t.Run("cross world is incomparable", func(t *testing.T) {
		h := newHandler(t, nil)
		a := branchID(demo(t, h))
		b := branchID(demo(t, h))
		wantProblem(t, do(t, h, "POST", "/diffs", obj{"branchA": a, "branchB": b}), http.StatusUnprocessableEntity, "incomparable-branches")
	})

	t.Run("not yet simulated", func(t *testing.T) {
		h := newHandler(t, nil)
		root, child, forkTick := diffFixture(t, h)
		wantProblem(t, do(t, h, "POST", "/diffs", obj{"branchA": root, "branchB": child, "comparisonTick": forkTick + 11}), http.StatusConflict, "not-yet-simulated")
	})

	t.Run("request validation", func(t *testing.T) {
		h := newHandler(t, nil)
		root, child, _ := diffFixture(t, h)
		wantProblem(t, do(t, h, "POST", "/diffs", obj{"branchA": root}), http.StatusBadRequest, "bad-request")
		wantProblem(t, do(t, h, "POST", "/diffs", obj{"branchA": root, "branchB": child, "extra": 1}), http.StatusBadRequest, "bad-request")
		wantProblem(t, do(t, h, "POST", "/diffs", obj{"branchA": root, "branchB": "nope"}), http.StatusNotFound, "not-found")
		wantProblem(t, do(t, h, "POST", "/diffs", obj{"branchA": root, "branchB": child, "comparisonTick": -1}), http.StatusUnprocessableEntity, "invalid-tick")
	})

	t.Run("statement and disclaimer verbatim", func(t *testing.T) {
		h := newHandler(t, nil)
		root, child, forkTick := diffFixture(t, h)
		res := mustStatus(t, do(t, h, "POST", "/diffs", obj{"branchA": root, "branchB": child, "comparisonTick": forkTick + 5}), http.StatusOK)
		if res["statement"] != app.DiffStatement {
			t.Errorf("statement = %q", res["statement"])
		}
		if res["disclaimer"] != sim.Disclaimer {
			t.Errorf("disclaimer = %q", res["disclaimer"])
		}
		if res["currency"] != app.DiffCurrency {
			t.Errorf("currency = %v", res["currency"])
		}
		if num(t, res, "comparisonTick") != forkTick+5 {
			t.Errorf("comparisonTick = %v", res["comparisonTick"])
		}
	})
}
