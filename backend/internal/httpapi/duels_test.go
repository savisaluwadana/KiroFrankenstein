package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func createDuelFixture(t *testing.T, h http.Handler, body any) obj {
	t.Helper()
	return mustStatus(t, do(t, h, "POST", "/duels", body), http.StatusCreated)
}

func TestDuelsFullFlow(t *testing.T) {
	h := newHandler(t, nil)
	c := createDuelFixture(t, h, obj{"name": "Test duel"})
	duel := c["duel"].(obj)
	id := duel["id"].(string)
	human := duel["human"].(obj)
	ai := duel["ai"].(obj)
	if human["managerLabel"] != "You" || ai["managerLabel"] != "Rule-based AI" {
		t.Fatalf("labels = %v / %v", human["managerLabel"], ai["managerLabel"])
	}
	if ai["branch"].(obj)["managerKind"] != "ai_rule_based" || duel["aiKind"] != "ai_rule_based" {
		t.Fatalf("ai kind: %v", ai)
	}
	st := c["status"].(obj)
	if st["waitingOn"] != "human" || num(t, st, "comparisonTick") != 0 || num(t, st, "horizonTick") != 120 {
		t.Fatalf("status = %v", st)
	}
	hb := human["branch"].(obj)["id"].(string)
	ab := ai["branch"].(obj)["id"].(string)

	// The AI's move at the open window is withheld from the human.
	dec := mustStatus(t, do(t, h, "GET", "/duels/"+id+"/decisions", nil), http.StatusOK)
	if n := len(dec["decisions"].([]any)); n != 0 {
		t.Fatalf("decisions visible while waiting on the human: %d", n)
	}

	// The window is open: the duel cannot advance.
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/advance", obj{}), http.StatusConflict, "duel-window-open")

	// Human submits with an idempotency key, twice.
	body := obj{"windowTick": 0, "commands": []obj{{"kind": "schedule_overtime", "qty": 1}}}
	r1 := mustStatus(t, do(t, h, "POST", "/duels/"+id+"/commands", body, "Idempotency-Key", "k-1"), http.StatusOK)
	if len(r1["accepted"].([]any)) != 1 || r1["duplicate"] != false {
		t.Fatalf("first submit = %v", r1)
	}
	r2 := mustStatus(t, do(t, h, "POST", "/duels/"+id+"/commands", body, "Idempotency-Key", "k-1"), http.StatusOK)
	if r2["duplicate"] != true {
		t.Fatalf("repeat submit = %v", r2)
	}

	// One injection hits both sides identically.
	inj := mustStatus(t, do(t, h, "POST", "/duels/"+id+"/disruptions",
		obj{"kind": "demand_surge", "startTick": 10, "durationTicks": 5, "magnitudeBps": 20000}), http.StatusOK)
	if inj["human"].(obj)["effectiveTick"] != inj["ai"].(obj)["effectiveTick"] || inj["human"].(obj)["kind"] != "demand_surge" {
		t.Fatalf("inject = %v", inj)
	}

	if rec := do(t, h, "POST", "/duels/"+id+"/decision-window/end", obj{"windowTick": 0}); rec.Code != http.StatusNoContent {
		t.Fatalf("end window status %d: %s", rec.Code, rec.Body.String())
	}
	// Stale head is a 409 naming both ticks.
	p := wantProblem(t, do(t, h, "POST", "/duels/"+id+"/advance", obj{"expectedHeadTick": 5}), http.StatusConflict, "stale-head")
	if num(t, p, "expectedHeadTick") != 5 || num(t, p, "actualHeadTick") != 0 {
		t.Fatalf("stale problem = %v", p)
	}

	// Mid-duel score is provisional and carries the disclaimer.
	adv := mustStatus(t, do(t, h, "POST", "/duels/"+id+"/advance", obj{"runToTick": 4}), http.StatusOK)
	if num(t, adv, "headTick") != 4 || adv["disclaimer"] != sim.Disclaimer || adv["currency"] != "USD" {
		t.Fatalf("advance = %v", adv["headTick"])
	}
	sc := mustStatus(t, do(t, h, "GET", "/duels/"+id+"/score", nil), http.StatusOK)
	if sc["provisional"] != true || sc["disclaimer"] != sim.Disclaimer || sc["currency"] != "USD" || num(t, sc, "comparisonTick") != 4 {
		t.Fatalf("mid score = %v", sc)
	}
	for _, k := range []string{"weights", "profitFloorCents", "profitTargetCents", "tieToleranceHundredths", "outcome"} {
		if _, ok := sc[k]; !ok {
			t.Fatalf("score lacks %s", k)
		}
	}
	hs := sc["human"].(obj)
	if hs["managerLabel"] != "You" || hs["branchId"] != hb || sc["ai"].(obj)["branchId"] != ab {
		t.Fatalf("score branches = %v", sc)
	}
	if _, ok := hs["metrics"].(obj)["netProfitCents"]; !ok {
		t.Fatalf("raw metrics missing: %v", hs["metrics"])
	}

	// Play to the horizon: end each open window, run to the horizon.
	for i := 0; i < 200; i++ {
		s := mustStatus(t, do(t, h, "GET", "/duels/"+id, nil), http.StatusOK)["status"].(obj)
		if s["finished"] == true {
			break
		}
		if s["waitingOn"] == "human" {
			mustStatus(t, do(t, h, "POST", "/duels/"+id+"/decision-window/end", obj{"windowTick": num(t, s, "humanHeadTick")}), http.StatusNoContent)
		}
		mustStatus(t, do(t, h, "POST", "/duels/"+id+"/advance", obj{"runToTick": 120}), http.StatusOK)
	}
	fin := mustStatus(t, do(t, h, "GET", "/duels/"+id+"/score", nil), http.StatusOK)
	if fin["provisional"] != false || num(t, fin, "comparisonTick") != 120 || fin["disclaimer"] != sim.Disclaimer {
		t.Fatalf("final score = %v", fin)
	}
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/advance", obj{}), http.StatusConflict, "horizon-reached")

	// The AI's decisions are listed with plain labels and provenance.
	dec = mustStatus(t, do(t, h, "GET", "/duels/"+id+"/decisions", nil), http.StatusOK)
	ds := dec["decisions"].([]any)
	if len(ds) == 0 {
		t.Fatal("no AI decisions after the duel")
	}
	d0 := ds[0].(obj)
	if d0["provenance"] != "ai_rule_based" || d0["managerLabel"] != "Rule-based AI" {
		t.Fatalf("decision = %v", d0)
	}
}

func TestDuelsGuardsDirectBranchMutation(t *testing.T) {
	h := newHandler(t, nil)
	c := createDuelFixture(t, h, obj{})
	duel := c["duel"].(obj)
	for _, side := range []string{"human", "ai"} {
		b := duel[side].(obj)["branch"].(obj)["id"].(string)
		for _, tc := range []struct {
			path string
			body any
		}{
			{"/advance", obj{}},
			{"/commands", obj{"windowTick": 0, "commands": []obj{{"kind": "schedule_overtime", "qty": 1}}}},
			{"/decision-window/end", obj{"windowTick": 0}},
			{"/fork", obj{"atTick": 0, "label": "x"}},
			{"/disruptions", obj{"kind": "demand_surge", "startTick": 5, "durationTicks": 2, "magnitudeBps": 20000}},
		} {
			p := wantProblem(t, do(t, h, "POST", "/branches/"+b+tc.path, tc.body), http.StatusConflict, "duel-branch")
			if p["detail"] == "" {
				t.Fatalf("%s %s: no detail", side, tc.path)
			}
		}
		// Reads stay allowed.
		mustStatus(t, do(t, h, "GET", "/branches/"+b+"/state?tick=0", nil), http.StatusOK)
		mustStatus(t, do(t, h, "GET", "/branches/"+b+"/tick-frames?from=0&to=0", nil), http.StatusOK)
	}
	// The parent root branch is not part of the duel's sides and stays mutable.
	root := duel["parentBranchId"].(string)
	mustStatus(t, do(t, h, "POST", "/branches/"+root+"/fork", obj{"atTick": 0, "label": "side"}), http.StatusCreated)
}

func TestDuelsForkTickAndLLMLabel(t *testing.T) {
	h := newHandler(t, nil)
	c := createDuelFixture(t, h, obj{"forkTick": 6, "aiKind": "ai_llm", "seed": 7})
	d := c["duel"].(obj)
	if num(t, d, "forkTick") != 6 || num(t, c["status"].(obj), "comparisonTick") != 6 {
		t.Fatalf("fork tick: %v", c["status"])
	}
	if l := d["ai"].(obj)["managerLabel"].(string); l != "Rule-based AI (LLM fallback: llm_unavailable)" {
		t.Fatalf("unconfigured LLM label = %q", l)
	}
	id := d["id"].(string)
	// End the human window so the fallback record becomes visible.
	mustStatus(t, do(t, h, "POST", "/duels/"+id+"/decision-window/end", obj{"windowTick": 6}), http.StatusNoContent)
	dec := mustStatus(t, do(t, h, "GET", "/duels/"+id+"/decisions", nil), http.StatusOK)["decisions"].([]any)
	if len(dec) == 0 {
		t.Fatal("no decision records")
	}
	r := dec[0].(obj)
	if r["provenance"] != "ai_rule_based_fallback" || r["fallbackReason"] != "llm_unavailable" ||
		r["managerLabel"] != "Rule-based AI (LLM fallback: llm_unavailable)" {
		t.Fatalf("fallback record = %v", r)
	}
}

func TestDuelsValidation(t *testing.T) {
	h := newHandler(t, nil)
	wantProblem(t, do(t, h, "POST", "/duels", obj{"aiKind": "human"}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels", obj{"nope": 1}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels", obj{"seed": -1}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels", obj{"forkTick": -1}), http.StatusUnprocessableEntity, "invalid-tick")
	wantProblem(t, do(t, h, "POST", "/duels", obj{"forkTick": 9999}), http.StatusUnprocessableEntity, "invalid-tick")
	wantProblem(t, do(t, h, "POST", "/duels", obj{"name": ""}), http.StatusBadRequest, "invalid-name")
	wantProblem(t, do(t, h, "GET", "/duels/not-a-uuid", nil), http.StatusNotFound, "not-found")
	wantProblem(t, do(t, h, "GET", "/duels/6f1c0f1e-0000-4000-8000-000000000000/score", nil), http.StatusNotFound, "not-found")

	id := createDuelFixture(t, h, obj{})["duel"].(obj)["id"].(string)
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/commands", obj{"windowTick": 0, "commands": []obj{}}), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/commands", obj{"windowTick": 0, "commands": []obj{{"kind": "bogus"}}}), http.StatusUnprocessableEntity, "validation")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/commands", obj{"commands": []obj{{"kind": "schedule_overtime", "qty": 1}}}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/commands", obj{"windowTick": 0, "commands": []obj{{"kind": "schedule_overtime", "qty": 1}}}, "Idempotency-Key", "bad key"), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/decision-window/end", obj{}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/decision-window/end", obj{"windowTick": 3}), http.StatusConflict, "not-yet-simulated")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/disruptions", obj{"kind": "meteor", "startTick": 5, "durationTicks": 1}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/disruptions", obj{"kind": "supplier_outage", "startTick": 5, "durationTicks": 1}), http.StatusBadRequest, "bad-request")
	wantProblem(t, do(t, h, "POST", "/duels/"+id+"/disruptions", obj{"kind": "demand_surge", "startTick": 0, "durationTicks": 1, "magnitudeBps": 20000}), http.StatusConflict, "history-immutable")
}
