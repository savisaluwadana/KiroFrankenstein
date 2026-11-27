package httpapi

import (
	"net/http"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

type createDiffRequest struct {
	BranchA        *string `json:"branchA"`
	BranchB        *string `json:"branchB"`
	ComparisonTick *int64  `json:"comparisonTick"`
}

type diffComponentDTO struct {
	Name string  `json:"name"`
	SKUs []int64 `json:"skus"`
}

type diffInputDTO struct {
	Side          string         `json:"side"`
	BranchID      string         `json:"branchId"`
	Seq           uint64         `json:"seq"`
	EffectiveTick int64          `json:"effectiveTick"`
	Kind          string         `json:"kind"`
	Command       *commandDTO    `json:"command,omitempty"`
	Disruption    *diffDisrupDTO `json:"disruption,omitempty"`
	Provenance    string         `json:"provenance"`
}

type diffDisrupDTO struct {
	Kind          string `json:"kind"`
	Target        int64  `json:"target"`
	Role          string `json:"role,omitempty"`
	DurationTicks int64  `json:"durationTicks"`
	MagnitudeBps  int64  `json:"magnitudeBps"`
}

type diffMetricDTO struct {
	Key   string `json:"key"`
	A     int64  `json:"a"`
	B     int64  `json:"b"`
	Delta int64  `json:"delta"`
}

type diffSKUStockDTO struct {
	SKU        int64 `json:"sku"`
	A          int64 `json:"a"`
	B          int64 `json:"b"`
	Delta      int64 `json:"delta"`
	ValueA     int64 `json:"valueCentsA"`
	ValueB     int64 `json:"valueCentsB"`
	ValueDelta int64 `json:"valueCentsDelta"`
}

// diffDivergenceDTO is the first divergence: the tick, both state hashes at
// it, and the first outcome that differs on each side.
type diffDivergenceDTO struct {
	Tick        int64              `json:"tick"`
	StateHashA  string             `json:"stateHashA"`
	StateHashB  string             `json:"stateHashB"`
	Components  []diffComponentDTO `json:"components"`
	FirstEventA *journalEntryDTO   `json:"firstEventA,omitempty"`
	FirstEventB *journalEntryDTO   `json:"firstEventB,omitempty"`
}

type diffResponse struct {
	BranchA        string             `json:"branchA"`
	BranchB        string             `json:"branchB"`
	WorldID        string             `json:"worldId"`
	ComparisonTick int64              `json:"comparisonTick"`
	Divergence     *diffDivergenceDTO `json:"divergence"`
	Inputs         []diffInputDTO     `json:"differingInputs"`
	Metrics        []diffMetricDTO    `json:"metrics"`
	SKUStock       []diffSKUStockDTO  `json:"skuStock"`
	Statement      string             `json:"statement"`
	Disclaimer     string             `json:"disclaimer"`
	Currency       string             `json:"currency"`
	Warnings       []warningDTO       `json:"warnings,omitempty"`
}

func diffInputKindName(k sim.InputKind) string {
	if k == sim.InputDisruption {
		return "disruption"
	}
	return "command"
}

// createDiff serves POST /diffs. It only reads, so it also works for the
// branches of a duel.
func (s *Server) createDiff(w http.ResponseWriter, r *http.Request) {
	var req createDiffRequest
	if err := decodeBody(w, r, &req, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("branchA", req.BranchA); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := requireField("branchB", req.BranchB); err != nil {
		s.fail(w, r, err)
		return
	}
	if !isUUID(*req.BranchA) || !isUUID(*req.BranchB) {
		s.fail(w, r, newProblem("not-found", "the world or branch does not exist"))
		return
	}
	if req.ComparisonTick != nil && *req.ComparisonTick < 0 {
		s.fail(w, r, newProblem("invalid-tick", "ticks are not negative"))
		return
	}
	ctx := r.Context()
	res, err := s.svc.Diff(ctx, app.BranchID(*req.BranchA), app.BranchID(*req.BranchB), tickPtr(req.ComparisonTick))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := diffResponse{
		BranchA: string(res.BranchA), BranchB: string(res.BranchB), WorldID: string(res.World),
		ComparisonTick: int64(res.ComparisonTick), Inputs: make([]diffInputDTO, 0, len(res.DifferingInputs)),
		Metrics: make([]diffMetricDTO, 0, len(res.Metrics)), SKUStock: make([]diffSKUStockDTO, 0, len(res.SKUStock)),
		Statement: res.Statement, Disclaimer: res.Disclaimer, Currency: res.Currency, Warnings: warningsToDTO(res.Warnings),
	}
	if res.FirstDivergenceTick != nil {
		t := *res.FirstDivergenceTick
		ha, err := s.svc.HashAt(ctx, res.BranchA, t)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		hb, err := s.svc.HashAt(ctx, res.BranchB, t)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d := &diffDivergenceDTO{
			Tick: int64(t), StateHashA: hashHex(ha), StateHashB: hashHex(hb),
			Components: make([]diffComponentDTO, 0, len(res.DifferingComponents)),
		}
		for _, c := range res.DifferingComponents {
			skus := c.SKUs
			if skus == nil {
				skus = []int64{}
			}
			d.Components = append(d.Components, diffComponentDTO{Name: c.Name, SKUs: skus})
		}
		if res.FirstEventA != nil {
			e := journalEntryToDTO(0, *res.FirstEventA)
			d.FirstEventA = &e
		}
		if res.FirstEventB != nil {
			e := journalEntryToDTO(0, *res.FirstEventB)
			d.FirstEventB = &e
		}
		out.Divergence = d
	}
	for _, in := range res.DifferingInputs {
		x := diffInputDTO{
			Side: in.Side, BranchID: string(in.Branch), Seq: in.Seq, EffectiveTick: int64(in.EffectiveTick),
			Kind: diffInputKindName(in.Kind), Provenance: string(in.Provenance),
		}
		if in.Command != nil {
			c := commandToDTO(*in.Command)
			x.Command = &c
		}
		if in.Disruption != nil {
			d := in.Disruption
			x.Disruption = &diffDisrupDTO{
				Kind: disruptionKindName(d.Kind), Target: d.Target, DurationTicks: d.DurationTicks, MagnitudeBps: int64(d.MagnitudeBps),
			}
			if d.Kind == sim.DisruptionSupplierOutage {
				x.Disruption.Role = supplierRoleName(d.Role)
			}
		}
		out.Inputs = append(out.Inputs, x)
	}
	for _, m := range res.Metrics {
		out.Metrics = append(out.Metrics, diffMetricDTO{Key: m.Key, A: m.A, B: m.B, Delta: m.Delta})
	}
	for _, k := range res.SKUStock {
		out.SKUStock = append(out.SKUStock, diffSKUStockDTO{
			SKU: k.SKU, A: k.A, B: k.B, Delta: k.Delta, ValueA: k.ValueA, ValueB: k.ValueB, ValueDelta: k.ValueDelta,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
