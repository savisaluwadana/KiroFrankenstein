package postgres_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/postgres"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/storetest"
)

const envURL = "PARALLEL_TEST_DATABASE_URL"

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv(envURL)
	if url == "" {
		t.Skipf("%s not set; skipping PostgreSQL store tests (see README, Testing)", envURL)
	}
	pool, err := postgres.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// idMap gives each test a private namespace: the contract suite uses short
// names ("w1", "b1"); PostgreSQL needs UUIDs, and the input log cannot be
// truncated, so every name maps to a fresh random-per-run UUID. Unique IDs per
// run replace cleanup.
type idMap struct {
	nonce [16]byte
	mu    sync.Mutex
	back  map[string]string
}

func newIDMap() *idMap {
	m := &idMap{back: map[string]string{}}
	if _, err := rand.Read(m.nonce[:]); err != nil {
		panic(err)
	}
	return m
}

func (m *idMap) to(name string) string {
	if name == "" {
		return ""
	}
	h := sha256.Sum256(append(m.nonce[:], name...))
	h[6] = h[6]&0x0f | 0x40 // version 4 shape
	h[8] = h[8]&0x3f | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
	m.mu.Lock()
	m.back[id] = name
	m.mu.Unlock()
	return id
}

func (m *idMap) from(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.back[id]; ok {
		return n
	}
	return id
}

// mapped adapts the short-name contract suite to UUID identifiers.
type mapped struct {
	s app.Store
	m *idMap
}

func (a mapped) CreateWorld(ctx context.Context, w app.World) error {
	w.ID = app.WorldID(a.m.to(string(w.ID)))
	return a.s.CreateWorld(ctx, w)
}
func (a mapped) GetWorld(ctx context.Context, id app.WorldID) (app.World, error) {
	w, err := a.s.GetWorld(ctx, app.WorldID(a.m.to(string(id))))
	w.ID = app.WorldID(a.m.from(string(w.ID)))
	return w, err
}
func (a mapped) out(b app.Branch) app.Branch {
	b.ID = app.BranchID(a.m.from(string(b.ID)))
	b.WorldID = app.WorldID(a.m.from(string(b.WorldID)))
	b.ParentBranchID = app.BranchID(a.m.from(string(b.ParentBranchID)))
	return b
}
func (a mapped) CreateBranch(ctx context.Context, b app.Branch) error {
	b.ID = app.BranchID(a.m.to(string(b.ID)))
	b.WorldID = app.WorldID(a.m.to(string(b.WorldID)))
	b.ParentBranchID = app.BranchID(a.m.to(string(b.ParentBranchID)))
	return a.s.CreateBranch(ctx, b)
}
func (a mapped) GetBranch(ctx context.Context, id app.BranchID) (app.Branch, error) {
	b, err := a.s.GetBranch(ctx, app.BranchID(a.m.to(string(id))))
	return a.out(b), err
}
func (a mapped) ListBranches(ctx context.Context, w app.WorldID) ([]app.Branch, error) {
	l, err := a.s.ListBranches(ctx, app.WorldID(a.m.to(string(w))))
	for i := range l {
		l[i] = a.out(l[i])
	}
	return l, err
}
func (a mapped) CountBranches(ctx context.Context, w app.WorldID) (int, error) {
	return a.s.CountBranches(ctx, app.WorldID(a.m.to(string(w))))
}
func (a mapped) SetHeadTick(ctx context.Context, id app.BranchID, t sim.Tick) error {
	return a.s.SetHeadTick(ctx, app.BranchID(a.m.to(string(id))), t)
}
func (a mapped) rec(r app.InputRecord) app.InputRecord {
	r.BranchID = app.BranchID(a.m.from(string(r.BranchID)))
	return r
}
func (a mapped) AppendInput(ctx context.Context, id app.BranchID, in app.NewInput) (app.InputRecord, error) {
	r, err := a.s.AppendInput(ctx, app.BranchID(a.m.to(string(id))), in)
	return a.rec(r), err
}
func (a mapped) InputsForBranch(ctx context.Context, id app.BranchID, upTo sim.Tick) ([]app.InputRecord, error) {
	rs, err := a.s.InputsForBranch(ctx, app.BranchID(a.m.to(string(id))), upTo)
	for i := range rs {
		rs[i] = a.rec(rs[i])
	}
	return rs, err
}
func (a mapped) FindByIdempotencyKey(ctx context.Context, id app.BranchID, key string) (app.InputRecord, bool, error) {
	r, ok, err := a.s.FindByIdempotencyKey(ctx, app.BranchID(a.m.to(string(id))), key)
	return a.rec(r), ok, err
}
func (a mapped) SaveSnapshot(ctx context.Context, s app.Snapshot) error {
	s.BranchID = app.BranchID(a.m.to(string(s.BranchID)))
	return a.s.SaveSnapshot(ctx, s)
}
func (a mapped) LoadLatestSnapshot(ctx context.Context, id app.BranchID, t sim.Tick) (app.Snapshot, bool, error) {
	s, ok, err := a.s.LoadLatestSnapshot(ctx, app.BranchID(a.m.to(string(id))), t)
	s.BranchID = id
	return s, ok, err
}

func TestContract(t *testing.T) {
	pool := testPool(t)
	storetest.Run(t, func(*testing.T) app.Store {
		return mapped{s: postgres.New(pool), m: newIDMap()}
	})
}

func TestSeedRoundTrip(t *testing.T) {
	pool := testPool(t)
	s := postgres.New(pool)
	ctx := context.Background()
	for _, seed := range []sim.Seed{0, 1, 1 << 63, 1<<63 + 12345, math.MaxUint64} {
		m := newIDMap()
		id := app.WorldID(m.to("w"))
		if err := s.CreateWorld(ctx, app.World{ID: id, Name: "seed", ScenarioJSON: []byte(`{}`), Seed: seed, EngineVersion: "1"}); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		w, err := s.GetWorld(ctx, id)
		if err != nil || w.Seed != seed {
			t.Fatalf("seed %d round-tripped to %d (err %v)", seed, w.Seed, err)
		}
	}
}

func TestRuntimeRoleCannotMutateInputLog(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE input_log SET payload = payload`); err == nil {
		t.Fatal("runtime role must not UPDATE input_log")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM input_log`); err == nil {
		t.Fatal("runtime role must not DELETE from input_log")
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE should_fail (x int)`); err == nil {
		t.Fatal("runtime role must not run DDL")
	}
}

func TestMalformedIDsAreNotFound(t *testing.T) {
	s := postgres.New(testPool(t))
	if _, err := s.GetBranch(context.Background(), "not-a-uuid"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
