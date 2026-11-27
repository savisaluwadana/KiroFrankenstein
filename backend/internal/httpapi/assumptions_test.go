package httpapi_test

import (
	"net/http"
	"testing"
)

func scriptOnsets(t *testing.T, m obj) []int64 {
	t.Helper()
	raw, ok := m["disruptionScript"].([]any)
	if !ok {
		return nil
	}
	out := make([]int64, 0, len(raw))
	for _, e := range raw {
		out = append(out, num(t, e.(obj), "onsetTick"))
	}
	return out
}

func equalTicks(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAssumptionsDefaultHidesScript(t *testing.T) {
	h := newHandler(t, nil)
	d := demo(t, h)
	w := d["world"].(obj)["id"].(string)
	for _, path := range []string{"/worlds/" + w + "/assumptions", "/worlds/" + w + "/assumptions?revealScript=false"} {
		m := mustStatus(t, do(t, h, "GET", path, nil), http.StatusOK)
		if _, has := m["disruptionScript"]; has || m["scriptRevealed"] != false {
			t.Fatalf("script leaked by default: %v", m)
		}
		as := m["assumptions"].([]any)
		if len(as) == 0 {
			t.Fatal("no assumptions")
		}
		for _, k := range []string{"path", "value", "unit", "default", "min", "max", "rationale", "overridden"} {
			if _, ok := as[0].(obj)[k]; !ok {
				t.Fatalf("assumption lacks %q: %v", k, as[0])
			}
		}
	}
}

func TestAssumptionsRevealFiltersByHeadAndShowsAllAtHorizon(t *testing.T) {
	h := newHandler(t, nil)
	d := demo(t, h)
	w := d["world"].(obj)["id"].(string)
	b := branchID(d)
	url := "/worlds/" + w + "/assumptions?revealScript=true"

	m := mustStatus(t, do(t, h, "GET", url, nil), http.StatusOK)
	if m["scriptRevealed"] != true || m["scriptComplete"] != false || len(scriptOnsets(t, m)) != 0 {
		t.Fatalf("at tick 0: %v", m)
	}
	advanceTo(t, h, b, 54)
	m = mustStatus(t, do(t, h, "GET", url+"&branchId="+b, nil), http.StatusOK)
	if got := scriptOnsets(t, m); !equalTicks(got, []int64{48, 54}) || m["scriptComplete"] != false {
		t.Fatalf("at tick 54 onsets = %v", got)
	}
	advanceTo(t, h, b, 120)
	m = mustStatus(t, do(t, h, "GET", url, nil), http.StatusOK)
	if got := scriptOnsets(t, m); !equalTicks(got, []int64{48, 54, 60}) || m["scriptComplete"] != true {
		t.Fatalf("at horizon onsets = %v", got)
	}
}

func TestAssumptionsRevealDuelBranchObeysDuelHead(t *testing.T) {
	h := newHandler(t, nil)
	c := mustStatus(t, do(t, h, "POST", "/duels", obj{"forkTick": 55}), http.StatusCreated)
	dw := c["duel"].(obj)["worldId"].(string)
	hb := c["duel"].(obj)["human"].(obj)["branch"].(obj)["id"].(string)
	ab := c["duel"].(obj)["ai"].(obj)["branch"].(obj)["id"].(string)
	for _, b := range []string{hb, ab} {
		m := mustStatus(t, do(t, h, "GET", "/worlds/"+dw+"/assumptions?revealScript=true&branchId="+b, nil), http.StatusOK)
		if got := scriptOnsets(t, m); !equalTicks(got, []int64{48, 54}) || m["scriptComplete"] != false {
			t.Fatalf("duel branch %s onsets = %v", b, got)
		}
	}
}

func TestAssumptionsRevealRejectsBadInput(t *testing.T) {
	h := newHandler(t, nil)
	d := demo(t, h)
	w := d["world"].(obj)["id"].(string)
	other := demo(t, h)
	ob := branchID(other)
	base := "/worlds/" + w + "/assumptions?revealScript=true"
	wantProblem(t, do(t, h, "GET", base+"&branchId="+ob, nil), http.StatusForbidden, "forbidden")
	wantProblem(t, do(t, h, "GET", base+"&branchId=not-a-uuid", nil), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "GET", base+"&branchId=11111111-1111-4111-8111-111111111111", nil), http.StatusNotFound, "not-found")
	wantProblem(t, do(t, h, "GET", "/worlds/"+w+"/assumptions?revealScript=maybe", nil), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "GET", "/worlds/11111111-1111-4111-8111-111111111111/assumptions", nil), http.StatusNotFound, "not-found")
}

func TestAssumptionsFlagOverrides(t *testing.T) {
	h := newHandler(t, nil)
	c := mustStatus(t, do(t, h, "POST", "/worlds", obj{"name": "w", "seed": 5, "overrides": obj{"workforce.regularWorkers": 12}}), http.StatusCreated)
	w := c["world"].(obj)["id"].(string)
	m := mustStatus(t, do(t, h, "GET", "/worlds/"+w+"/assumptions", nil), http.StatusOK)
	changed := 0
	for _, a := range m["assumptions"].([]any) {
		x := a.(obj)
		if x["overridden"] == true {
			changed++
			if x["path"] != "workforce.regularWorkers" || num(t, x, "value") != 12 || num(t, x, "value") == num(t, x, "default") {
				t.Fatalf("unexpected override flag: %v", x)
			}
		}
	}
	if changed != 1 {
		t.Fatalf("overridden count = %d, want 1", changed)
	}
}
