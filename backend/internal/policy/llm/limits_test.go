package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

func TestLoadConfigGlobalLimits(t *testing.T) {
	c, err := LoadConfig(env(nil))
	if err != nil || c.MaxPromptBytes != 32768 || c.GlobalMaxCalls != 2000 || c.GlobalMaxTokens != 5000000 || c.MaxConcurrentRequests != 4 {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for _, tc := range []struct{ k, v string }{
		{EnvMaxPromptBytes, "1023"}, {EnvMaxPromptBytes, "262145"},
		{EnvGlobalMaxCalls, "0"}, {EnvGlobalMaxCalls, "1000001"},
		{EnvGlobalMaxTokens, "999"}, {EnvGlobalMaxTokens, "1000000001"},
		{EnvMaxConcurrentCalls, "0"}, {EnvMaxConcurrentCalls, "65"},
		{EnvGlobalWindow, "week"},
	} {
		_, err := LoadConfig(env(map[string]string{tc.k: tc.v}))
		if !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), tc.k) {
			t.Errorf("%s=%s: err = %v", tc.k, tc.v, err)
		}
	}
	if _, err := LoadConfig(env(map[string]string{EnvGlobalWindow: "day", EnvMaxConcurrentCalls: "64"})); err != nil {
		t.Fatal(err)
	}
}

func bigObs(n int) sim.Observation {
	o := testObs()
	for i := 0; i < n; i++ {
		o.SKUs = append(o.SKUs, sim.ObsSKU{SKU: int64(i + 1), ID: "S", OnHand: 1})
	}
	return o
}

func TestReservationGrowsWithPrompt(t *testing.T) {
	cfg := testConfig("x")
	small, _ := UserPrompt(testObs(), 2)
	large, _ := UserPrompt(bigObs(100), 2)
	sys := SystemPrompt(2)
	rs, rl := cfg.ReservationTokens(sys, small), cfg.ReservationTokens(sys, large)
	if rl <= rs || rs != EstimatePromptTokens(sys, small)+cfg.MaxTokensPerCall {
		t.Fatalf("reservations small=%d large=%d", rs, rl)
	}
	if got := EstimatePromptTokens("abcd", "e"); got != 2 { // ceil(5/3)
		t.Fatalf("estimate = %d", got)
	}
}

func TestOvershootAccountedAndBlocksFurtherCalls(t *testing.T) {
	var reqs atomic.Int32
	m, _ := newManager(t, func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		_, _ = io.WriteString(w, envelope(`{"commands":[],"rationale":"x"}`, 9000)) // far above the reservation
	})
	if _, err := m.Propose(context.Background(), testObs(), 2); err != nil {
		t.Fatal(err)
	}
	if got := m.Usage().TotalTokens; got != 9000 {
		t.Fatalf("actual must be accounted, got %d", got)
	}
	if m.tracker.Overshoot() <= 0 || m.client.Global().Overshoot() != m.tracker.Overshoot() {
		t.Fatalf("overshoot world=%d global=%d", m.tracker.Overshoot(), m.client.Global().Overshoot())
	}
	_, err := m.Propose(context.Background(), testObs(), 2) // 9000 > 4000 limit
	if FallbackReason(err) != "budget_exhausted" || reqs.Load() != 1 {
		t.Fatalf("err=%v requests=%d", err, reqs.Load())
	}
}

func TestPromptTooLargeRejectedBeforeNetwork(t *testing.T) {
	var reqs atomic.Int32
	m, _ := newManager(t, func(w http.ResponseWriter, r *http.Request) { reqs.Add(1) })
	m.client.cfg.MaxPromptBytes = MinMaxPromptBytes
	_, err := m.Propose(context.Background(), bigObsCtx(), 2)
	if FallbackReason(err) != "budget_exhausted" || !errors.Is(err, ErrPromptTooLarge) || reqs.Load() != 0 {
		t.Fatalf("err=%v requests=%d", err, reqs.Load())
	}
	if u := m.Usage(); u.Calls != 0 || u.TotalTokens != 0 {
		t.Fatalf("nothing may be charged: %+v", u)
	}
	if _, _, err := m.client.Complete(context.Background(), strings.Repeat("a", 2000), ""); !errors.Is(err, ErrPromptTooLarge) {
		t.Fatalf("Complete must also refuse: %v", err)
	}
}

func bigObsCtx() sim.Observation { return bigObs(200) }

func TestConcurrencySemaphore(t *testing.T) {
	var cur, peak atomic.Int32
	m, _ := newManager(t, func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
		_, _ = io.WriteString(w, envelope(`{"commands":[]}`, 20))
	})
	cfg := m.cfg
	cfg.MaxConcurrentRequests, cfg.MaxCallsPerWorld, cfg.MaxTotalTokensPerWorld = 2, 50, 1000000
	cfg.RequestTimeout = 5 * time.Second
	c, _ := NewClient(cfg, nil)
	m.client, m.tracker = c, NewTracker(cfg)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Propose(context.Background(), testObs(), 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 2 {
		t.Fatalf("peak in flight = %d, want 2", peak.Load())
	}
}

func TestGlobalTrackerDailyReset(t *testing.T) {
	now := time.Date(2026, 12, 1, 23, 59, 0, 0, time.UTC)
	cfg := testConfig("x")
	cfg.GlobalMaxCalls, cfg.GlobalMaxTokens = 2, 5000
	g := NewGlobalTracker(cfg, func() time.Time { return now })
	for i := 0; i < 2; i++ {
		if err := g.Reserve(100); err != nil {
			t.Fatal(err)
		}
		g.Settle(100, Usage{PromptTokens: 50, CompletionTokens: 50})
	}
	if err := g.Reserve(1); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute) // next UTC day
	if c, tk := g.Remaining(); c != 2 || tk != 5000 {
		t.Fatalf("after rollover remaining = %d, %d", c, tk)
	}
}

func TestKeyNotInRedactedConfigOrErrors(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:1").withDefaults()
	if strings.Contains(cfg.String(), testKey) || strings.Contains(ErrPromptTooLarge.Error()+ErrSaturated.Error(), testKey) {
		t.Fatal("key leaked")
	}
}

var _ = sim.Observation{}
