package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

func TestEngineVersionWarningsOverHTTP(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	old := app.NewBranchService(st, nil, nil, app.WithEngineVersion(1))
	world, root, err := old.CreateWorld(ctx, app.CreateWorldRequest{Name: "old", ScenarioJSON: scenarios.BlackFriday, Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Advance(ctx, app.AdvanceRequest{Branch: root.ID}); err != nil {
		t.Fatal(err)
	}
	h := newHandler(t, func(o *httpapi.Options) { o.Service = app.NewBranchService(st, nil, nil) })
	b := "/branches/" + string(root.ID)

	reads := []string{
		"/worlds/" + string(world.ID), b,
		"/worlds/" + string(world.ID) + "/branches",
		b + "/state?tick=2", b + "/metrics?tick=2", b + "/tick-frames?from=0&to=2",
		b + "/ledger?from=0&to=2", b + "/journal?from=0&to=2", b + "/end-of-horizon",
	}
	for _, p := range reads {
		rec := do(t, h, "GET", p, nil)
		m := mustStatus(t, rec, http.StatusOK)
		ws, ok := m["warnings"].([]any)
		if !ok || len(ws) != 1 {
			t.Errorf("GET %s: warnings = %v", p, m["warnings"])
			continue
		}
		w := ws[0].(obj)
		msg, _ := w["message"].(string)
		if w["code"] != "engine-version-mismatch" || !strings.Contains(msg, "version 1") || !strings.Contains(msg, "version 2") {
			t.Errorf("GET %s: warning = %v", p, w)
		}
	}
	if rec := do(t, h, "GET", b+"/export", nil); rec.Header().Get("X-Parallel-Warnings") != "engine-version-mismatch" {
		t.Errorf("export header = %q", rec.Header().Get("X-Parallel-Warnings"))
	}

	// Mutations answer 409.
	p := wantProblem(t, do(t, h, "POST", b+"/advance", obj{}), http.StatusConflict, "engine-version-mismatch")
	if d, _ := p["detail"].(string); !strings.Contains(d, "version 1") {
		t.Errorf("detail = %q", d)
	}
	wantProblem(t, do(t, h, "POST", b+"/fork", obj{"atTick": 1, "label": "x"}), http.StatusConflict, "engine-version-mismatch")

	// A matching world has no warnings field.
	d := demo(t, h)
	root2 := branchID(d)
	for _, p := range []string{"/branches/" + root2, "/branches/" + root2 + "/state?tick=0"} {
		if m := mustStatus(t, do(t, h, "GET", p, nil), http.StatusOK); m["warnings"] != nil {
			t.Errorf("GET %s: unexpected warnings %v", p, m["warnings"])
		}
	}
}
