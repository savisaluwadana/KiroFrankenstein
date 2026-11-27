package app_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/policy/llm"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// failTransport counts every outbound request and fails the test on it.
type failTransport struct {
	t    *testing.T
	hits atomic.Int64
}

func (f *failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.hits.Add(1)
	f.t.Errorf("unexpected network request: %s %s", r.Method, r.URL.Redacted())
	return nil, fmt.Errorf("network access is forbidden in this test")
}

// TestNoLLMConfiguration covers INV-13: with no LLM variables set, creating,
// advancing, commanding, forking, rewinding, replaying, running the AI
// managers and exporting never touch the network.
func TestNoLLMConfiguration(t *testing.T) {
	for _, k := range []string{"LLM_BASE_URL", "LLM_API_KEY", "LLM_MODEL"} {
		t.Setenv(k, "") // empty is unset for llm.LoadConfig; restored by the test
		_ = os.Unsetenv(k)
	}
	ft := &failTransport{t: t}
	old := http.DefaultTransport
	http.DefaultTransport = ft
	t.Cleanup(func() { http.DefaultTransport = old })

	// Built exactly like cmd/parallel-api/main.go without LLM configuration.
	cfg, err := llm.LoadConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Available() {
		t.Fatal("LLM configuration is available with the variables unset")
	}
	var opts []app.Option // llmOptions returns no options when unavailable
	ctx := context.Background()
	st := memory.New()
	svc := app.NewBranchService(st, nil, nil, opts...)
	if ok, model := svc.LLMAvailable(); ok || model != "" {
		t.Fatalf("LLMAvailable = %v %q", ok, model)
	}

	// Human world: create, advance, submit, advance, fork, rewind reads.
	_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "h", ScenarioJSON: bfJSON(t), Seed: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitCommands(ctx, app.SubmitRequest{Branch: root.ID, WindowTick: 0,
		Commands: []sim.Command{{Kind: sim.CmdScheduleOvertime, Qty: 1}}}); err != nil {
		t.Fatal(err)
	}
	runTo(t, svc, root.ID, 10)
	child, err := svc.ForkBranch(ctx, root.ID, 8, "fork", app.ManagerHuman)
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, svc, child.ID, 20)
	for _, tick := range []sim.Tick{0, 5, 8, 10} {
		if _, err := svc.HashAt(ctx, root.ID, tick); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.StateAt(ctx, root.ID, tick); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.MetricsAt(ctx, root.ID, tick); err != nil {
			t.Fatal(err)
		}
	}

	// Rule-based AI branch over the whole horizon.
	_, ai, err := svc.CreateWorld(ctx, app.CreateWorldRequest{Name: "ai", ScenarioJSON: bfJSON(t), Seed: 2026, Manager: app.ManagerAIRuleBased})
	if err != nil {
		t.Fatal(err)
	}
	if res := runTo(t, svc, ai.ID, 1000); res.Paused != app.PauseHorizon {
		t.Fatalf("AI run paused %q", res.Paused)
	}
	recs, err := svc.DecisionRecords(ctx, ai.ID)
	if err != nil || len(recs) == 0 {
		t.Fatalf("AI records %d err %v", len(recs), err)
	}
	for _, r := range recs {
		if r.Provenance != app.ProvAIRuleBased {
			t.Fatalf("record = %+v", r)
		}
	}

	// An ai_llm branch without configuration records the fallback, no request.
	llmB, err := svc.ForkBranch(ctx, root.ID, 8, "llm", app.ManagerAILLM)
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, svc, llmB.ID, 40)
	lr, err := svc.DecisionRecords(ctx, llmB.ID)
	if err != nil || len(lr) == 0 {
		t.Fatalf("llm-kind records %d err %v", len(lr), err)
	}
	for _, r := range lr {
		if r.Provenance != app.ProvAIRuleBasedFallback || r.FallbackReason != app.FallbackLLMUnavailable {
			t.Fatalf("llm-kind record = %+v", r)
		}
	}

	// Export, then replay from a cold service and compare every hash.
	if _, err := svc.ExportInputs(ctx, ai.ID); err != nil {
		t.Fatal(err)
	}
	cold := app.NewBranchService(st, nil, nil, opts...)
	for _, c := range []struct {
		id   app.BranchID
		last sim.Tick
	}{{root.ID, 10}, {child.ID, 20}, {ai.ID, 120}, {llmB.ID, 40}} {
		for tick := sim.Tick(0); tick <= c.last; tick++ {
			want, err := svc.HashAt(ctx, c.id, tick)
			if err != nil {
				t.Fatal(err)
			}
			got, err := cold.HashAt(ctx, c.id, tick)
			if err != nil || got != want {
				t.Fatalf("branch %s tick %d: cold hash %x err %v, want %x", c.id, tick, got, err, want)
			}
		}
		if _, err := cold.ExportInputs(ctx, c.id); err != nil {
			t.Fatal(err)
		}
	}

	// Duel with the Rule_Based_Manager over the whole horizon (INV-13).
	ds := app.NewDuelService(svc, nil)
	du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: 0, AIKind: app.ManagerAIRuleBased})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		s, err := ds.DuelStatus(ctx, du.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.HumanHead >= s.Horizon && s.AIHead >= s.Horizon {
			break
		}
		if s.WaitingOnHuman {
			if err := ds.EndHumanWindow(ctx, du.ID, s.HumanHead, nil); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ds.AdvanceDuel(ctx, du.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	score, err := ds.DuelScore(ctx, du.ID)
	if err != nil || score.Provisional || score.ComparisonTick != 120 {
		t.Fatalf("duel score = %+v err %v", score, err)
	}
	if recs, err := svc.DecisionRecords(ctx, du.AIBranchID); err != nil || len(recs) == 0 {
		t.Fatalf("duel AI records %d err %v", len(recs), err)
	}

	if n := ft.hits.Load(); n != 0 {
		t.Fatalf("%d network requests were made", n)
	}
}
