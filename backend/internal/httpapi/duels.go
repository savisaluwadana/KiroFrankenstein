package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/demoplan"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

const defaultDuelName = "Duel"

// guardDuelBranch rejects a direct mutation of a Branch that belongs to a Duel
// (409 duel-branch). Both sides must move together through the Duel endpoints,
// or the lockstep, the parity of decision windows and the shared Comparison_Tick
// could be bypassed. Reads are not guarded.
func (s *Server) guardDuelBranch(r *http.Request, id string) error {
	du, ok, err := s.duels.DuelOfBranch(r.Context(), app.BranchID(id))
	if err != nil {
		return err
	}
	if ok {
		return newProblem("duel-branch", fmt.Sprintf(
			"this branch belongs to duel %s and moves only with it; use the /duels/%s endpoints", du.ID, du.ID))
	}
	return nil
}

func duelPathID(r *http.Request) (app.DuelID, error) {
	id := chi.URLParam(r, "duelId")
	if !isUUID(id) {
		return "", newProblem("not-found", "the duel does not exist")
	}
	return app.DuelID(id), nil
}

// ---- labels ----

// managerLabel is the plain label of a Branch's manager. The LLM AI without a
// configured provider plays as the rule-based fallback and says so.
func (s *Server) managerLabel(kind app.ManagerKind) string {
	switch kind {
	case app.ManagerHuman:
		return app.DuelHumanLabel
	case app.ManagerAILLM:
		if ok, model := s.svc.LLMAvailable(); ok {
			return "LLM AI (" + model + ")"
		}
		return "Rule-based AI (LLM fallback: " + app.FallbackLLMUnavailable + ")"
	}
	return app.DuelAILabel
}

// provenanceLabel is the plain label of one decision record.
func provenanceLabel(p app.Provenance, model, reason string) string {
	switch p {
	case app.ProvHuman:
		return app.DuelHumanLabel
	case app.ProvAILLM:
		if model == "" {
			return "LLM AI"
		}
		return "LLM AI (" + model + ")"
	case app.ProvAIRuleBasedFallback:
		return "Rule-based AI (LLM fallback: " + reason + ")"
	}
	return app.DuelAILabel
}

// ---- DTOs ----

type duelBranchDTO struct {
	Branch       branchDTO `json:"branch"`
	ManagerLabel string    `json:"managerLabel"`
}

type duelDTO struct {
	ID               string        `json:"id"`
	WorldID          string        `json:"worldId"`
	ParentBranchID   string        `json:"parentBranchId"`
	AIKind           string        `json:"aiKind"`
	ForkTick         int64         `json:"forkTick"`
	ComparisonPolicy string        `json:"comparisonPolicy"`
	CreatedAt        string        `json:"createdAt"`
	Human            duelBranchDTO `json:"human"`
	AI               duelBranchDTO `json:"ai"`
}

type duelStatusDTO struct {
	DuelID         string       `json:"duelId"`
	HumanHeadTick  int64        `json:"humanHeadTick"`
	AIHeadTick     int64        `json:"aiHeadTick"`
	ComparisonTick int64        `json:"comparisonTick"`
	HorizonTick    int64        `json:"horizonTick"`
	Finished       bool         `json:"finished"`
	WindowOpen     bool         `json:"windowOpen"`
	WindowReasons  []string     `json:"windowReasons"`
	WaitingOn      string       `json:"waitingOn"`
	Warnings       []warningDTO `json:"warnings,omitempty"`
}

type duelResponse struct {
	Duel   duelDTO       `json:"duel"`
	Status duelStatusDTO `json:"status"`
}

type createDuelResponse struct {
	World  worldDTO      `json:"world"`
	Duel   duelDTO       `json:"duel"`
	Status duelStatusDTO `json:"status"`
}

func (s *Server) duelToDTO(ctx context.Context, du app.Duel) (duelDTO, error) {
	hb, err := s.svc.GetBranch(ctx, du.HumanBranchID)
	if err != nil {
		return duelDTO{}, err
	}
	ab, err := s.svc.GetBranch(ctx, du.AIBranchID)
	if err != nil {
		return duelDTO{}, err
	}
	return duelDTO{
		ID: string(du.ID), WorldID: string(du.WorldID), ParentBranchID: string(du.ParentBranchID),
		AIKind: string(du.AIKind), ForkTick: int64(du.ForkTick), ComparisonPolicy: du.ComparisonPolicy,
		CreatedAt: timeString(du.CreatedAt),
		Human:     duelBranchDTO{Branch: branchToDTO(hb), ManagerLabel: s.managerLabel(hb.ManagerKind)},
		AI:        duelBranchDTO{Branch: branchToDTO(ab), ManagerLabel: s.managerLabel(ab.ManagerKind)},
	}, nil
}

func (s *Server) duelStatusOf(ctx context.Context, id app.DuelID) (duelStatusDTO, error) {
	st, err := s.duels.DuelStatus(ctx, id)
	if err != nil {
		return duelStatusDTO{}, err
	}
	waiting := "none"
	if st.WaitingOnHuman {
		waiting = "human"
	}
	return duelStatusDTO{
		DuelID: string(id), HumanHeadTick: int64(st.HumanHead), AIHeadTick: int64(st.AIHead),
		ComparisonTick: int64(st.ComparisonTick), HorizonTick: int64(st.Horizon),
		Finished:   st.HumanHead >= st.Horizon && st.AIHead >= st.Horizon,
		WindowOpen: st.WindowOpen, WindowReasons: windowReasons(st.WindowReasons), WaitingOn: waiting,
		Warnings: warningsToDTO(st.Warnings),
	}, nil
}

// ---- POST /duels ----

type createDuelRequest struct {
	Name      *string          `json:"name"`
	Seed      *int64           `json:"seed"`
	Scenario  json.RawMessage  `json:"scenario"`
	Overrides map[string]int64 `json:"overrides"`
	ForkTick  *int64           `json:"forkTick"`
	AIKind    string           `json:"aiKind"`
}

// createDuel creates a fresh World (flagship scenario and demo seed unless
// given) and a Duel on it: a human Branch and an AI Branch forked from the
// World's root Branch at forkTick (default 0).
func (s *Server) createDuel(w http.ResponseWriter, r *http.Request) {
	var req createDuelRequest
	if err := decodeBody(w, r, &req, true); err != nil {
		s.fail(w, r, err)
		return
	}
	name := defaultDuelName
	if req.Name != nil {
		name = *req.Name
	}
	seed := int64(demoplan.DefaultSeed)
	if req.Seed != nil {
		seed = *req.Seed
	}
	if seed < 0 || seed > MaxSeed {
		s.fail(w, r, badRequest("seed out of range", issue("seed", "must be between 0 and %d", int64(MaxSeed))))
		return
	}
	kind := app.ManagerKind(req.AIKind)
	if kind == "" {
		kind = app.ManagerAIRuleBased
	}
	if kind != app.ManagerAIRuleBased && kind != app.ManagerAILLM {
		s.fail(w, r, badRequest("unknown AI manager kind", issue("aiKind", "must be ai_rule_based or ai_llm")))
		return
	}
	var forkTick sim.Tick
	if req.ForkTick != nil {
		if *req.ForkTick < 0 {
			s.fail(w, r, newProblem("invalid-tick", "ticks are not negative"))
			return
		}
		forkTick = sim.Tick(*req.ForkTick)
	}
	scenario := []byte(req.Scenario)
	if len(scenario) == 0 || string(scenario) == "null" {
		scenario = s.flagship
	}
	ctx := r.Context()
	world, root, err := s.svc.CreateWorld(ctx, app.CreateWorldRequest{
		Name: name, ScenarioJSON: scenario, Seed: sim.Seed(seed), Overrides: req.Overrides,
	})
	if err != nil {
		s.fail(w, r, withIssueField(err, "name"))
		return
	}
	if forkTick > 0 {
		if err := s.advanceRootTo(ctx, world.ID, root.ID, forkTick); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	du, err := s.duels.CreateDuel(ctx, app.CreateDuelRequest{Parent: root.ID, ForkTick: forkTick, AIKind: kind})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := createDuelResponse{}
	if out.World, err = s.worldDTO(r, world.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	if out.Duel, err = s.duelToDTO(ctx, du); err != nil {
		s.fail(w, r, err)
		return
	}
	if out.Status, err = s.duelStatusOf(ctx, du.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// advanceRootTo plays the new World's root Branch (no commands) to the fork
// tick, so the Duel starts there.
func (s *Server) advanceRootTo(ctx context.Context, wid app.WorldID, root app.BranchID, to sim.Tick) error {
	_, sc, err := s.svc.GetWorld(ctx, wid)
	if err != nil {
		return err
	}
	if int64(to) > sc.HorizonTicks {
		return newProblem("invalid-tick", fmt.Sprintf("forkTick must be at most the horizon, %d", sc.HorizonTicks))
	}
	for i := int64(0); i <= sc.HorizonTicks+1; i++ {
		head, err := s.svc.HeadTick(ctx, root)
		if err != nil {
			return err
		}
		if head >= to {
			return nil
		}
		if _, err := s.svc.Advance(ctx, app.AdvanceRequest{Branch: root, RunToTick: &to}); err != nil {
			return err
		}
	}
	return errors.New("httpapi: root branch did not reach the fork tick")
}

// ---- GET /duels/{id} ----

func (s *Server) getDuel(w http.ResponseWriter, r *http.Request) {
	id, err := duelPathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	du, err := s.duels.GetDuel(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := duelResponse{}
	if out.Duel, err = s.duelToDTO(r.Context(), du); err != nil {
		s.fail(w, r, err)
		return
	}
	if out.Status, err = s.duelStatusOf(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- POST /duels/{id}/advance ----

type advanceDuelResponse struct {
	HeadTick      int64             `json:"headTick"`
	Paused        string            `json:"paused"`
	WindowReasons []string          `json:"windowReasons"`
	Human         *advanceResultDTO `json:"human,omitempty"`
	AI            *advanceResultDTO `json:"ai,omitempty"`
	Status        duelStatusDTO     `json:"status"`
	Currency      string            `json:"currency"`
	Disclaimer    string            `json:"disclaimer"`
}

func advanceToDTO(res app.AdvanceResult) *advanceResultDTO {
	if len(res.Frames) == 0 && res.Paused == "" {
		return nil
	}
	out := advanceResultDTO{
		HeadTick: int64(res.HeadTick), StateHash: hashHex(res.Hash), Paused: string(res.Paused),
		WindowReasons: windowReasons(res.WindowReasons), Frames: make([]tickFrameDTO, 0, len(res.Frames)),
		Currency: Currency, Disclaimer: sim.Disclaimer,
	}
	for _, f := range res.Frames {
		out.Frames = append(out.Frames, frameToDTO(f))
	}
	return &out
}

// duelStale turns a stale expected head tick into a problem naming both ticks.
func (s *Server) duelStale(ctx context.Context, err error, id app.DuelID, expected *int64) error {
	if !errors.Is(err, app.ErrStaleHead) {
		return err
	}
	p := problemFor(err)
	p.ExpectedHeadTick = expected
	if st, serr := s.duels.DuelStatus(ctx, id); serr == nil {
		h := int64(st.HumanHead)
		p.ActualHeadTick = &h
	}
	return p
}

// checkDuelHead enforces expectedHeadTick against the Human Branch head.
func (s *Server) checkDuelHead(ctx context.Context, id app.DuelID, expected *int64) error {
	if expected == nil {
		return nil
	}
	st, err := s.duels.DuelStatus(ctx, id)
	if err != nil {
		return err
	}
	if int64(st.HumanHead) != *expected {
		return s.duelStale(ctx, app.ErrStaleHead, id, expected)
	}
	return nil
}

func (s *Server) advanceDuel(w http.ResponseWriter, r *http.Request) {
	id, err := duelPathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req advanceRequest
	if err := decodeBody(w, r, &req, true); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.checkDuelHead(r.Context(), id, req.ExpectedHeadTick); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.duels.AdvanceDuel(r.Context(), id, tickPtr(req.RunToTick))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := advanceDuelResponse{
		HeadTick: int64(res.HeadTick), Paused: string(res.Paused), WindowReasons: windowReasons(res.WindowReasons),
		Human: advanceToDTO(res.Human), AI: advanceToDTO(res.AI), Currency: Currency, Disclaimer: sim.Disclaimer,
	}
	if out.Status, err = s.duelStatusOf(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- POST /duels/{id}/commands ----

// submitDuelCommands submits the Human Manager's Commands. The AI side has no
// endpoint: its Commands come only from its manager.
func (s *Server) submitDuelCommands(w http.ResponseWriter, r *http.Request) {
	id, err := duelPathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req submitCommandsRequest
	if err := decodeBody(w, r, &req, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("windowTick", req.WindowTick); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Commands == nil {
		s.fail(w, r, badRequest("missing request field", issue("commands", "is required")))
		return
	}
	if n := len(req.Commands); n < 1 || n > maxCommandsInReq {
		s.fail(w, r, validation("a request holds 1 to 50 commands", issue("commands", "has %d items, want 1 to %d", n, maxCommandsInReq)))
		return
	}
	idem := ""
	if key, has := r.Header["Idempotency-Key"]; has {
		idem = key[0]
		if !validIdempotencyKey(idem) {
			s.fail(w, r, badRequest("malformed header", issue("Idempotency-Key", "must be 1 to %d printable ASCII characters", maxIdemKeyLen)))
			return
		}
	}
	cmds := make([]sim.Command, 0, len(req.Commands))
	var issues []ProblemIssue
	for i, c := range req.Commands {
		cmd, is := c.toSim(fmt.Sprintf("commands[%d]", i))
		if is != nil {
			issues = append(issues, *is)
			continue
		}
		cmds = append(cmds, cmd)
	}
	if len(issues) > 0 {
		s.fail(w, r, validation("a command is invalid", issues...))
		return
	}
	res, err := s.duels.SubmitHuman(r.Context(), id, app.SubmitRequest{
		WindowTick: sim.Tick(*req.WindowTick), Commands: cmds, IdempotencyKey: idem, ExpectedHeadTick: tickPtr(req.ExpectedHeadTick),
	})
	if err != nil {
		s.fail(w, r, s.duelStale(r.Context(), err, id, req.ExpectedHeadTick))
		return
	}
	out, err := submitResultToDTO(res)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- POST /duels/{id}/decision-window/end ----

func (s *Server) endDuelWindow(w http.ResponseWriter, r *http.Request) {
	id, err := duelPathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req endWindowRequest
	if err := decodeBody(w, r, &req, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("windowTick", req.WindowTick); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.duels.EndHumanWindow(r.Context(), id, sim.Tick(*req.WindowTick), tickPtr(req.ExpectedHeadTick)); err != nil {
		s.fail(w, r, s.duelStale(r.Context(), err, id, req.ExpectedHeadTick))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- POST /duels/{id}/disruptions ----

type duelInjectResponse struct {
	Human injectDisruptionResponse `json:"human"`
	AI    injectDisruptionResponse `json:"ai"`
}

func (s *Server) injectDuelDisruption(w http.ResponseWriter, r *http.Request) {
	id, err := duelPathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req injectDisruptionRequest
	if err := decodeBody(w, r, &req, false); err != nil {
		s.fail(w, r, err)
		return
	}
	for _, f := range []struct {
		name string
		p    *int64
	}{{"startTick", req.StartTick}, {"durationTicks", req.DurationTicks}} {
		if err := requireField(f.name, f.p); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	kind, ok := disruptionKindCode(req.Kind)
	if !ok {
		s.fail(w, r, badRequest("unknown disruption kind", issue("kind", "must be demand_surge, supplier_outage or workforce_absence")))
		return
	}
	in := app.DuelInjectRequest{
		Kind: kind, StartTick: sim.Tick(*req.StartTick), DurationTicks: *req.DurationTicks, Target: -1,
		ExpectedHeadTick: tickPtr(req.ExpectedHeadTick),
	}
	if req.Target != nil {
		in.Target = *req.Target
	}
	if req.MagnitudeBps != nil {
		in.MagnitudeBps = sim.Bps(*req.MagnitudeBps)
	}
	switch req.Role {
	case "":
	case "primary":
		in.Role = sim.SupplierPrimary
	case "backup":
		in.Role = sim.SupplierBackup
	default:
		s.fail(w, r, badRequest("unknown supplier role", issue("role", "must be primary or backup")))
		return
	}
	if kind == sim.DisruptionSupplierOutage && req.Role == "" {
		s.fail(w, r, badRequest("missing request field", issue("role", "is required for supplier_outage")))
		return
	}
	if kind != sim.DisruptionSupplierOutage && req.Role != "" {
		s.fail(w, r, validation("a disruption input is invalid", issue("role", "only supplier_outage takes a role")))
		return
	}
	if key, has := r.Header["Idempotency-Key"]; has {
		if !validIdempotencyKey(key[0]) {
			s.fail(w, r, badRequest("malformed header", issue("Idempotency-Key", "must be 1 to %d printable ASCII characters", maxIdemKeyLen)))
			return
		}
		in.IdempotencyKey = key[0]
	}
	res, err := s.duels.InjectDisruption(r.Context(), id, in)
	if err != nil {
		if errors.Is(err, sim.ErrBadDisruption) {
			err = validation("the disruption is outside its allowed range", issue("disruption", "%s", err.Error()))
		}
		s.fail(w, r, s.duelStale(r.Context(), err, id, req.ExpectedHeadTick))
		return
	}
	var out duelInjectResponse
	if out.Human, err = injectResultToDTO(res.Human); err != nil {
		s.fail(w, r, err)
		return
	}
	if out.AI, err = injectResultToDTO(res.AI); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- GET /duels/{id}/score ----

type scoreComponentsDTO struct {
	FinancialBps        int64 `json:"financialBps"`
	FulfilmentRateBps   int64 `json:"fulfilmentRateBps"`
	OnTimeBps           int64 `json:"onTimeBps"`
	CompositeHundredths int64 `json:"compositeHundredths"`
}

type weightedComponentsDTO struct {
	FinancialHundredths  int64 `json:"financialHundredths"`
	FulfilmentHundredths int64 `json:"fulfilmentHundredths"`
	OnTimeHundredths     int64 `json:"onTimeHundredths"`
}

type duelBranchScoreDTO struct {
	BranchID        string                `json:"branchId"`
	ManagerKind     string                `json:"managerKind"`
	ManagerLabel    string                `json:"managerLabel"`
	Score           scoreComponentsDTO    `json:"score"`
	Weighted        weightedComponentsDTO `json:"weighted"`
	Metrics         metricsDTO            `json:"metrics"`
	CancelledOrders int64                 `json:"cancelledOrders"`
}

type duelScoreResponse struct {
	DuelID                 string             `json:"duelId"`
	ComparisonTick         int64              `json:"comparisonTick"`
	HorizonTick            int64              `json:"horizonTick"`
	Provisional            bool               `json:"provisional"`
	Outcome                string             `json:"outcome"`
	Human                  duelBranchScoreDTO `json:"human"`
	AI                     duelBranchScoreDTO `json:"ai"`
	Weights                scoreWeightsDTO    `json:"weights"`
	ProfitFloorCents       int64              `json:"profitFloorCents"`
	ProfitTargetCents      int64              `json:"profitTargetCents"`
	TieToleranceHundredths int64              `json:"tieToleranceHundredths"`
	Currency               string             `json:"currency"`
	Disclaimer             string             `json:"disclaimer"`
	Warnings               []warningDTO       `json:"warnings,omitempty"`
}

func (s *Server) branchScoreToDTO(b app.DuelBranchScore) duelBranchScoreDTO {
	return duelBranchScoreDTO{
		BranchID: string(b.Branch), ManagerKind: string(b.Manager), ManagerLabel: s.managerLabel(b.Manager),
		Score: scoreComponentsDTO{
			FinancialBps: int64(b.Score.Financial), FulfilmentRateBps: int64(b.Score.FulfilmentRate),
			OnTimeBps: int64(b.Score.OnTime), CompositeHundredths: b.Score.Composite,
		},
		Weighted: weightedComponentsDTO{
			FinancialHundredths: b.Weighted.Financial, FulfilmentHundredths: b.Weighted.Fulfilment, OnTimeHundredths: b.Weighted.OnTime,
		},
		Metrics: metricsToDTO(b.Metrics), CancelledOrders: b.Cancelled,
	}
}

func (s *Server) getDuelScore(w http.ResponseWriter, r *http.Request) {
	id, err := duelPathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sc, err := s.duels.DuelScore(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, duelScoreResponse{
		DuelID: string(id), ComparisonTick: int64(sc.ComparisonTick), HorizonTick: int64(sc.Horizon),
		Provisional: sc.Provisional, Outcome: string(sc.Outcome),
		Human: s.branchScoreToDTO(sc.Human), AI: s.branchScoreToDTO(sc.AI),
		Weights: scoreWeightsDTO{
			FinancialWeightBps: int64(sc.Weights.FinancialWeightBps), FulfilmentWeightBps: int64(sc.Weights.FulfilmentWeightBps),
			OnTimeWeightBps: int64(sc.Weights.OnTimeWeightBps),
		},
		ProfitFloorCents: int64(sc.ProfitFloorCents), ProfitTargetCents: int64(sc.ProfitTargetCents),
		TieToleranceHundredths: sc.TieToleranceHundredths,
		Currency:               Currency, Disclaimer: sim.Disclaimer, Warnings: warningsToDTO(sc.Warnings),
	})
}

// ---- GET /duels/{id}/decisions ----

type duelDecisionDTO struct {
	decisionRecordDTO
	ManagerLabel string `json:"managerLabel"`
}

type duelDecisionsResponse struct {
	DuelID    string            `json:"duelId"`
	BranchID  string            `json:"branchId"`
	Decisions []duelDecisionDTO `json:"decisions"`
}

// listDuelDecisions returns the AI side's decision records. While the Human
// Manager's window at the comparison tick is still open, the AI's record for
// that window is withheld: the human must not see the opponent's move before
// committing their own (parity of information, Req 21).
func (s *Server) listDuelDecisions(w http.ResponseWriter, r *http.Request) {
	id, err := duelPathID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.duels.DuelStatus(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	recs, err := s.svc.DecisionRecords(r.Context(), st.Duel.AIBranchID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := duelDecisionsResponse{DuelID: string(id), BranchID: string(st.Duel.AIBranchID), Decisions: make([]duelDecisionDTO, 0, len(recs))}
	for _, d := range recs {
		if st.WaitingOnHuman && d.WindowTick >= st.ComparisonTick {
			continue
		}
		dto := decisionRecordDTO{
			WindowTick: int64(d.WindowTick), ManagerKind: string(d.ManagerKind), Provenance: string(d.Provenance),
			Model: d.Model, FallbackReason: d.FallbackReason, Rationale: d.Rationale,
			Proposed: make([]commandDTO, 0, len(d.Proposed)), AcceptedCount: d.AcceptedCount,
			Rejected: make([]validationResultDTO, 0, len(d.Rejected)), CreatedAt: timeString(d.CreatedAt),
		}
		for _, c := range d.Proposed {
			dto.Proposed = append(dto.Proposed, commandToDTO(c))
		}
		for _, v := range d.Rejected {
			dto.Rejected = append(dto.Rejected, validationResultDTO{Index: v.Index, Command: commandToDTO(v.Command), Code: v.Code, Message: v.Message})
		}
		out.Decisions = append(out.Decisions, duelDecisionDTO{
			decisionRecordDTO: dto, ManagerLabel: provenanceLabel(d.Provenance, d.Model, d.FallbackReason),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
