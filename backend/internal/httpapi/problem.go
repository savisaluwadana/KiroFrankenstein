package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

const problemPrefix = "urn:parallel:problem:"

// Problem is an RFC 9457 problem details document. It never carries a stack
// trace. *Problem implements error so handlers can return it directly.
type Problem struct {
	Type             string         `json:"type"`
	Title            string         `json:"title"`
	Status           int            `json:"status"`
	Detail           string         `json:"detail,omitempty"`
	Instance         string         `json:"instance,omitempty"`
	Issues           []ProblemIssue `json:"issues,omitempty"`
	ExpectedHeadTick *int64         `json:"expectedHeadTick,omitempty"`
	ActualHeadTick   *int64         `json:"actualHeadTick,omitempty"`
}

// ProblemIssue is one field-level issue.
type ProblemIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (p *Problem) Error() string { return p.Type + ": " + p.Detail }

// problemSpec is the status and title of one problem slug (openapi.yaml,
// schema ProblemType).
type problemSpec struct {
	status int
	title  string
}

var problemSpecs = map[string]problemSpec{
	"history-immutable":       {http.StatusConflict, "History is immutable"},
	"not-yet-simulated":       {http.StatusConflict, "Tick is not yet simulated"},
	"stale-head":              {http.StatusConflict, "Stale head tick"},
	"window-not-open":         {http.StatusConflict, "No decision window is open"},
	"branch-limit":            {http.StatusConflict, "Branch limit reached"},
	"horizon-reached":         {http.StatusConflict, "Horizon reached"},
	"engine-version-mismatch": {http.StatusConflict, "Engine version mismatch"},
	"budget-exceeded":         {http.StatusUnprocessableEntity, "Command budget exceeded"},
	"validation":              {http.StatusUnprocessableEntity, "Validation failed"},
	"invalid-tick":            {http.StatusUnprocessableEntity, "Invalid tick"},
	"invalid-scenario":        {http.StatusBadRequest, "Invalid scenario"},
	"invalid-name":            {http.StatusBadRequest, "Invalid name"},
	"bad-request":             {http.StatusBadRequest, "Bad request"},
	"not-found":               {http.StatusNotFound, "Not found"},
	"payload-too-large":       {http.StatusRequestEntityTooLarge, "Payload too large"},
	"rate-limited":            {http.StatusTooManyRequests, "Rate limit exceeded"},
	"unavailable":             {http.StatusServiceUnavailable, "Service unavailable"},
	"internal":                {http.StatusInternalServerError, "Internal error"},
}

func newProblem(slug, detail string, issues ...ProblemIssue) *Problem {
	spec, ok := problemSpecs[slug]
	if !ok {
		panic(fmt.Sprintf("httpapi: unknown problem slug %q", slug))
	}
	return &Problem{Type: problemPrefix + slug, Title: spec.title, Status: spec.status, Detail: detail, Issues: issues}
}

func badRequest(detail string, issues ...ProblemIssue) *Problem {
	return newProblem("bad-request", detail, issues...)
}

func validation(detail string, issues ...ProblemIssue) *Problem {
	return newProblem("validation", detail, issues...)
}

func issue(field, format string, a ...any) ProblemIssue {
	return ProblemIssue{Field: field, Message: fmt.Sprintf(format, a...)}
}

// problemFor maps an error to a Problem. Typed application errors are matched
// with errors.Is, so wrapping never changes the mapping. Anything unknown
// becomes a generic 500 whose detail does not echo the error text.
func problemFor(err error) *Problem {
	var p *Problem
	if errors.As(err, &p) {
		return p
	}
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return newProblem("payload-too-large", fmt.Sprintf("request body exceeds %d bytes", MaxBodyBytes))
	case errors.Is(err, app.ErrEngineVersionMismatch):
		detail := "the world was recorded under another engine version; reads are flagged, but the history cannot be extended"
		var em *app.EngineVersionMismatchError
		if errors.As(err, &em) {
			detail = fmt.Sprintf("the world was recorded under engine version %s but the running engine is version %s; "+
				"the engine cannot guarantee identical outcomes, so the world is read-only", em.Stored, em.Running)
		}
		return newProblem("engine-version-mismatch", detail)
	case errors.Is(err, app.ErrNotFound):
		return newProblem("not-found", "the world or branch does not exist")
	case errors.Is(err, app.ErrHistoryImmutable):
		return newProblem("history-immutable", "the input would take effect at a tick that is already computed; fork to change the past")
	case errors.Is(err, app.ErrNotYetSimulated):
		return newProblem("not-yet-simulated", "the tick is above the head tick")
	case errors.Is(err, app.ErrStaleHead):
		return newProblem("stale-head", "expectedHeadTick differs from the branch head tick")
	case errors.Is(err, app.ErrWindowNotOpen):
		return newProblem("window-not-open", "no decision window is open at the head tick")
	case errors.Is(err, app.ErrBranchLimit):
		return newProblem("branch-limit", fmt.Sprintf("a world holds at most %d branches", app.MaxBranchesPerWorld))
	case errors.Is(err, app.ErrHorizonReached):
		return newProblem("horizon-reached", "the branch is at the scenario horizon")
	case errors.Is(err, app.ErrBudgetExceeded):
		return newProblem("budget-exceeded", "the command budget of this decision window is spent")
	case errors.Is(err, app.ErrInvalidTick):
		return newProblem("invalid-tick", "the tick is negative or not ahead of the head tick")
	case errors.Is(err, app.ErrInvalidName):
		return newProblem("invalid-name", "a name is 1 to 64 printable characters", issue("name", "must be 1 to 64 printable characters"))
	case errors.Is(err, app.ErrInvalidScenario):
		return newProblem("invalid-scenario", "the scenario or an override failed validation", scenarioIssues(err)...)
	}
	return newProblem("internal", "the server could not complete the request")
}

func scenarioIssues(err error) []ProblemIssue {
	var ve *sim.ScenarioValidationError
	if errors.As(err, &ve) {
		out := make([]ProblemIssue, 0, len(ve.Issues))
		for _, is := range ve.Issues {
			out = append(out, ProblemIssue{Field: is.Assumption, Message: is.Msg})
		}
		return out
	}
	var pe *sim.ScenarioParseError
	if errors.As(err, &pe) {
		f := pe.Path
		if f == "" {
			f = "scenario"
		}
		return []ProblemIssue{{Field: f, Message: pe.Msg}}
	}
	if errors.Is(err, sim.ErrUnknownAssumption) {
		return []ProblemIssue{{Field: "overrides", Message: err.Error()}}
	}
	return []ProblemIssue{{Field: "scenario", Message: err.Error()}}
}

// writeProblem writes p as application/problem+json.
func writeProblem(w http.ResponseWriter, r *http.Request, p *Problem) {
	out := *p
	if out.Instance == "" && r != nil {
		out.Instance = r.URL.Path
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(out.Status)
	_ = json.NewEncoder(w).Encode(out)
}

// withIssueField renames the field of the single issue of an invalid-name
// problem (the service does not know whether a world name or a branch label
// was rejected).
func withIssueField(err error, field string) error {
	var p *Problem
	if !errors.Is(err, app.ErrInvalidName) {
		return err
	}
	p = problemFor(err)
	if len(p.Issues) == 1 {
		p.Issues[0].Field = field
	}
	return p
}
