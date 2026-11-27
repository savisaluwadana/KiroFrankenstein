package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
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

type sseFixture struct {
	srv     *httptest.Server
	svc     *app.BranchService
	branch  app.BranchID
	horizon sim.Tick
}

func newSSEFixture(t *testing.T) *sseFixture {
	t.Helper()
	svc := app.NewBranchService(memory.New(), nil, nil)
	api, err := httpapi.New(httpapi.Options{
		Service: svc, FlagshipScenario: scenarios.BlackFriday, RatePerSecond: 100000, RateBurst: 100000,
		SSEPollInterval: 10 * time.Millisecond, SSEHeartbeat: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	_, b, err := svc.CreateWorld(context.Background(), app.CreateWorldRequest{Name: "sse", ScenarioJSON: scenarios.BlackFriday, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	_, sc, err := svc.GetWorld(context.Background(), b.WorldID)
	if err != nil {
		t.Fatal(err)
	}
	return &sseFixture{srv: srv, svc: svc, branch: b.ID, horizon: sim.Tick(sc.HorizonTicks)}
}

// advanceTo advances until the head is at least t (windows are ended by the
// next Advance call).
func (f *sseFixture) advanceTo(t *testing.T, to sim.Tick) {
	t.Helper()
	for {
		head, err := f.svc.HeadTick(context.Background(), f.branch)
		if err != nil {
			t.Fatal(err)
		}
		if head >= to {
			return
		}
		if _, err := f.svc.Advance(context.Background(), app.AdvanceRequest{Branch: f.branch}); err != nil {
			t.Fatal(err)
		}
	}
}

type sseEvent struct {
	ID    string
	Event string
	Data  string
}

type sseConn struct {
	resp   *http.Response
	sc     *bufio.Scanner
	cancel context.CancelFunc
}

func (f *sseFixture) open(t *testing.T, query string, hdr ...string) *sseConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	req, err := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/v1/branches/"+string(f.branch)+"/progress"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	c := &sseConn{resp: resp, sc: bufio.NewScanner(resp.Body), cancel: cancel}
	c.sc.Buffer(make([]byte, 1<<16), 1<<22)
	t.Cleanup(c.close)
	return c
}

func (c *sseConn) close() {
	c.cancel()
	_ = c.resp.Body.Close()
}

// next returns the next real event, skipping heartbeat comments.
func (c *sseConn) next(t *testing.T) sseEvent {
	t.Helper()
	var ev sseEvent
	have := false
	for c.sc.Scan() {
		line := c.sc.Text()
		switch {
		case line == "":
			if have {
				return ev
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "id: "):
			ev.ID, have = line[4:], true
		case strings.HasPrefix(line, "event: "):
			ev.Event, have = line[7:], true
		case strings.HasPrefix(line, "data: "):
			ev.Data, have = line[6:], true
		}
	}
	t.Fatalf("stream ended early: %v", c.sc.Err())
	return ev
}

func (c *sseConn) ended() bool { return !c.sc.Scan() }

func tickOf(t *testing.T, ev sseEvent) int64 {
	t.Helper()
	var d struct {
		Tick int64 `json:"tick"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &d); err != nil {
		t.Fatal(err)
	}
	return d.Tick
}

func TestProgressStreamHeadersAndReconnect(t *testing.T) {
	f := newSSEFixture(t)
	f.advanceTo(t, 12)
	c := f.open(t, "", "Last-Event-ID", "10")
	if ct := c.resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	if c.resp.Header.Get("Cache-Control") != "no-cache, no-transform" || c.resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers %v", c.resp.Header)
	}
	ev := c.next(t)
	if ev.Event != "tick" || ev.ID != "11" || tickOf(t, ev) != 11 {
		t.Fatalf("first event = %+v, want tick 11", ev)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &d); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"stateHash", "metrics", "skus", "markers"} {
		if _, ok := d[k]; !ok {
			t.Fatalf("tick payload lacks %q", k)
		}
	}
	// fromTick works without Last-Event-ID
	c2 := f.open(t, "?fromTick=3")
	if ev := c2.next(t); ev.ID != "3" {
		t.Fatalf("fromTick first event id = %s, want 3", ev.ID)
	}
}

func TestProgressStreamNeverAdvances(t *testing.T) {
	f := newSSEFixture(t)
	c := f.open(t, "")
	if ev := c.next(t); ev.ID != "0" {
		t.Fatalf("first id %s", ev.ID)
	}
	time.Sleep(150 * time.Millisecond)
	if head, _ := f.svc.HeadTick(context.Background(), f.branch); head != 0 {
		t.Fatalf("stream advanced the branch to %d", head)
	}
}

func TestProgressStreamLiveAdvance(t *testing.T) {
	f := newSSEFixture(t)
	c := f.open(t, "")
	for {
		ev := c.next(t)
		if ev.Event == "tick" {
			break // tick 0 (and a possible window event follow)
		}
	}
	f.advanceTo(t, 1)
	head, _ := f.svc.HeadTick(context.Background(), f.branch)
	seen := map[int64]bool{}
	for !seen[int64(head)] {
		ev := c.next(t)
		if ev.Event == "tick" {
			n, _ := strconv.ParseInt(ev.ID, 10, 64)
			seen[n] = true
		}
	}
}

func TestProgressStreamDecisionWindowEvent(t *testing.T) {
	f := newSSEFixture(t)
	f.advanceTo(t, 1)
	head, _ := f.svc.HeadTick(context.Background(), f.branch)
	c := f.open(t, "?fromTick="+fmt.Sprint(head))
	first := c.next(t)
	if first.Event != "tick" || first.ID != fmt.Sprint(head) {
		t.Fatalf("first = %+v", first)
	}
	win := c.next(t)
	if win.Event != "decision_window" || win.ID != fmt.Sprint(head) {
		t.Fatalf("second = %+v, want decision_window at %d", win, head)
	}
	var d struct {
		Tick    int64    `json:"tick"`
		Reasons []string `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(win.Data), &d); err != nil {
		t.Fatal(err)
	}
	if d.Tick != int64(head) || len(d.Reasons) == 0 {
		t.Fatalf("window data %+v", d)
	}
}

func TestProgressStreamCompletedAtHorizon(t *testing.T) {
	f := newSSEFixture(t)
	f.advanceTo(t, f.horizon)
	c := f.open(t, "?fromTick="+fmt.Sprint(f.horizon-1))
	var lastTickHash string
	for {
		ev := c.next(t)
		if ev.Event == "tick" && ev.ID == fmt.Sprint(f.horizon) {
			var d struct {
				StateHash string `json:"stateHash"`
			}
			_ = json.Unmarshal([]byte(ev.Data), &d)
			lastTickHash = d.StateHash
		}
		if ev.Event == "completed" {
			var d struct {
				Tick      int64  `json:"tick"`
				StateHash string `json:"stateHash"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err != nil {
				t.Fatal(err)
			}
			if d.Tick != int64(f.horizon) || d.StateHash != lastTickHash || lastTickHash == "" {
				t.Fatalf("completed %+v, horizon hash %q", d, lastTickHash)
			}
			break
		}
	}
	if !c.ended() {
		t.Fatal("stream continued after completed")
	}
	// reconnect after completion restates it
	c2 := f.open(t, "", "Last-Event-ID", fmt.Sprint(f.horizon))
	if ev := c2.next(t); ev.Event != "completed" {
		t.Fatalf("reconnect event = %+v", ev)
	}
}

func TestProgressStreamPerIPLimit(t *testing.T) {
	f := newSSEFixture(t)
	var conns []*sseConn
	for i := 0; i < 4; i++ {
		c := f.open(t, "")
		c.next(t)
		conns = append(conns, c)
	}
	resp, err := http.Get(f.srv.URL + "/api/v1/branches/" + string(f.branch) + "/progress")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" ||
		resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("fifth connection: %d %v", resp.StatusCode, resp.Header)
	}
	var p map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil || p["type"] != "urn:parallel:problem:rate-limited" {
		t.Fatalf("problem %v err %v", p, err)
	}
	conns[0].close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := http.Get(f.srv.URL + "/api/v1/branches/" + string(f.branch) + "/progress?fromTick=0")
		if err != nil {
			t.Fatal(err)
		}
		code := resp.StatusCode
		resp.Body.Close()
		if code == http.StatusOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not freed, last status %d", code)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProgressStreamNoGoroutineLeak(t *testing.T) {
	f := newSSEFixture(t)
	f.advanceTo(t, 2)
	http.DefaultClient.CloseIdleConnections()
	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 3; i++ {
		c := f.open(t, "")
		c.next(t)
		c.close()
	}
	http.DefaultClient.CloseIdleConnections()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if n := runtime.NumGoroutine(); n <= before+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines: before %d, after %d", before, runtime.NumGoroutine())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProgressStreamBadInput(t *testing.T) {
	f := newSSEFixture(t)
	for _, tc := range []struct {
		path string
		hdr  string
		want int
	}{
		{"/branches/not-a-uuid/progress", "", 404},
		{"/branches/" + string(f.branch) + "/progress", "abc", 400},
		{"/branches/" + string(f.branch) + "/progress?fromTick=-1", "", 422},
		{"/branches/" + string(f.branch) + "/progress?fromTick=100000", "", 422},
	} {
		req, _ := http.NewRequest("GET", f.srv.URL+"/api/v1"+tc.path, nil)
		if tc.hdr != "" {
			req.Header.Set("Last-Event-ID", tc.hdr)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s (%q): status %d, want %d", tc.path, tc.hdr, resp.StatusCode, tc.want)
		}
	}
}
