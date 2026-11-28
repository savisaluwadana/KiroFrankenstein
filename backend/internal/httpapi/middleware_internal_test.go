package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/scenarios"
)

func newInternalServer(t *testing.T, mod func(*Options)) *Server {
	t.Helper()
	o := Options{
		Service: app.NewBranchService(memory.New(), nil, nil), FlagshipScenario: scenarios.BlackFriday,
		RatePerSecond: 100000, RateBurst: 100000, MaxConcurrentSimulations: 1, SimQueueWait: 30 * time.Millisecond,
	}
	if mod != nil {
		mod(&o)
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The expensive routes share one slot pool; with every slot taken they answer
// 503 with Retry-After after the queue wait, while cheap reads still work.
func TestConcurrencyLimit(t *testing.T) {
	s := newInternalServer(t, nil)
	h := s.Handler()
	// A real world and duel: the write-token check runs before the slot queue,
	// so unknown ids would be refused with 404 before ever waiting.
	var demo struct {
		WriteToken string `json:"writeToken"`
		RootBranch struct {
			ID string `json:"id"`
		} `json:"rootBranch"`
	}
	rec0 := httptest.NewRecorder()
	h.ServeHTTP(rec0, httptest.NewRequest("POST", "/api/v1/demo", nil))
	if err := json.Unmarshal(rec0.Body.Bytes(), &demo); err != nil || demo.WriteToken == "" {
		t.Fatalf("demo: %v %s", err, rec0.Body.String())
	}
	id := demo.RootBranch.ID
	recD := httptest.NewRecorder()
	h.ServeHTTP(recD, httptest.NewRequest("POST", "/api/v1/duels", nil))
	var duel struct {
		WriteToken string `json:"writeToken"`
		Duel       struct {
			ID string `json:"id"`
		} `json:"duel"`
	}
	if err := json.Unmarshal(recD.Body.Bytes(), &duel); err != nil || duel.Duel.ID == "" {
		t.Fatalf("duel: %v %s", err, recD.Body.String())
	}
	tokenOf := map[string]string{id: demo.WriteToken, duel.Duel.ID: duel.WriteToken}
	s.sims <- struct{}{} // occupy the only slot
	heavy := [][2]string{
		{"POST", "/api/v1/branches/" + id + "/advance"},
		{"POST", "/api/v1/branches/" + id + "/fork"},
		{"GET", "/api/v1/branches/" + id + "/journal"},
		{"GET", "/api/v1/branches/" + id + "/ledger/summary"},
		{"POST", "/api/v1/diffs"},
		{"POST", "/api/v1/duels"},
		{"POST", "/api/v1/duels/" + duel.Duel.ID + "/advance"},
	}
	for _, e := range heavy {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(e[0], e[1], strings.NewReader("{}"))
		for k, t := range tokenOf {
			if strings.Contains(e[1], k) {
				req.Header.Set(WriteTokenHeader, t)
			}
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" ||
			rec.Header().Get("Content-Type") != "application/problem+json" ||
			!strings.Contains(rec.Body.String(), "urn:parallel:problem:unavailable") {
			t.Errorf("%s %s = %d %v %s", e[0], e[1], rec.Code, rec.Header(), rec.Body.String())
		}
	}
	for _, e := range [][2]string{{"GET", "/api/v1/health"}, {"GET", "/api/v1/branches/" + id}, {"GET", "/healthz"}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(e[0], e[1], nil))
		if rec.Code == http.StatusServiceUnavailable {
			t.Errorf("%s %s must not take a simulation slot", e[0], e[1])
		}
	}
	<-s.sims // free the slot: the same request is admitted
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/branches/"+id+"/advance", strings.NewReader(`{"ticks":1}`))
	req.Header.Set(WriteTokenHeader, demo.WriteToken)
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusServiceUnavailable {
		t.Errorf("request refused with a free slot: %s", rec.Body.String())
	}
}

// A waiting request is admitted as soon as the running one finishes, and the
// number of requests inside the section never exceeds the limit.
func TestConcurrencyLimitAdmits(t *testing.T) {
	s := newInternalServer(t, func(o *Options) { o.MaxConcurrentSimulations = 2; o.SimQueueWait = 2 * time.Second })
	var mu sync.Mutex
	inside, peak := 0, 0
	h := s.simSlot(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inside++
		peak = max(peak, inside)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inside--
		mu.Unlock()
	}))
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
			codes[i] = rec.Code
		}()
	}
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Errorf("request %d = %d", i, c)
		}
	}
	if peak > 2 {
		t.Errorf("peak concurrency %d exceeds the limit 2", peak)
	}
}

func TestRequestTimeout(t *testing.T) {
	s := newInternalServer(t, func(o *Options) { o.RequestTimeout = 40 * time.Millisecond })
	h := s.withTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			s.fail(w, r, r.Context().Err())
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "urn:parallel:problem:unavailable") {
		t.Fatalf("timeout response = %d %s", rec.Code, rec.Body.String())
	}
	if time.Since(start) > time.Second {
		t.Error("deadline not applied")
	}
}

// BeginShutdown ends an open progress stream.
func TestShutdownEndsStreams(t *testing.T) {
	s := newInternalServer(t, func(o *Options) { o.MaxConcurrentSimulations = 8 })
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/demo", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		RootBranch struct {
			ID string `json:"id"`
		} `json:"rootBranch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/branches/"+created.RootBranch.ID+"/progress?fromTick=99", nil)
	// fromTick 99 is in range but unreached: the stream stays open.
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	if got.StatusCode != 200 {
		t.Fatalf("status %d", got.StatusCode)
	}
	if cc := got.Header.Get("Cache-Control"); cc != "no-cache, no-transform" || got.Header.Get("X-Accel-Buffering") != "no" ||
		got.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("SSE headers wrong: %v", got.Header)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := got.Body.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	s.BeginShutdown()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream still open after BeginShutdown")
	}
}
