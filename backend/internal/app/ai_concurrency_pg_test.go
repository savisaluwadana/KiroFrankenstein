package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/postgres"
)

// The AI/LLM concurrency scenarios of ai_concurrency_test.go over the real
// PostgreSQL store. Skipped unless PARALLEL_TEST_DATABASE_URL is set (the
// database must be migrated). Rows are append-only, so every test creates its
// own worlds with random UUIDs instead of cleaning up.

const pgEnvURL = "PARALLEL_TEST_DATABASE_URL"

func pgPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv(pgEnvURL)
	if url == "" {
		t.Skipf("%s not set; skipping PostgreSQL AI concurrency tests", pgEnvURL)
	}
	pool, err := postgres.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	// The global (per-day) usage is a sum over every decision record of the
	// database, so tests that assert it exactly must not overlap with another
	// test process writing usage: serialise them with an advisory lock.
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, pgAdvisoryKey); err != nil {
		conn.Release()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, pgAdvisoryKey)
		conn.Release()
	})
	return pool
}

const pgAdvisoryKey = 7710042

func pgSvc(t *testing.T, pool *pgxpool.Pool, cfg llm.Config) *app.BranchService {
	t.Helper()
	return app.NewBranchService(postgres.New(pool), nil, nil, withLLM(t, cfg))
}

func pgWorld(t *testing.T, svc *app.BranchService, mgr app.ManagerKind) (app.World, app.Branch) {
	t.Helper()
	return createWorld(t, svc, mgr)
}

func countRows(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// decisionRows counts decision_records rows and the distinct windows among
// them with a query that does not go through the service.
func decisionRows(t *testing.T, pool *pgxpool.Pool, id app.BranchID) (rows, windows int) {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM decision_records WHERE branch_id = $1`, string(id)),
		countRows(t, pool, `SELECT count(DISTINCT window_tick) FROM decision_records WHERE branch_id = $1`, string(id))
}

// duplicateAIInputs counts AI inputs of the branch that repeat an effective
// tick and payload, i.e. a decision committed twice.
func duplicateAIInputs(t *testing.T, pool *pgxpool.Pool, id app.BranchID) int {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM (
		SELECT 1 FROM input_log WHERE branch_id = $1 AND provenance IN ('ai_llm','ai_rule_based','ai_rule_based_fallback')
		GROUP BY effective_tick, kind, payload HAVING count(*) > 1) d`, string(id))
}

func aiInputCount(t *testing.T, pool *pgxpool.Pool, id app.BranchID) int {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM input_log WHERE branch_id = $1 AND provenance IN ('ai_llm','ai_rule_based','ai_rule_based_fallback')`, string(id))
}

func humanInputCount(t *testing.T, pool *pgxpool.Pool, id app.BranchID) int {
	t.Helper()
	return countRows(t, pool, `SELECT count(*) FROM input_log WHERE branch_id = $1 AND provenance = 'human'`, string(id))
}

// pgHash replays the branch from the stored inputs in a fresh service without
// an LLM: the clean replay.
func pgHash(t *testing.T, pool *pgxpool.Pool, id app.BranchID, tick sim.Tick) [32]byte {
	t.Helper()
	h, err := app.NewBranchService(postgres.New(pool), nil, nil).HashAt(context.Background(), id, tick)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// pgCfg is llmCfg with a request timeout that a held request survives: forks
// over PostgreSQL arrive at the stub slower than over memory.
func pgCfg(url string) llm.Config {
	c := llmCfg(url)
	c.RequestTimeout = 30 * time.Second
	c.MaxConcurrentRequests = 64 // the default of 4 would make held requests saturate
	return c
}

func dayStart() time.Time {
	n := time.Now().UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func usageToday(t *testing.T, pool *pgxpool.Pool) app.LLMUsageSum {
	t.Helper()
	u, err := postgres.New(pool).LLMUsageSince(context.Background(), dayStart())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// (a) Concurrent Advances of one LLM branch make one request per window and
// one record per (branch, window).
func TestAIConcurrencyPGSingleFlight(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)
	stub := newGateStub(t, always(okReply(validProposal)))
	svc := pgSvc(t, pool, pgCfg(stub.srv.URL))
	_, root := pgWorld(t, svc, app.ManagerAILLM)
	stub.waitArrived(t, 1) // window 0

	release := stub.hold()
	defer release()
	const n = 6
	outs := make([]<-chan advOut, n)
	outs[0] = advanceAsync(svc, ctx, root.ID)
	stub.waitArrived(t, 1) // window 4 in flight
	for i := 1; i < n; i++ {
		outs[i] = advanceAsync(svc, ctx, root.ID)
	}
	joined := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { joined <- svc.EnsureDecidedForTest(ctx, root.ID) }()
	}
	time.Sleep(150 * time.Millisecond)
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("provider requests while window 4 is in flight = %d, want 2", got)
	}
	release()
	for i := 0; i < 3; i++ {
		if err := recv(t, joined, "joined call"); err != nil {
			t.Fatal(err)
		}
	}
	heads := map[sim.Tick]bool{}
	for i, ch := range outs {
		r := recv(t, ch, fmt.Sprintf("advance %d", i))
		if r.err != nil {
			t.Fatalf("advance %d: %v", i, r.err)
		}
		heads[r.res.HeadTick] = true
	}
	// Six advances queue up: windows 4, 8, ..., 24, one request each, so 7 in all.
	if len(heads) != n {
		t.Fatalf("heads = %v, want %d distinct window stops", heads, n)
	}
	rows, windows := decisionRows(t, pool, root.ID)
	if rows != n+1 || windows != rows || stub.calls.Load() != int64(rows) {
		t.Fatalf("decision rows %d (windows %d), provider requests %d, want %d each", rows, windows, stub.calls.Load(), n+1)
	}
	if d := duplicateAIInputs(t, pool, root.ID); d != 0 {
		t.Fatalf("%d duplicated AI inputs", d)
	}
	if got := aiInputCount(t, pool, root.ID); got != rows {
		t.Fatalf("ai inputs %d, want one per window (%d)", got, rows)
	}
	if svc.InflightForTest() != 0 {
		t.Fatal("flight left registered")
	}
	head := sim.Tick(4 * n)
	h, _ := svc.HashAt(ctx, root.ID, head)
	if h != pgHash(t, pool, root.ID, head) {
		t.Fatal("hash differs from a clean replay")
	}
}

// (b) A stale answer commits nothing, is charged, and the hash equals a clean replay.
func TestAIConcurrencyPGStaleDecision(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)
	stub := newGateStub(t, always(okReply(validProposal)))
	svc := pgSvc(t, pool, pgCfg(stub.srv.URL))
	w, root := pgWorld(t, svc, app.ManagerAILLM)
	stub.waitArrived(t, 1)
	release := stub.hold()
	defer release()
	out := advanceAsync(svc, ctx, root.ID)
	stub.waitArrived(t, 1)
	humanSubmit(t, svc, root.ID, 4, 1) // the branch changes under the request
	release()
	stub.waitArrived(t, 1) // the retry
	r := recv(t, out, "advance")
	if r.err != nil || r.res.HeadTick != 4 {
		t.Fatalf("advance = %+v err %v", r.res, r.err)
	}
	if svc.StaleDecisions() != 1 || stub.calls.Load() != 3 {
		t.Fatalf("stale %d, requests %d", svc.StaleDecisions(), stub.calls.Load())
	}
	recs, _ := svc.DecisionRecords(ctx, root.ID)
	var rec *app.DecisionRecord
	for i := range recs {
		if recs[i].WindowTick == 4 {
			rec = &recs[i]
		}
	}
	if rec == nil || rec.LLMCallsCharged != 2 || rec.Provenance != app.ProvAILLM {
		t.Fatalf("window 4 record = %+v", rec)
	}
	if rows, windows := decisionRows(t, pool, root.ID); rows != 2 || windows != 2 {
		t.Fatalf("rows %d windows %d", rows, windows)
	}
	// Exactly the retry's commands (one per AI window: 0 and 4), no stale copy.
	if got := aiInputCount(t, pool, root.ID); got != 2 {
		t.Fatalf("ai inputs = %d, want 2", got)
	}
	if got := humanInputCount(t, pool, root.ID); got != 1 {
		t.Fatalf("human inputs = %d", got)
	}
	s, _ := svc.LLMStatus(ctx, w.ID)
	if s.CallsMade != 3 {
		t.Fatalf("calls made %d, want 3", s.CallsMade)
	}
	if u := rec.PromptTokens + rec.CompletionTokens; u != 2*150 {
		t.Fatalf("window 4 tokens %d, want the two answers charged (300)", u)
	}
	h, _ := svc.HashAt(ctx, root.ID, 4)
	if h != pgHash(t, pool, root.ID, 4) {
		t.Fatal("hash differs from a clean replay")
	}
}

// (c) A cancelled caller does not lose or double-charge the decision.
func TestAIConcurrencyPGCancelledCaller(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)
	stub := newGateStub(t, always(okReply(validProposal)))
	svc := pgSvc(t, pool, pgCfg(stub.srv.URL))
	w, root := pgWorld(t, svc, app.ManagerAILLM)
	stub.waitArrived(t, 1)
	runtime.GC()
	before := runtime.NumGoroutine()

	release := stub.hold()
	defer release()
	cctx, cancel := context.WithCancel(ctx)
	out := advanceAsync(svc, cctx, root.ID)
	stub.waitArrived(t, 1)
	cancel()
	if r := recv(t, out, "cancelled advance"); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", r.err)
	}
	release()
	deadline := time.Now().Add(5 * time.Second)
	for svc.InflightForTest() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("flight never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rows, windows := decisionRows(t, pool, root.ID); rows != 2 || windows != 2 {
		t.Fatalf("rows %d windows %d, want 2 (windows 0 and 4)", rows, windows)
	}
	r, err := svc.Advance(ctx, app.AdvanceRequest{Branch: root.ID})
	if err != nil || r.HeadTick != 8 {
		t.Fatalf("advance = %+v %v", r, err)
	}
	if got := stub.calls.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
	if s, _ := svc.LLMStatus(ctx, w.ID); s.CallsMade != 3 {
		t.Fatalf("calls made = %d", s.CallsMade)
	}
	if d := duplicateAIInputs(t, pool, root.ID); d != 0 {
		t.Fatalf("%d duplicated AI inputs", d)
	}
	deadline = time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+4 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	// Allowance covers the HTTP and pgx connection pools.
	if n := runtime.NumGoroutine(); n > before+4 {
		t.Fatalf("goroutines: %d before, %d after", before, n)
	}
}

// (d) Per-world and global caps are exact under concurrent windows over three
// worlds; counters rebuild identically after a restart; replay asks nothing.
func TestAIConcurrencyPGCapsRestartReplay(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)

	t.Run("per-world caps", func(t *testing.T) {
		stub := newGateStub(t, always(stubReply{content: validProposal, prompt: 60, completion: 40}))
		cfg := pgCfg(stub.srv.URL)
		cfg.MaxCallsPerWorld = 2
		cfg.GlobalMaxCalls = 1_000_000
		cfg.GlobalMaxTokens = 1_000_000_000
		svc := pgSvc(t, pool, cfg)
		baseline := usageToday(t, pool)

		type wb struct {
			w app.World
			b app.Branch
		}
		var worlds []wb
		for i := 0; i < 3; i++ {
			w, b := pgWorld(t, svc, app.ManagerHuman)
			worlds = append(worlds, wb{w, b})
		}
		release := stub.hold()
		defer release()
		const n = 10
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			parent := worlds[i%3].b.ID
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.ForkBranch(ctx, parent, 0, fmt.Sprintf("ai-%d", i), app.ManagerAILLM)
				errs <- err
			}()
		}
		stub.waitArrived(t, 6)
		time.Sleep(200 * time.Millisecond)
		release()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := stub.calls.Load(); got != 6 {
			t.Fatalf("provider requests = %d, want 6 (2 per world)", got)
		}
		for _, x := range worlds {
			s, _ := svc.LLMStatus(ctx, x.w.ID)
			if s.CallsMade != 2 || s.TokensUsed != 200 {
				t.Fatalf("world %s status = %+v, want 2 calls and 200 tokens", x.w.ID, s)
			}
			// Persisted usage columns agree.
			if got := countRows(t, pool, `SELECT COALESCE(sum(d.llm_calls_charged),0) FROM decision_records d JOIN branches b ON b.id = d.branch_id WHERE b.world_id = $1`, string(x.w.ID)); got != 2 {
				t.Fatalf("persisted calls of world %s = %d", x.w.ID, got)
			}
			if got := countRows(t, pool, `SELECT COALESCE(sum(d.prompt_tokens + d.completion_tokens),0) FROM decision_records d JOIN branches b ON b.id = d.branch_id WHERE b.world_id = $1`, string(x.w.ID)); got != 200 {
				t.Fatalf("persisted tokens of world %s = %d", x.w.ID, got)
			}
		}
		after := usageToday(t, pool)
		if after.Calls-baseline.Calls != 6 || after.PromptTokens-baseline.PromptTokens != 360 || after.CompletionTokens-baseline.CompletionTokens != 240 {
			t.Fatalf("global usage grew by %+v, want 6 calls, 360+240 tokens", app.LLMUsageSum{
				Calls: after.Calls - baseline.Calls, PromptTokens: after.PromptTokens - baseline.PromptTokens, CompletionTokens: after.CompletionTokens - baseline.CompletionTokens})
		}

		// Restart: a new service over the same database sees the same counters.
		svc2 := pgSvc(t, pool, cfg)
		for _, x := range worlds {
			a, _ := svc.LLMStatus(ctx, x.w.ID)
			b, err := svc2.LLMStatus(ctx, x.w.ID)
			if err != nil || a != b {
				t.Fatalf("after restart %+v err %v, before %+v", b, err, a)
			}
		}
		c1, t1, _, _ := svc.LLMGlobalStatus(ctx)
		c2, t2, _, err := svc2.LLMGlobalStatus(ctx)
		if err != nil || c1 != c2 || t1 != t2 {
			t.Fatalf("global remaining after restart %d/%d err %v, before %d/%d", c2, t2, err, c1, t1)
		}

		// Replay by a cold service over the same database: no provider request, equal hashes.
		before := stub.calls.Load()
		cold := pgSvc(t, pool, cfg)
		bs, _ := svc.ListBranches(ctx, worlds[0].w.ID)
		for _, b := range bs {
			want, err := svc.HashAt(ctx, b.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := cold.HashAt(ctx, b.ID, 0)
			if err != nil || got != want {
				t.Fatalf("cold hash of %s: %v (equal %v)", b.ID, err, got == want)
			}
		}
		if stub.calls.Load() != before {
			t.Fatal("replay made provider requests")
		}
	})

	t.Run("global caps", func(t *testing.T) {
		stub := newGateStub(t, always(stubReply{content: validProposal, prompt: 60, completion: 40}))
		base := usageToday(t, pool)
		cfg := pgCfg(stub.srv.URL)
		cfg.MaxCallsPerWorld = 100
		cfg.GlobalMaxCalls = base.Calls + 4
		cfg.GlobalMaxTokens = 1_000_000_000
		svc := pgSvc(t, pool, cfg)

		var roots []app.Branch
		for i := 0; i < 3; i++ {
			_, b := pgWorld(t, svc, app.ManagerHuman)
			roots = append(roots, b)
		}
		release := stub.hold()
		defer release()
		const n = 10
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			parent := roots[i%3].ID
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.ForkBranch(ctx, parent, 0, fmt.Sprintf("g-%d", i), app.ManagerAILLM)
				errs <- err
			}()
		}
		stub.waitArrived(t, 4)
		time.Sleep(200 * time.Millisecond)
		release()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := stub.calls.Load(); got != 4 {
			t.Fatalf("provider requests = %d, want exactly the 4 left of the global day cap", got)
		}
		if u := usageToday(t, pool); u.Calls-base.Calls != 4 {
			t.Fatalf("persisted global calls grew by %d, want 4", u.Calls-base.Calls)
		}
		c, _, ok, err := svc.LLMGlobalStatus(ctx)
		if err != nil || !ok || c != 0 {
			t.Fatalf("global calls remaining = %d ok %v err %v", c, ok, err)
		}
		// Restart: the exhausted day cap is rebuilt from the records.
		svc2 := pgSvc(t, pool, cfg)
		if c2, _, _, err := svc2.LLMGlobalStatus(ctx); err != nil || c2 != 0 {
			t.Fatalf("after restart remaining = %d err %v", c2, err)
		}
		// Fallbacks recorded with the budget reason, never a silent drop.
		var fallbacks int
		for _, r := range roots {
			bs, _ := svc.ListBranches(ctx, r.WorldID)
			for _, b := range bs[1:] {
				recs, _ := svc.DecisionRecords(ctx, b.ID)
				if len(recs) != 1 {
					t.Fatalf("branch %s has %d records", b.Label, len(recs))
				}
				if recs[0].Provenance == app.ProvAIRuleBasedFallback {
					if recs[0].FallbackReason != "budget_exhausted" || recs[0].LLMCallsCharged != 0 {
						t.Fatalf("record = %+v", recs[0])
					}
					fallbacks++
				}
			}
		}
		if fallbacks != n-4 {
			t.Fatalf("budget_exhausted fallbacks = %d, want %d", fallbacks, n-4)
		}
	})
}

// (e) A duel stays in lockstep and human reads are not blocked while the AI's
// request is slow, over PostgreSQL.
func TestAIConcurrencyPGDuelLockstep(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)
	stub := newGateStub(t, always(okReply(validProposal)))
	st := postgres.New(pool)
	svc := app.NewBranchService(st, nil, nil, withLLM(t, pgCfg(stub.srv.URL)))
	ds := app.NewDuelService(svc, st)
	_, root := pgWorld(t, svc, app.ManagerHuman)
	du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: 0, AIKind: app.ManagerAILLM})
	if err != nil {
		t.Fatal(err)
	}
	stub.waitArrived(t, 1)
	if err := ds.EndHumanWindow(ctx, du.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	release := stub.hold()
	defer release()
	type res struct {
		r   app.AdvanceDuelResult
		err error
	}
	adv := make(chan res, 1)
	go func() { r, err := ds.AdvanceDuel(ctx, du.ID, nil); adv <- res{r, err} }()
	stub.waitArrived(t, 1)

	within(t, 500*time.Millisecond, "duel status", func() error {
		s, err := ds.DuelStatus(ctx, du.ID)
		if err == nil && (s.HumanHead != 4 || s.AIHead != 4) {
			err = fmt.Errorf("heads %d/%d, want 4/4 (neither side runs ahead)", s.HumanHead, s.AIHead)
		}
		return err
	})
	within(t, 500*time.Millisecond, "human reads", func() error {
		if _, err := svc.StateAt(ctx, du.HumanBranchID, 4); err != nil {
			return err
		}
		if _, err := svc.HashAt(ctx, du.HumanBranchID, 4); err != nil {
			return err
		}
		_, err := svc.DecisionRecords(ctx, du.HumanBranchID)
		return err
	})
	within(t, 500*time.Millisecond, "duel score", func() error { _, err := ds.DuelScore(ctx, du.ID); return err })
	second := make(chan res, 1)
	go func() { r, err := ds.AdvanceDuel(ctx, du.ID, nil); second <- res{r, err} }()
	select {
	case r := <-second:
		t.Fatalf("a second advance returned during the request: %+v %v", r.r, r.err)
	case <-time.After(150 * time.Millisecond):
	}
	release()
	r := recv(t, adv, "advance")
	if r.err != nil || r.r.Human.HeadTick != 4 || r.r.AI.HeadTick != 4 {
		t.Fatalf("advance = %+v err %v", r.r, r.err)
	}
	if r := recv(t, second, "second advance"); !errors.Is(r.err, app.ErrDuelWindowOpen) {
		t.Fatalf("second advance = %+v err %v, want the human's window to hold the duel", r.r, r.err)
	}
	for tk := sim.Tick(0); tk <= 4; tk++ {
		hs, _ := svc.StateAt(ctx, du.HumanBranchID, tk)
		as, _ := svc.StateAt(ctx, du.AIBranchID, tk)
		if hs.Orders.Created != as.Orders.Created {
			t.Fatalf("tick %d: orders created differ (%d vs %d)", tk, hs.Orders.Created, as.Orders.Created)
		}
	}
	if rows, windows := decisionRows(t, pool, du.AIBranchID); rows != 2 || windows != 2 {
		t.Fatalf("AI rows %d windows %d, want windows 0 and 4", rows, windows)
	}
	if rows, _ := decisionRows(t, pool, du.HumanBranchID); rows != 0 {
		t.Fatalf("human branch has %d decision records", rows)
	}
}

// rivalRepo is the store as seen by a service that loses a race: just before
// its first AppendDecision, another writer (another process) commits a record
// for the same window.
type rivalRepo struct {
	*postgres.Store
	rival app.DecisionRecord
	armed atomic.Bool
	rErr  atomic.Pointer[error]
}

func (r *rivalRepo) AppendDecision(ctx context.Context, d app.DecisionRecord) error {
	if r.armed.CompareAndSwap(true, false) {
		rec := r.rival
		rec.BranchID, rec.WindowTick = d.BranchID, d.WindowTick
		if err := r.Store.AppendDecision(ctx, rec); err != nil {
			r.rErr.Store(&err)
		}
	}
	return r.Store.AppendDecision(ctx, d)
}

// (f) A unique-constraint race on decision_records is handled: the loser's
// record is discarded and nothing is duplicated.
func TestAIConcurrencyPGDecisionUniqueRace(t *testing.T) {
	ctx := context.Background()
	pool := pgPool(t)

	t.Run("rival record committed first", func(t *testing.T) {
		stub := newGateStub(t, always(okReply(validProposal)))
		repo := &rivalRepo{Store: postgres.New(pool), rival: app.DecisionRecord{
			ManagerKind: app.ManagerAILLM, Provenance: app.ProvAILLM, Model: testModel, Rationale: "rival",
			CreatedAt: time.Now(), LLMCallsCharged: 1, PromptTokens: 1, CompletionTokens: 1,
		}}
		svc := app.NewBranchService(repo, nil, nil, withLLM(t, pgCfg(stub.srv.URL)))
		_, root := pgWorld(t, svc, app.ManagerHuman)
		repo.armed.Store(true)
		child, err := svc.ForkBranch(ctx, root.ID, 0, "ai", app.ManagerAILLM)
		if err != nil {
			t.Fatalf("losing the race must not fail the fork: %v", err)
		}
		if e := repo.rErr.Load(); e != nil {
			t.Fatalf("rival insert failed: %v", *e)
		}
		if rows, windows := decisionRows(t, pool, child.ID); rows != 1 || windows != 1 {
			t.Fatalf("rows %d windows %d, want 1 (the rival's)", rows, windows)
		}
		recs, _ := svc.DecisionRecords(ctx, child.ID)
		if len(recs) != 1 || recs[0].Rationale != "rival" {
			t.Fatalf("records = %+v, the first committed record must stand", recs)
		}
		if d := duplicateAIInputs(t, pool, child.ID); d != 0 {
			t.Fatalf("%d duplicated AI inputs", d)
		}
		// The branch still advances and replays cleanly.
		if r, err := svc.Advance(ctx, app.AdvanceRequest{Branch: child.ID}); err != nil || r.HeadTick != 4 {
			t.Fatalf("advance = %+v %v", r, err)
		}
		h, _ := svc.HashAt(ctx, child.ID, 4)
		if h != pgHash(t, pool, child.ID, 4) {
			t.Fatal("hash differs from a clean replay")
		}
	})

	t.Run("two services race for one window", func(t *testing.T) {
		stub := newGateStub(t, always(okReply(validProposal)))
		cfg := pgCfg(stub.srv.URL)
		svcA, svcB := pgSvc(t, pool, cfg), pgSvc(t, pool, cfg)
		_, root := pgWorld(t, svcA, app.ManagerAILLM)
		stub.waitArrived(t, 1)

		release := stub.hold()
		defer release()
		a := advanceAsync(svcA, ctx, root.ID)
		b := advanceAsync(svcB, ctx, root.ID)
		stub.waitArrived(t, 2) // both decide window 4 independently
		release()
		ra, rb := recv(t, a, "service A"), recv(t, b, "service B")
		heads := map[sim.Tick]bool{}
		for _, r := range []advOut{ra, rb} {
			// A service whose cached head is behind the other's is refused with
			// ErrStaleHead (optimistic concurrency); nothing is written for it.
			if r.err != nil && !errors.Is(r.err, app.ErrStaleHead) {
				t.Fatalf("advance: %v", r.err)
			}
			heads[r.res.HeadTick] = true
		}
		// The service that lost the race discards its commands and, having
		// found the window decided, takes its step on to window 8.
		rows, windows := decisionRows(t, pool, root.ID)
		if rows < 2 || rows > 4 || windows != rows {
			t.Fatalf("rows %d windows %d, want one per window", rows, windows)
		}
		if d := duplicateAIInputs(t, pool, root.ID); d != 0 {
			t.Fatalf("%d duplicated AI inputs", d)
		}
		if got := aiInputCount(t, pool, root.ID); got != rows {
			t.Fatalf("ai inputs = %d, want one per window (%d)", got, rows)
		}
		head := max(ra.res.HeadTick, rb.res.HeadTick)
		hA, _ := svcA.HashAt(ctx, root.ID, head)
		hB, _ := svcB.HashAt(ctx, root.ID, head)
		if hA != hB || hA != pgHash(t, pool, root.ID, head) {
			t.Fatal("services disagree with a clean replay")
		}
	})
}
