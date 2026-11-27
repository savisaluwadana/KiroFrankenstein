package httpapi_test

// API operation benchmarks over the PostgreSQL store (Requirements 29.3 to
// 29.7, 29.8, 29.10, 29.11). They skip when PARALLEL_TEST_DATABASE_URL is not
// set. Every correctness check stays on: the real service, validator, engine,
// snapshot verification, projections and state hashing all run; the only thing
// replaced is the network (httptest recorder, no TCP). Results are for
// synthetic simulation only.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/postgres"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

const benchHorizon = 120

func benchPool(b *testing.B) *pgxpool.Pool {
	b.Helper()
	url := os.Getenv("PARALLEL_TEST_DATABASE_URL")
	if url == "" {
		b.Skip("PARALLEL_TEST_DATABASE_URL not set")
	}
	pool, err := postgres.Connect(context.Background(), url)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pool.Close)
	return pool
}

// benchHandler builds a fresh service (empty in-process caches) over the pool.
func benchHandler(b *testing.B, pool *pgxpool.Pool) http.Handler {
	b.Helper()
	s, err := httpapi.New(httpapi.Options{
		Service:          app.NewBranchService(postgres.New(pool), nil, nil),
		FlagshipScenario: scenarios.BlackFriday,
		WebOrigin:        webOrigin, RatePerSecond: 1000000, RateBurst: 1000000,
		// Benchmark setup creates many Worlds; only the per-IP request and
		// World-creation limiters are raised, nothing in the measured path.
		WorldCreatePerHourPerIP: 1 << 30,
	})
	if err != nil {
		b.Fatal(err)
	}
	return s.Handler()
}

// call performs one request and returns the status and body.
func call(h http.Handler, method, path string, body any) (int, []byte) {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, "/api/v1"+path, rd)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func bmust(b *testing.B, h http.Handler, method, path string, body any, want int) obj {
	b.Helper()
	code, raw := call(h, method, path, body)
	if code != want {
		b.Fatalf("%s %s = %d, want %d: %.300s", method, path, code, want, raw)
	}
	var m obj
	if want != http.StatusNoContent {
		if err := json.Unmarshal(raw, &m); err != nil {
			b.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return m
}

func bnum(b *testing.B, m obj, key string) int64 {
	b.Helper()
	f, ok := m[key].(float64)
	if !ok {
		b.Fatalf("field %q missing in %v", key, m)
	}
	return int64(f)
}

// bAdvance runs the branch to tick through disruption-onset pauses (one
// request each) and returns the number of requests made.
func bAdvance(b *testing.B, h http.Handler, branch string, tick int64) int {
	b.Helper()
	for n := 1; n <= 200; n++ {
		r := bmust(b, h, "POST", "/branches/"+branch+"/advance", obj{"runToTick": tick}, http.StatusOK)
		if bnum(b, r, "headTick") >= tick {
			return n
		}
	}
	b.Fatalf("branch %s did not reach tick %d", branch, tick)
	return 0
}

type benchWorld struct {
	world, root string
	forkTick    int64
	cmd         obj
}

// newBenchWorld creates a demo World and returns its root and demo plan.
func newBenchWorld(b *testing.B, h http.Handler) benchWorld {
	b.Helper()
	d := bmust(b, h, "POST", "/demo", nil, http.StatusCreated)
	plan := d["plan"].(obj)
	pc := plan["commands"].([]any)[0].(obj)
	return benchWorld{
		world: d["world"].(obj)["id"].(string), root: d["rootBranch"].(obj)["id"].(string),
		forkTick: bnum(b, plan, "forkTick"),
		cmd:      obj{"kind": "place_purchase_order", "sku": pc["skuIndex"], "role": pc["role"], "qty": pc["qty"]},
	}
}

// benchPair holds a parent and a fork child, both at the horizon.
type benchPair struct {
	benchWorld
	child string
}

func newBenchPair(b *testing.B, h http.Handler) benchPair {
	b.Helper()
	w := newBenchWorld(b, h)
	bAdvance(b, h, w.root, benchHorizon)
	c := bmust(b, h, "POST", "/branches/"+w.root+"/fork", obj{"atTick": w.forkTick, "label": "bench fork"}, http.StatusCreated)
	cid := c["id"].(string)
	bmust(b, h, "POST", "/branches/"+cid+"/commands", obj{"windowTick": w.forkTick, "commands": []obj{w.cmd}}, http.StatusOK)
	bAdvance(b, h, cid, benchHorizon)
	return benchPair{benchWorld: w, child: cid}
}

// BenchmarkAPIStateRead: GET /state of a computed Tick (29.3). The horizon read
// and a mid-history rewind read, with a warm service (head cached) and a cold
// one (a new service per iteration, as after a restart).
func BenchmarkAPIStateRead(b *testing.B) {
	pool := benchPool(b)
	for _, tc := range []struct {
		name string
		tick int64
		cold bool
	}{
		{"horizon-warm", benchHorizon, false}, {"horizon-cold", benchHorizon, true},
		{"rewind-60-warm", 60, false}, {"rewind-60-cold", 60, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			h := benchHandler(b, pool)
			w := newBenchWorld(b, h)
			bAdvance(b, h, w.root, benchHorizon)
			path := fmt.Sprintf("/branches/%s/state?tick=%d", w.root, tc.tick)
			bmust(b, h, "GET", path, nil, http.StatusOK) // warm-up and check
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if tc.cold {
					b.StopTimer()
					h = benchHandler(b, pool)
					b.StartTimer()
				}
				if code, raw := call(h, "GET", path, nil); code != http.StatusOK {
					b.Fatalf("state = %d %.200s", code, raw)
				}
			}
		})
	}
}

// BenchmarkAPIFork: POST /fork at a mid Tick of a branch at the horizon (29.4).
// A World holds at most 32 Branches, so a new World is prepared (untimed)
// every 30 forks.
func BenchmarkAPIFork(b *testing.B) {
	pool := benchPool(b)
	h := benchHandler(b, pool)
	var w benchWorld
	used := 1 << 30
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if used >= 30 {
			b.StopTimer()
			w = newBenchWorld(b, h)
			bAdvance(b, h, w.root, benchHorizon)
			used = 0
			b.StartTimer()
		}
		code, raw := call(h, "POST", "/branches/"+w.root+"/fork", obj{"atTick": 60, "label": "bench"})
		if code != http.StatusCreated {
			b.Fatalf("fork = %d %.200s", code, raw)
		}
		used++
	}
}

// BenchmarkAPIDiff: POST /diffs of a parent and its fork, both at the horizon,
// compared at the horizon (29.5). Warm and cold service.
func BenchmarkAPIDiff(b *testing.B) {
	pool := benchPool(b)
	for _, cold := range []bool{false, true} {
		name := "warm"
		if cold {
			name = "cold"
		}
		b.Run(name, func(b *testing.B) {
			h := benchHandler(b, pool)
			p := newBenchPair(b, h)
			req := obj{"branchA": p.root, "branchB": p.child, "comparisonTick": benchHorizon}
			bmust(b, h, "POST", "/diffs", req, http.StatusOK) // warm-up and check
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if cold {
					b.StopTimer()
					h = benchHandler(b, pool)
					b.StartTimer()
				}
				if code, raw := call(h, "POST", "/diffs", req); code != http.StatusOK {
					b.Fatalf("diff = %d %.200s", code, raw)
				}
			}
		})
	}
}

// BenchmarkAPIAdvance: advancing a fresh branch to the horizon over HTTP, with
// persistence of inputs, projections and snapshots (29.6).
//
//	last-segment: one request from the Tick of the last disruption-onset pause
//	to the horizon (no open Decision_Window in between, the 29.6 wording).
//	from-zero: Tick 0 to the horizon, including the extra requests that resume
//	after each disruption-onset pause (the number of requests is reported).
func BenchmarkAPIAdvance(b *testing.B) {
	pool := benchPool(b)
	h := benchHandler(b, pool)

	// Probe once (untimed) for the Tick the final request starts from.
	probe := newBenchWorld(b, h)
	var last int64
	requests := 0
	for {
		r := bmust(b, h, "POST", "/branches/"+probe.root+"/advance", obj{"runToTick": benchHorizon}, http.StatusOK)
		requests++
		if bnum(b, r, "headTick") >= benchHorizon {
			break
		}
		last = bnum(b, r, "headTick")
	}

	b.Run("from-zero", func(b *testing.B) {
		b.ReportMetric(float64(requests), "requests/op")
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			w := newBenchWorld(b, h)
			b.StartTimer()
			bAdvance(b, h, w.root, benchHorizon)
		}
	})
	b.Run("last-segment", func(b *testing.B) {
		b.ReportMetric(float64(last), "start-tick")
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			w := newBenchWorld(b, h)
			if last > 0 {
				bAdvance(b, h, w.root, last)
			}
			b.StartTimer()
			if code, raw := call(h, "POST", "/branches/"+w.root+"/advance", obj{"runToTick": benchHorizon}); code != http.StatusOK {
				b.Fatalf("advance = %d %.200s", code, raw)
			}
		}
	})
}

// BenchmarkAPITickFrames: GET /tick-frames for 120 Ticks (1..120) of a branch
// at the horizon (29.7). Warm and cold service.
func BenchmarkAPITickFrames(b *testing.B) {
	pool := benchPool(b)
	for _, cold := range []bool{false, true} {
		name := "warm"
		if cold {
			name = "cold"
		}
		b.Run(name, func(b *testing.B) {
			h := benchHandler(b, pool)
			w := newBenchWorld(b, h)
			bAdvance(b, h, w.root, benchHorizon)
			path := fmt.Sprintf("/branches/%s/tick-frames?from=1&to=%d", w.root, benchHorizon)
			r := bmust(b, h, "GET", path, nil, http.StatusOK) // warm-up and check
			if n := len(r["frames"].([]any)); n != benchHorizon {
				b.Fatalf("frames = %d", n)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if cold {
					b.StopTimer()
					h = benchHandler(b, pool)
					b.StartTimer()
				}
				if code, raw := call(h, "GET", path, nil); code != http.StatusOK {
					b.Fatalf("frames = %d %.200s", code, raw)
				}
			}
		})
	}
}
