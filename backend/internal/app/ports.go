package app

import (
	"context"
	"errors"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Identifiers are opaque strings (UUIDv4 in the PostgreSQL store).
type (
	WorldID  string
	BranchID string
)

// ManagerKind and Provenance are recorded metadata. The engine never reads
// them and the state hash excludes them (INV-15, INV-18).
type (
	ManagerKind string
	Provenance  string
)

// Manager kinds of a Branch.
const (
	ManagerHuman       ManagerKind = "human"
	ManagerAIRuleBased ManagerKind = "ai_rule_based"
	ManagerAILLM       ManagerKind = "ai_llm"
)

// Provenance of a recorded input.
const (
	ProvHuman                 Provenance = "human"
	ProvAIRuleBased           Provenance = "ai_rule_based"
	ProvAILLM                 Provenance = "ai_llm"
	ProvAIRuleBasedFallback   Provenance = "ai_rule_based_fallback"
	ProvScenario              Provenance = "scenario"
	ProvInjectedDisruptionLog Provenance = "injected"
)

// Repository errors. Stores wrap them with %w.
var (
	ErrNotFound                = errors.New("app: not found")
	ErrAlreadyExists           = errors.New("app: already exists")
	ErrDuplicateIdempotencyKey = errors.New("app: duplicate idempotency key")
	ErrSnapshotConflict        = errors.New("app: snapshot exists with a different hash")
)

// World is one business instance: canonical scenario, seed, engine version.
type World struct {
	ID            WorldID
	Name          string
	ScenarioHash  [32]byte
	ScenarioJSON  []byte // canonical bytes
	Seed          sim.Seed
	EngineVersion string
	CreatedAt     time.Time
}

// Branch is one timeline. The root has an empty ParentBranchID and ForkTick 0.
// HeadTick is a derived cache of the latest computed tick.
type Branch struct {
	ID             BranchID
	WorldID        WorldID
	ParentBranchID BranchID
	ForkTick       sim.Tick
	HeadTick       sim.Tick
	Label          string
	ManagerKind    ManagerKind
	CreatedAt      time.Time
}

// Metadata is non-engine information stored with an input. It is excluded
// from every state hash.
type Metadata struct {
	Provenance     Provenance
	IdempotencyKey string // empty means none
	Rationale      string // untrusted display text
	ModelName      string
}

// NewInput is what a caller appends. The store assigns Seq.
type NewInput struct {
	EffectiveTick sim.Tick
	Kind          sim.InputKind
	Payload       []byte // canonical: sim.EncodeCommand output for commands
	Metadata      Metadata
}

// InputRecord is one accepted, immutable input of a branch.
type InputRecord struct {
	BranchID      BranchID
	Seq           uint64 // per-branch, starts at 1, strictly increasing
	EffectiveTick sim.Tick
	Kind          sim.InputKind
	Payload       []byte
	Metadata      Metadata
}

// NewCommandInput builds a NewInput for a command using the canonical encoding.
func NewCommandInput(effective sim.Tick, c sim.Command, md Metadata) NewInput {
	return NewInput{EffectiveTick: effective, Kind: sim.InputCommand, Payload: sim.EncodeCommand(c), Metadata: md}
}

// Snapshot is a cached full state at a tick. It is trusted only when Hash
// equals the hash of the replayed state.
type Snapshot struct {
	BranchID BranchID
	Tick     sim.Tick
	Hash     [32]byte
	Blob     []byte
}

// WorldRepo stores worlds. Worlds are immutable once created.
type WorldRepo interface {
	CreateWorld(ctx context.Context, w World) error
	GetWorld(ctx context.Context, id WorldID) (World, error)
}

// BranchRepo stores branches. Only HeadTick, a derived cache, can change.
type BranchRepo interface {
	CreateBranch(ctx context.Context, b Branch) error
	GetBranch(ctx context.Context, id BranchID) (Branch, error)
	ListBranches(ctx context.Context, w WorldID) ([]Branch, error)
	CountBranches(ctx context.Context, w WorldID) (int, error)
	SetHeadTick(ctx context.Context, id BranchID, t sim.Tick) error
}

// InputRepo is the append-only input log. It has no update or delete method
// by design (INV-3).
type InputRepo interface {
	// AppendInput assigns the next per-branch Seq. If md.IdempotencyKey is
	// non-empty and already used on the branch, it returns the existing record
	// and an error wrapping ErrDuplicateIdempotencyKey, and stores nothing.
	AppendInput(ctx context.Context, b BranchID, in NewInput) (InputRecord, error)
	// InputsForBranch returns the inputs that govern the branch up to and
	// including tick upTo (by effective tick). For a forked branch this is the
	// ancestor chain: each ancestor contributes only inputs with
	// effective_tick <= the fork tick of the child below it. Order: effective
	// tick, then ancestor-first, then Seq.
	InputsForBranch(ctx context.Context, b BranchID, upTo sim.Tick) ([]InputRecord, error)
	FindByIdempotencyKey(ctx context.Context, b BranchID, key string) (InputRecord, bool, error)
}

// SnapshotRepo stores snapshot caches.
type SnapshotRepo interface {
	// SaveSnapshot is idempotent for an identical hash and returns
	// ErrSnapshotConflict for a different hash at the same (branch, tick).
	SaveSnapshot(ctx context.Context, s Snapshot) error
	LoadLatestSnapshot(ctx context.Context, b BranchID, atOrBefore sim.Tick) (Snapshot, bool, error)
}

// Store bundles all repositories; the in-memory and PostgreSQL stores both
// implement it and run the same contract suite.
type Store interface {
	WorldRepo
	BranchRepo
	InputRepo
	SnapshotRepo
}
