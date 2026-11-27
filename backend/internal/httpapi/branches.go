package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Read bounds (openapi.yaml, tick-frames and ledger).
const (
	maxFrameRange    = 240
	maxCommandsInReq = 50
	defaultPageLimit = 200
	maxPageLimit     = 1000
	maxIdemKeyLen    = 128
)

func engineVersion() string { return strconv.Itoa(sim.EngineVersion) }

func tickPtr(p *int64) *sim.Tick {
	if p == nil {
		return nil
	}
	t := sim.Tick(*p)
	return &t
}

// stale turns app.ErrStaleHead into a problem that names both head ticks.
func (s *Server) stale(ctx context.Context, err error, id app.BranchID, expected *int64) error {
	if !errors.Is(err, app.ErrStaleHead) {
		return err
	}
	p := problemFor(err)
	p.ExpectedHeadTick = expected
	if head, herr := s.svc.HeadTick(ctx, id); herr == nil {
		h := int64(head)
		p.ActualHeadTick = &h
	}
	return p
}

func (s *Server) getBranch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	b, err := s.svc.GetBranch(r.Context(), app.BranchID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dto := branchToDTO(b)
	if dto.Warnings, err = s.branchWarnings(r, b.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

type forkRequest struct {
	AtTick      *int64  `json:"atTick"`
	Label       *string `json:"label"`
	ManagerKind string  `json:"managerKind"`
}

func (s *Server) forkBranch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req forkRequest
	if err := decodeBody(w, r, &req, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("atTick", req.AtTick); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("label", req.Label); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.ManagerKind != "" && !managerKindValid(req.ManagerKind) {
		s.fail(w, r, badRequest("unknown manager kind", issue("managerKind", "must be human, ai_rule_based or ai_llm")))
		return
	}
	child, err := s.svc.ForkBranch(r.Context(), app.BranchID(id), sim.Tick(*req.AtTick), *req.Label, app.ManagerKind(req.ManagerKind))
	if err != nil {
		s.fail(w, r, withIssueField(err, "label"))
		return
	}
	writeJSON(w, http.StatusCreated, branchToDTO(child))
}

type advanceRequest struct {
	RunToTick        *int64 `json:"runToTick"`
	ExpectedHeadTick *int64 `json:"expectedHeadTick"`
}

type advanceResultDTO struct {
	HeadTick      int64          `json:"headTick"`
	StateHash     string         `json:"stateHash"`
	Paused        string         `json:"paused"`
	WindowReasons []string       `json:"windowReasons"`
	Frames        []tickFrameDTO `json:"frames"`
	Currency      string         `json:"currency"`
	Disclaimer    string         `json:"disclaimer"`
}

func (s *Server) advanceBranch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req advanceRequest
	if err := decodeBody(w, r, &req, true); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.svc.Advance(r.Context(), app.AdvanceRequest{
		Branch: app.BranchID(id), RunToTick: tickPtr(req.RunToTick), ExpectedHeadTick: tickPtr(req.ExpectedHeadTick),
	})
	if err != nil {
		s.fail(w, r, s.stale(r.Context(), err, app.BranchID(id), req.ExpectedHeadTick))
		return
	}
	out := advanceResultDTO{
		HeadTick: int64(res.HeadTick), StateHash: hashHex(res.Hash), Paused: string(res.Paused),
		WindowReasons: windowReasons(res.WindowReasons), Frames: make([]tickFrameDTO, 0, len(res.Frames)),
		Currency: Currency, Disclaimer: sim.Disclaimer,
	}
	for _, f := range res.Frames {
		out.Frames = append(out.Frames, frameToDTO(f))
	}
	writeJSON(w, http.StatusOK, out)
}

type submitCommandsRequest struct {
	WindowTick       *int64       `json:"windowTick"`
	Commands         []commandDTO `json:"commands"`
	ExpectedHeadTick *int64       `json:"expectedHeadTick"`
}

// validIdempotencyKey: 1 to 128 printable ASCII characters (0x21 to 0x7E).
func validIdempotencyKey(k string) bool {
	if len(k) < 1 || len(k) > maxIdemKeyLen {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 0x21 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

func (s *Server) submitCommands(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
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
	key, hasKey := r.Header["Idempotency-Key"]
	idem := ""
	if hasKey {
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
	res, err := s.svc.SubmitCommands(r.Context(), app.SubmitRequest{
		Branch: app.BranchID(id), WindowTick: sim.Tick(*req.WindowTick), Commands: cmds,
		IdempotencyKey: idem, ExpectedHeadTick: tickPtr(req.ExpectedHeadTick),
	})
	if err != nil {
		s.fail(w, r, s.stale(r.Context(), err, app.BranchID(id), req.ExpectedHeadTick))
		return
	}
	out, err := submitResultToDTO(res)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type endWindowRequest struct {
	WindowTick       *int64 `json:"windowTick"`
	ExpectedHeadTick *int64 `json:"expectedHeadTick"`
}

func (s *Server) endDecisionWindow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
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
	bid := app.BranchID(id)
	if err := s.svc.EndDecisionWindow(r.Context(), bid, sim.Tick(*req.WindowTick), tickPtr(req.ExpectedHeadTick)); err != nil {
		s.fail(w, r, s.stale(r.Context(), err, bid, req.ExpectedHeadTick))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type injectDisruptionRequest struct {
	Kind             string `json:"kind"`
	StartTick        *int64 `json:"startTick"`
	DurationTicks    *int64 `json:"durationTicks"`
	MagnitudeBps     *int64 `json:"magnitudeBps"`
	Target           *int64 `json:"target"`
	Role             string `json:"role"`
	ExpectedHeadTick *int64 `json:"expectedHeadTick"`
}

type injectDisruptionResponse struct {
	Seq           uint64 `json:"seq"`
	EffectiveTick int64  `json:"effectiveTick"`
	Kind          string `json:"kind"`
	DurationTicks int64  `json:"durationTicks"`
	MagnitudeBps  int64  `json:"magnitudeBps"`
	Target        int64  `json:"target"`
	Role          string `json:"role,omitempty"`
	Provenance    string `json:"provenance"`
	Duplicate     bool   `json:"duplicate"`
}

func disruptionKindCode(name string) (uint8, bool) {
	for _, c := range []uint8{sim.DisruptionDemandSurge, sim.DisruptionSupplierOutage, sim.DisruptionWorkforceAbsence} {
		if disruptionKindName(c) == name {
			return c, true
		}
	}
	return 0, false
}

func (s *Server) injectDisruption(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
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
	in := app.InjectRequest{
		Branch: app.BranchID(id), Kind: kind, StartTick: sim.Tick(*req.StartTick),
		DurationTicks: *req.DurationTicks, Target: -1, ExpectedHeadTick: tickPtr(req.ExpectedHeadTick),
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
	res, err := s.svc.InjectDisruption(r.Context(), in)
	if err != nil {
		if errors.Is(err, sim.ErrBadDisruption) {
			err = validation("the disruption is outside its allowed range", issue("disruption", "%s", err.Error()))
		}
		s.fail(w, r, s.stale(r.Context(), err, app.BranchID(id), req.ExpectedHeadTick))
		return
	}
	d, err := sim.DecodeDisruptionInput(res.Input.Payload)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := injectDisruptionResponse{
		Seq: res.Input.Seq, EffectiveTick: int64(res.Input.EffectiveTick), Kind: disruptionKindName(d.Kind),
		DurationTicks: d.DurationTicks, MagnitudeBps: int64(d.MagnitudeBps), Target: d.Target,
		Provenance: string(res.Input.Metadata.Provenance), Duplicate: res.Duplicate,
	}
	if d.Kind == sim.DisruptionSupplierOutage {
		out.Role = "primary"
		if d.Role == sim.SupplierBackup {
			out.Role = "backup"
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type tickFramesResponse struct {
	BranchID   string         `json:"branchId"`
	Frames     []tickFrameDTO `json:"frames"`
	Currency   string         `json:"currency"`
	Disclaimer string         `json:"disclaimer"`
	Warnings   []warningDTO   `json:"warnings,omitempty"`
}

// rangeParams reads from and to. A missing or non-integer value is a 400; a
// range that is reversed, negative or longer than max ticks is a 422.
func rangeParams(r *http.Request, maxSpan int64) (from, to int64, err error) {
	if from, _, err = queryInt(r, "from", true); err != nil {
		return
	}
	if to, _, err = queryInt(r, "to", true); err != nil {
		return
	}
	switch {
	case from < 0 || to < 0:
		return 0, 0, newProblem("invalid-tick", "ticks are not negative")
	case to < from:
		return 0, 0, validation("the range is reversed", issue("to", "must be at least from"))
	case maxSpan > 0 && to-from+1 > maxSpan:
		return 0, 0, validation("the range is too long", issue("to", "a range spans at most %d ticks", maxSpan))
	}
	return from, to, nil
}

func (s *Server) listTickFrames(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	from, to, err := rangeParams(r, maxFrameRange)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	bid := app.BranchID(id)
	warn, err := s.branchWarnings(r, bid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := tickFramesResponse{Warnings: warn, BranchID: id, Frames: make([]tickFrameDTO, 0, to-from+1), Currency: Currency, Disclaimer: sim.Disclaimer}
	for t := from; t <= to; t++ {
		f, err := s.svc.FrameAt(r.Context(), bid, sim.Tick(t))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out.Frames = append(out.Frames, frameToDTO(f))
	}
	writeJSON(w, http.StatusOK, out)
}

func tickParam(r *http.Request) (sim.Tick, error) {
	t, _, err := queryInt(r, "tick", true)
	if err != nil {
		return 0, err
	}
	if t < 0 {
		return 0, newProblem("invalid-tick", "ticks are not negative")
	}
	return sim.Tick(t), nil
}

type branchStateResponse struct {
	BranchID    string         `json:"branchId"`
	Tick        int64          `json:"tick"`
	StateHash   string         `json:"stateHash"`
	Observation observationDTO `json:"observation"`
	Currency    string         `json:"currency"`
	Disclaimer  string         `json:"disclaimer"`
	Warnings    []warningDTO   `json:"warnings,omitempty"`
}

func (s *Server) getBranchState(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := tickParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	bid := app.BranchID(id)
	h, err := s.svc.HashAt(r.Context(), bid, t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	obs, err := s.svc.ObservationAt(r.Context(), bid, t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	warn, err := s.branchWarnings(r, bid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, branchStateResponse{
		Warnings: warn, BranchID: id, Tick: int64(t), StateHash: hashHex(h), Observation: observationToDTO(obs),
		Currency: Currency, Disclaimer: sim.Disclaimer,
	})
}

type metricsResponse struct {
	BranchID   string       `json:"branchId"`
	Tick       int64        `json:"tick"`
	StateHash  string       `json:"stateHash"`
	Metrics    metricsDTO   `json:"metrics"`
	Currency   string       `json:"currency"`
	Disclaimer string       `json:"disclaimer"`
	Warnings   []warningDTO `json:"warnings,omitempty"`
}

func (s *Server) getBranchMetrics(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := tickParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f, err := s.svc.FrameAt(r.Context(), app.BranchID(id), t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	warn, err := s.branchWarnings(r, app.BranchID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, metricsResponse{
		Warnings: warn, BranchID: id, Tick: int64(t), StateHash: hashHex(f.Hash), Metrics: metricsToDTO(f.Metrics),
		Currency: Currency, Disclaimer: sim.Disclaimer,
	})
}

type ledgerResponse struct {
	BranchID   string           `json:"branchId"`
	From       int64            `json:"from"`
	To         int64            `json:"to"`
	Total      int              `json:"total"`
	Offset     int64            `json:"offset"`
	Entries    []ledgerEntryDTO `json:"entries"`
	Currency   string           `json:"currency"`
	Disclaimer string           `json:"disclaimer"`
	Warnings   []warningDTO     `json:"warnings,omitempty"`
}

func (s *Server) listLedgerEntries(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	from, to, err := rangeParams(r, 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var account sim.AccountID
	if name := r.URL.Query().Get("account"); name != "" {
		a, ok := accountByName(name)
		if !ok {
			s.fail(w, r, badRequest("unknown account", issue("account", "must be one of the chart of accounts, for example Cash")))
			return
		}
		account = a
	}
	limit, present, err := queryInt(r, "limit", false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !present {
		limit = defaultPageLimit
	}
	offset, _, err := queryInt(r, "offset", false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if limit < 1 || limit > maxPageLimit {
		s.fail(w, r, validation("limit out of range", issue("limit", "must be 1 to %d", maxPageLimit)))
		return
	}
	if offset < 0 {
		s.fail(w, r, validation("offset out of range", issue("offset", "must not be negative")))
		return
	}
	st, err := s.svc.StateAt(r.Context(), app.BranchID(id), sim.Tick(to))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	warn, err := s.branchWarnings(r, app.BranchID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := ledgerResponse{
		Warnings: warn, BranchID: id, From: from, To: to, Offset: offset, Entries: make([]ledgerEntryDTO, 0),
		Currency: Currency, Disclaimer: sim.Disclaimer,
	}
	for i, e := range st.Ledger.Entries {
		if int64(e.Tick) < from || int64(e.Tick) > to || (account != 0 && !hasAccount(e, account)) {
			continue
		}
		if int64(out.Total) >= offset && int64(len(out.Entries)) < limit {
			out.Entries = append(out.Entries, ledgerEntryToDTO(i, e))
		}
		out.Total++
	}
	writeJSON(w, http.StatusOK, out)
}

func hasAccount(e sim.LedgerEntry, a sim.AccountID) bool {
	for _, l := range e.Lines {
		if l.Account == a {
			return true
		}
	}
	return false
}
