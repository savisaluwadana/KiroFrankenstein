package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
)

// dpJSONPaths collects the key paths of a decoded JSON value; arrays are
// collapsed to "[]" and empty arrays contribute only their own path.
func dpJSONPaths(prefix string, v any, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			dpJSONPaths(prefix+"."+k, c, out)
		}
	case []any:
		out[prefix+"[]"] = true
		for _, c := range x {
			dpJSONPaths(prefix+"[]", c, out)
		}
	default:
		out[prefix] = true
	}
}

// dpObsShape returns the sorted field paths of an Observation. Collections
// whose contents legitimately depend on a manager's decisions (open orders,
// purchase orders) are excluded; everything else must have the same shape.
func dpObsShape(t *rapid.T, o sim.Observation) string {
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "OpenOrders")
	delete(m, "PurchaseOrders")
	paths := map[string]bool{}
	dpJSONPaths("", m, paths)
	keys := make([]string, 0, len(paths))
	for p := range paths {
		// A Disruption list is exogenous but may be empty or not at a tick;
		// emptiness is compared by value below, not by shape.
		if strings.HasPrefix(p, ".Disruptions") || strings.HasPrefix(p, ".SKUs[]") && strings.Contains(p, ".Suppliers") {
			p = strings.SplitN(p, "[]", 2)[0]
		}
		keys = append(keys, p)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// dpExogenousView is the part of an Observation that must be identical in both
// Duel Branches at every tick: window, budget, supplier terms and Disruptions.
type dpExogenousView struct {
	Tick        sim.Tick
	Window      sim.ObsWindow
	Suppliers   [][]sim.ObsSupplier
	StaticSKUs  []string
	Disruptions []sim.ObsDisruption
}

func dpExo(o sim.Observation) dpExogenousView {
	v := dpExogenousView{Tick: o.Tick, Window: o.Window}
	for _, s := range o.SKUs {
		v.Suppliers = append(v.Suppliers, s.Suppliers)
		v.StaticSKUs = append(v.StaticSKUs, fmt.Sprintf("%d/%s/%d/%d/%d", s.SKU, s.ID, s.ReferencePriceCents, s.MinPriceCents, s.MaxPriceCents))
	}
	for _, d := range o.Disruptions {
		d.InputSeq = 0 // per-branch Input sequence, not part of the Disruption
		v.Disruptions = append(v.Disruptions, d)
	}
	return v
}

func dpStream(t *rapid.T, st sim.State, name sim.StreamName) sim.StreamState {
	for _, s := range st.Streams {
		if s.Name == name {
			return s.State
		}
	}
	t.Fatalf("stream %q missing", name)
	return 0
}

type dpArrival struct {
	Tick sim.Tick
	SKU  int64
	Qty  int64
}

func dpArrivals(t *rapid.T, ctx context.Context, svc *app.BranchService, id app.BranchID, horizon sim.Tick) []dpArrival {
	var out []dpArrival
	for from := sim.Tick(1); from <= horizon; from += app.MaxJournalTicks {
		to := min(from+app.MaxJournalTicks-1, horizon)
		entries, err := svc.JournalRange(ctx, id, from, to)
		if err != nil {
			t.Fatalf("journal %d..%d: %v", from, to, err)
		}
		for _, e := range entries {
			if e.Outcome.Kind == sim.OutcomeDemand {
				out = append(out, dpArrival{e.Tick, int64(e.Outcome.SKU), e.Outcome.Qty})
			}
		}
	}
	return out
}

// TestPropDuelParity: in a Duel both Branches get Decision_Windows at the same
// Ticks with the same budget, Observation shape, supplier lead times, the same
// Potential_Arrivals, the same exogenous random streams and the same active
// Disruptions, whatever either Manager decides.
//
// Feature: parallel-simulation-mvp, Property 21: Duel parity and exogenous fairness
// Validates: Requirements 18.4, 18.5 (INV-11, INV-12)
func TestPropDuelParity(t *testing.T) {
	base, data := piScenario(t)
	ctx := context.Background()
	var cases, diverged, aiDecided, injected, injectedActive int

	rapid.Check(t, func(t *rapid.T) {
		horizon := rapid.Int64Range(24, 36).Draw(t, "horizon")
		sc := base
		sc.HorizonTicks = horizon
		plan := piDrawPlan(t, sc)

		svc := app.NewBranchService(memory.New(), nil, nil)
		_, root, err := svc.CreateWorld(ctx, app.CreateWorldRequest{
			Name: "dp", ScenarioJSON: data, Seed: sim.Seed(rapid.Uint64().Draw(t, "seed")),
			Overrides: map[string]int64{"horizonTicks": horizon},
		})
		if err != nil {
			t.Fatal(err)
		}
		ds := app.NewDuelService(svc, nil)
		du, err := ds.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: 0})
		if err != nil {
			t.Fatal(err)
		}

		inject := func(head sim.Tick) {
			if int64(head)+1 >= horizon {
				return
			}
			req := app.DuelInjectRequest{
				StartTick:     head + 1 + sim.Tick(rapid.Int64Range(0, horizon-int64(head)-2).Draw(t, "injectAt")),
				DurationTicks: rapid.Int64Range(1, 6).Draw(t, "injectDur"), Target: -1,
			}
			switch rapid.IntRange(0, 2).Draw(t, "injectKind") {
			case 0:
				req.Kind, req.MagnitudeBps = sim.DisruptionDemandSurge, sim.Bps(rapid.Int64Range(11000, 20000).Draw(t, "mag"))
			case 1:
				req.Kind, req.MagnitudeBps = sim.DisruptionWorkforceAbsence, sim.Bps(rapid.Int64Range(500, 5000).Draw(t, "mag"))
			default:
				req.Kind, req.Role = sim.DisruptionSupplierOutage, uint8(sim.SupplierPrimary)
			}
			if _, err := ds.InjectDisruption(ctx, du.ID, req); err != nil {
				t.Fatalf("inject at head %d: %v", head, err)
			}
			injected++
		}
		for i, n := 0, rapid.IntRange(0, 2).Draw(t, "initialInjections"); i < n; i++ {
			inject(0)
		}

		for guard := 0; ; guard++ {
			if guard > 200 {
				t.Fatal("duel did not reach the horizon")
			}
			s, err := ds.DuelStatus(ctx, du.ID)
			if err != nil {
				t.Fatal(err)
			}
			if s.HumanHead != s.AIHead {
				t.Fatalf("heads differ: %d vs %d", s.HumanHead, s.AIHead)
			}
			if int64(s.HumanHead) >= horizon {
				break
			}
			if s.WaitingOnHuman {
				if cmds, ok := plan[s.HumanHead]; ok {
					if _, err := ds.SubmitHuman(ctx, du.ID, app.SubmitRequest{WindowTick: s.HumanHead, Commands: cmds}); err != nil {
						t.Fatalf("submit at %d: %v", s.HumanHead, err)
					}
				}
				if rapid.IntRange(0, 5).Draw(t, "midInject") == 0 {
					inject(s.HumanHead)
				}
				if err := ds.EndHumanWindow(ctx, du.ID, s.HumanHead, nil); err != nil {
					t.Fatalf("end window %d: %v", s.HumanHead, err)
				}
			}
			if _, err := ds.AdvanceDuel(ctx, du.ID, nil); err != nil {
				t.Fatalf("advance from %d: %v", s.HumanHead, err)
			}
		}

		hid, aid := du.HumanBranchID, du.AIBranchID
		hz := sim.Tick(horizon)

		// Decision windows, budget, lead times, Observation shape, Disruptions.
		for tick := sim.Tick(0); tick < hz; tick++ {
			ho, err := svc.ObservationAt(ctx, hid, tick)
			if err != nil {
				t.Fatal(err)
			}
			ao, err := svc.ObservationAt(ctx, aid, tick)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dpExo(ho), dpExo(ao)) {
				t.Fatalf("tick %d: exogenous observation fields differ:\nhuman %+v\nai    %+v", tick, dpExo(ho), dpExo(ao))
			}
			if hs, as := dpObsShape(t, ho), dpObsShape(t, ao); hs != as {
				t.Fatalf("tick %d: observation shape differs:\n%s\n%s", tick, hs, as)
			}
		}

		// Active Disruptions and exogenous stream states at every tick.
		for tick := sim.Tick(0); tick <= hz; tick++ {
			hf, err := svc.FrameAt(ctx, hid, tick)
			if err != nil {
				t.Fatal(err)
			}
			af, err := svc.FrameAt(ctx, aid, tick)
			if err != nil {
				t.Fatal(err)
			}
			strip := func(in []sim.DisruptionFrame) []sim.DisruptionFrame {
				out := append([]sim.DisruptionFrame(nil), in...)
				for i := range out {
					out[i].InputSeq = 0
				}
				return out
			}
			hd, ad := strip(hf.Disruptions), strip(af.Disruptions)
			if !reflect.DeepEqual(hd, ad) {
				t.Fatalf("tick %d: active disruptions differ: %+v vs %+v", tick, hd, ad)
			}
			if len(hd) > 0 {
				injectedActive++
			}
			hst, err := svc.StateAt(ctx, hid, tick)
			if err != nil {
				t.Fatal(err)
			}
			ast, err := svc.StateAt(ctx, aid, tick)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []sim.StreamName{sim.StreamDemand, sim.StreamDisruption} {
				if h, a := dpStream(t, hst, name), dpStream(t, ast, name); h != a {
					t.Fatalf("tick %d: %s stream state differs: %d vs %d", tick, name, h, a)
				}
			}
		}

		// Potential_Arrivals per SKU per tick.
		if h, a := dpArrivals(t, ctx, svc, hid, hz), dpArrivals(t, ctx, svc, aid, hz); !reflect.DeepEqual(h, a) {
			t.Fatalf("potential arrivals differ:\nhuman %v\nai    %v", h, a)
		}

		// Non-vacuity bookkeeping.
		cases++
		hh, err := svc.HashAt(ctx, hid, hz)
		if err != nil {
			t.Fatal(err)
		}
		ah, err := svc.HashAt(ctx, aid, hz)
		if err != nil {
			t.Fatal(err)
		}
		if hh != ah {
			diverged++
		}
		recs, err := svc.DecisionRecords(ctx, aid)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			if r.AcceptedCount > 0 {
				aiDecided++
				break
			}
		}
	})

	t.Logf("cases=%d diverged=%d aiAcceptedCommands=%d injections=%d disruptionTicks=%d", cases, diverged, aiDecided, injected, injectedActive)
	if cases == 0 {
		t.Skip("rapid ran no cases")
	}
	if diverged == 0 {
		t.Fatal("vacuous: human and AI branches never diverged")
	}
	if aiDecided == 0 {
		t.Fatal("vacuous: the AI never had a command accepted")
	}
	if injected == 0 || injectedActive == 0 {
		t.Fatal("vacuous: no injected disruption occurred")
	}
}
