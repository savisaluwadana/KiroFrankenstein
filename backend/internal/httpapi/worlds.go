package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// demoWorldName is the World name when POST /demo has none.
const demoWorldName = "Black Friday Meltdown (demo)"

type healthDTO struct {
	Status        string `json:"status"`
	EngineVersion string `json:"engineVersion"`
}

func (s *Server) getHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthDTO{Status: "ok", EngineVersion: engineVersion()})
}

type createWorldRequest struct {
	Name        *string          `json:"name"`
	Seed        *int64           `json:"seed"`
	Scenario    json.RawMessage  `json:"scenario"`
	Overrides   map[string]int64 `json:"overrides"`
	ManagerKind string           `json:"managerKind"`
}

type createWorldResponse struct {
	World      worldDTO  `json:"world"`
	RootBranch branchDTO `json:"rootBranch"`
}

func (s *Server) createWorld(w http.ResponseWriter, r *http.Request) {
	var req createWorldRequest
	if err := decodeBody(w, r, &req, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("name", req.Name); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("seed", req.Seed); err != nil {
		s.fail(w, r, err)
		return
	}
	if *req.Seed < 0 || *req.Seed > MaxSeed {
		s.fail(w, r, badRequest("seed out of range", issue("seed", "must be between 0 and %d", int64(MaxSeed))))
		return
	}
	if req.ManagerKind != "" && !managerKindValid(req.ManagerKind) {
		s.fail(w, r, badRequest("unknown manager kind", issue("managerKind", "must be human, ai_rule_based or ai_llm")))
		return
	}
	scenario := []byte(req.Scenario)
	if len(scenario) == 0 || string(scenario) == "null" {
		scenario = s.flagship
	}
	world, root, err := s.svc.CreateWorld(r.Context(), app.CreateWorldRequest{
		Name: *req.Name, ScenarioJSON: scenario, Seed: sim.Seed(*req.Seed),
		Overrides: req.Overrides, Manager: app.ManagerKind(req.ManagerKind),
	})
	if err != nil {
		s.fail(w, r, withIssueField(err, "name"))
		return
	}
	dto, err := s.worldDTO(r, world.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, createWorldResponse{World: dto, RootBranch: branchToDTO(root)})
}

func (s *Server) worldDTO(r *http.Request, id app.WorldID) (worldDTO, error) {
	world, sc, err := s.svc.GetWorld(r.Context(), id)
	if err != nil {
		return worldDTO{}, err
	}
	dto := worldToDTO(world, sc)
	st, err := s.svc.LLMStatus(r.Context(), id)
	if err != nil {
		return worldDTO{}, err
	}
	dto.LLMAvailable, dto.LLMModel = st.Available, st.Model
	dto.LLMCallsMade, dto.LLMCallsRemaining = st.CallsMade, st.CallsRemaining
	dto.LLMTokensUsed, dto.LLMTokensRemaining = st.TokensUsed, st.TokensRemaining
	return dto, nil
}

func (s *Server) getWorld(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "worldId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dto, err := s.worldDTO(r, app.WorldID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if dto.Warnings, err = s.worldWarnings(r, app.WorldID(id)); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) worldWarnings(r *http.Request, id app.WorldID) ([]warningDTO, error) {
	ws, err := s.svc.WorldEngineWarnings(r.Context(), id)
	if err != nil {
		return nil, err
	}
	return warningsToDTO(ws), nil
}

func (s *Server) listBranches(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "worldId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	bs, err := s.svc.ListBranches(r.Context(), app.WorldID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	warn, err := s.worldWarnings(r, app.WorldID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := struct {
		Branches []branchDTO  `json:"branches"`
		Warnings []warningDTO `json:"warnings,omitempty"`
	}{Branches: make([]branchDTO, 0, len(bs)), Warnings: warn}
	for _, b := range bs {
		out.Branches = append(out.Branches, branchToDTO(b))
	}
	writeJSON(w, http.StatusOK, out)
}

type createDemoRequest struct {
	Name *string `json:"name"`
}

type demoCommandDTO struct {
	Kind     string `json:"kind"`
	SKUIndex int64  `json:"skuIndex"`
	SKUID    string `json:"skuId"`
	Role     string `json:"role"`
	Qty      int64  `json:"qty"`
}

type demoPlanDTO struct {
	Scenario   string           `json:"scenario"`
	Seed       uint64           `json:"seed"`
	CrisisTick int64            `json:"crisisTick"`
	ForkTick   int64            `json:"forkTick"`
	Commands   []demoCommandDTO `json:"commands"`
	Reasons    []string         `json:"reasons"`
}

type demoResponse struct {
	World      worldDTO    `json:"world"`
	RootBranch branchDTO   `json:"rootBranch"`
	Plan       demoPlanDTO `json:"plan"`
}

// demoPlan derives the plan once per process: it depends only on the flagship
// scenario and the demo seed.
func (s *Server) demoPlan() (demoplan.Plan, error) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	if s.plan != nil {
		return *s.plan, nil
	}
	sc, err := sim.ParseScenario(s.flagship)
	if err != nil {
		return demoplan.Plan{}, err
	}
	p, err := demoplan.Derive(*sc, demoplan.DefaultSeed)
	if err != nil {
		return demoplan.Plan{}, err
	}
	s.plan = &p
	return p, nil
}

func (s *Server) createDemo(w http.ResponseWriter, r *http.Request) {
	var req createDemoRequest
	if err := decodeBody(w, r, &req, true); err != nil {
		s.fail(w, r, err)
		return
	}
	name := demoWorldName
	if req.Name != nil {
		name = *req.Name
	}
	plan, err := s.demoPlan()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	world, root, err := s.svc.CreateWorld(r.Context(), app.CreateWorldRequest{
		Name: name, ScenarioJSON: s.flagship, Seed: demoplan.DefaultSeed,
	})
	if err != nil {
		s.fail(w, r, withIssueField(err, "name"))
		return
	}
	dto, err := s.worldDTO(r, world.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pd := demoPlanDTO{
		Scenario: plan.Scenario, Seed: plan.Seed, CrisisTick: plan.CrisisTick, ForkTick: plan.ForkTick,
		Commands: make([]demoCommandDTO, 0, len(plan.Commands)), Reasons: append([]string{}, plan.Reasons...),
	}
	for _, c := range plan.Commands {
		pd.Commands = append(pd.Commands, demoCommandDTO(c))
	}
	writeJSON(w, http.StatusCreated, demoResponse{World: dto, RootBranch: branchToDTO(root), Plan: pd})
}
