package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim/inputsfile"
)

func TestExportBranch(t *testing.T) {
	h := newHandler(t, nil)
	w := decode(t, do(t, h, http.MethodPost, "/worlds", obj{"name": "w", "seed": 7}))
	branch := w["rootBranch"]
	if branch == nil {
		t.Fatalf("no root branch in %v", w)
	}
	id := branch.(obj)["id"].(string)

	rec := do(t, h, http.MethodGet, "/branches/"+id+"/export", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d type %q body %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	f, err := inputsfile.Parse(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("export does not parse: %v\n%s", err, rec.Body.String())
	}
	if f.Seed != 7 || len(f.Inputs) != 0 || len(f.ScenarioHash) != 64 {
		t.Fatalf("file = %+v", f)
	}
	if string(inputsfile.Print(f)) != rec.Body.String() {
		t.Error("export is not the canonical printing")
	}

	wantProblem(t, do(t, h, http.MethodGet, "/branches/00000000-0000-4000-8000-000000000000/export", nil), http.StatusNotFound, "not-found")
	wantProblem(t, do(t, h, http.MethodGet, "/branches/nope/export", nil), http.StatusNotFound, "not-found")
}
