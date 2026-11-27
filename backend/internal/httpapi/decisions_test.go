package httpapi_test

import (
	"net/http"
	"testing"
)

func TestListDecisionsOverHTTP(t *testing.T) {
	h := newHandler(t, nil)
	world := mustStatus(t, do(t, h, "POST", "/worlds", obj{
		"name": "ai", "seed": 7, "managerKind": "ai_rule_based",
	}), http.StatusCreated)
	root := world["rootBranch"].(obj)
	id := root["id"].(string)

	m := mustStatus(t, do(t, h, "GET", "/branches/"+id+"/decisions", nil), http.StatusOK)
	ds, _ := m["decisions"].([]any)
	if m["branchId"] != id || len(ds) != 1 {
		t.Fatalf("decisions = %v", m)
	}
	d := ds[0].(obj)
	if d["windowTick"] != float64(0) || d["provenance"] != "ai_rule_based" || d["managerKind"] != "ai_rule_based" {
		t.Fatalf("record = %v", d)
	}
	for _, k := range []string{"proposed", "rejected"} {
		if _, ok := d[k].([]any); !ok {
			t.Fatalf("%s must be a JSON array: %v", k, d)
		}
	}
	wantProblem(t, do(t, h, "GET", "/branches/00000000-0000-4000-8000-000000000000/decisions", nil), http.StatusNotFound, "not-found")
}
