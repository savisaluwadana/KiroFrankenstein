package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/postgres"
)

// TestPostgresRestartSmoke: create the demo World, advance, "restart" with a
// new service over the same database, and read the same hash back.
func TestPostgresRestartSmoke(t *testing.T) {
	url := os.Getenv("PARALLEL_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PARALLEL_TEST_DATABASE_URL not set")
	}
	pool, err := postgres.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mk := func() http.Handler {
		return newHandler(t, func(o *httpapi.Options) {
			o.Service = app.NewBranchService(postgres.New(pool), nil, nil)
		})
	}

	h1 := mk()
	d := demo(t, h1)
	world, root := d["world"].(obj)["id"].(string), branchID(d)
	adv := advanceTo(t, h1, root, 12)
	head := num(t, adv, "headTick")
	hash := adv["stateHash"].(string)

	h2 := mk() // restart
	mustStatus(t, do(t, h2, "GET", "/worlds/"+world, nil), http.StatusOK)
	b := mustStatus(t, do(t, h2, "GET", "/branches/"+root, nil), http.StatusOK)
	if num(t, b, "headTick") != head {
		t.Fatalf("head after restart = %v, want %d", b["headTick"], head)
	}
	st := mustStatus(t, do(t, h2, "GET", fmt.Sprintf("/branches/%s/state?tick=%d", root, head), nil), http.StatusOK)
	if st["stateHash"] != hash {
		t.Fatalf("hash after restart %v != %v", st["stateHash"], hash)
	}
}
