package httpapi

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Currency is the ISO 4217 code of every money field. The scenario format has
// no currency field; the flagship business trades in US dollars.
const Currency = "USD"

// MaxSeed keeps seeds exact in JavaScript clients (openapi.yaml, Seed).
const MaxSeed = 1<<53 - 1

// ---- numeric code <-> string wire encodings ----

func supplierRoleName(code uint8) string {
	switch code {
	case sim.SupplierPrimary:
		return "primary"
	case sim.SupplierBackup:
		return "backup"
	}
	return ""
}

// supplierRoleCode returns an out-of-range code for an unknown name, which
// the validator answers with unknown_supplier.
func supplierRoleCode(name string) uint8 {
	switch name {
	case "primary":
		return sim.SupplierPrimary
	case "backup":
		return sim.SupplierBackup
	}
	return 255
}

func disruptionKindName(code uint8) string {
	switch code {
	case sim.DisruptionDemandSurge:
		return "demand_surge"
	case sim.DisruptionSupplierOutage:
		return "supplier_outage"
	case sim.DisruptionWorkforceAbsence:
		return "workforce_absence"
	}
	return fmt.Sprintf("unknown_%d", code)
}

func disruptionOriginName(code uint8) string {
	if code == sim.OriginInjected {
		return "injected"
	}
	return "script"
}

func windowReasonName(r sim.WindowReason) string {
	switch r {
	case sim.WindowCadence:
		return "cadence"
	case sim.WindowDisruptionOnset:
		return "disruption_onset"
	case sim.WindowFork:
		return "fork"
	}
	return fmt.Sprintf("unknown_%d", uint8(r))
}

func windowReasons(rs []sim.WindowReason) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, windowReasonName(r))
	}
	return out
}

func causeKindName(c uint8) string {
	switch c {
	case sim.CauseOpening:
		return "opening"
	case sim.CauseShipment:
		return "shipment"
	case sim.CauseReceipt:
		return "receipt"
	case sim.CauseFreight:
		return "freight"
	case sim.CauseLabor:
		return "labor"
	case sim.CauseMarketing:
		return "marketing"
	case sim.CauseOverhead:
		return "overhead"
	case sim.CauseServiceCredit:
		return "service_credit"
	}
	return fmt.Sprintf("unknown_%d", c)
}

// accountByName returns the account with the wire name s (for example
// "Cost_Of_Goods_Sold").
func accountByName(s string) (sim.AccountID, bool) {
	for _, a := range sim.Accounts() {
		if a.String() == s {
			return a, true
		}
	}
	return 0, false
}

var commandKindByName = func() map[string]sim.CommandKind {
	m := map[string]sim.CommandKind{}
	for k := sim.CmdSetPrice; k.Name() != ""; k++ {
		m[k.Name()] = k
	}
	return m
}()

func managerKindValid(k string) bool {
	switch app.ManagerKind(k) {
	case app.ManagerHuman, app.ManagerAIRuleBased, app.ManagerAILLM:
		return true
	}
	return false
}

func hashHex(h [32]byte) string { return hex.EncodeToString(h[:]) }

func timeString(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// ---- commands ----

// commandDTO is the wire form of a Command. Pointers distinguish an absent
// field from zero: an absent sku becomes -1, which the validator rejects with
// unknown_sku instead of silently addressing SKU 0.
type commandDTO struct {
	Kind        string `json:"kind"`
	SKU         *int64 `json:"sku,omitempty"`
	Role        string `json:"role,omitempty"`
	Qty         *int64 `json:"qty,omitempty"`
	AmountCents *int64 `json:"amountCents,omitempty"`
	PORef       *int64 `json:"poRef,omitempty"`
}

func val(p *int64, def int64) int64 {
	if p == nil {
		return def
	}
	return *p
}

// toSim converts a wire command. An unknown kind or a negative poRef is a
// request-level validation issue; every parameter problem is left to the
// deterministic validator.
func (c commandDTO) toSim(field string) (sim.Command, *ProblemIssue) {
	k, ok := commandKindByName[c.Kind]
	if !ok {
		is := issue(field+".kind", "unknown command kind %q", c.Kind)
		return sim.Command{}, &is
	}
	if c.PORef != nil && *c.PORef < 0 {
		is := issue(field+".poRef", "must not be negative")
		return sim.Command{}, &is
	}
	cmd := sim.Command{Kind: k, SKU: val(c.SKU, -1), Role: supplierRoleCode(c.Role), Qty: val(c.Qty, 0), AmountCents: sim.Cents(val(c.AmountCents, 0))}
	if c.PORef != nil {
		cmd.PORef = uint64(*c.PORef)
	}
	return cmd, nil
}

func i64(v int64) *int64 { return &v }

// commandToDTO renders only the fields that the kind reads.
func commandToDTO(c sim.Command) commandDTO {
	d := commandDTO{Kind: c.Kind.Name()}
	switch c.Kind {
	case sim.CmdSetPrice:
		d.SKU, d.AmountCents = i64(c.SKU), i64(int64(c.AmountCents))
	case sim.CmdPlacePurchaseOrder:
		d.SKU, d.Role, d.Qty = i64(c.SKU), supplierRoleName(c.Role), i64(c.Qty)
	case sim.CmdExpeditePurchaseOrder:
		d.PORef = i64(int64(c.PORef))
	case sim.CmdScheduleOvertime, sim.CmdHireTempWorkers:
		d.Qty = i64(c.Qty)
	case sim.CmdSetMarketingSpend:
		d.SKU, d.AmountCents = i64(c.SKU), i64(int64(c.AmountCents))
	}
	return d
}

type validationResultDTO struct {
	Index   int        `json:"index"`
	Command commandDTO `json:"command"`
	OK      bool       `json:"ok"`
	Code    string     `json:"code,omitempty"`
	Message string     `json:"message,omitempty"`
}

type acceptedCommandDTO struct {
	Seq           uint64     `json:"seq"`
	EffectiveTick int64      `json:"effectiveTick"`
	Command       commandDTO `json:"command"`
	Provenance    string     `json:"provenance"`
}

type submitResultDTO struct {
	Accepted        []acceptedCommandDTO  `json:"accepted"`
	Rejected        []validationResultDTO `json:"rejected"`
	BudgetRemaining int64                 `json:"budgetRemaining"`
	Duplicate       bool                  `json:"duplicate"`
}

func submitResultToDTO(res app.SubmitResult) (submitResultDTO, error) {
	out := submitResultDTO{
		Accepted: make([]acceptedCommandDTO, 0, len(res.Accepted)), Rejected: make([]validationResultDTO, 0, len(res.Rejected)),
		BudgetRemaining: max(res.BudgetRemaining, 0), Duplicate: res.Duplicate,
	}
	for _, rec := range res.Accepted {
		c, err := sim.DecodeCommand(rec.Payload)
		if err != nil {
			return submitResultDTO{}, fmt.Errorf("decode stored command %d: %w", rec.Seq, err)
		}
		out.Accepted = append(out.Accepted, acceptedCommandDTO{
			Seq: rec.Seq, EffectiveTick: int64(rec.EffectiveTick), Command: commandToDTO(c), Provenance: string(rec.Metadata.Provenance),
		})
	}
	for _, v := range res.Rejected {
		out.Rejected = append(out.Rejected, validationResultDTO{
			Index: v.Index, Command: commandToDTO(v.Command), OK: false, Code: string(v.Code), Message: v.Message,
		})
	}
	return out, nil
}

// ---- worlds and branches ----

type scoreWeightsDTO struct {
	FinancialWeightBps  int64 `json:"financialWeightBps"`
	FulfilmentWeightBps int64 `json:"fulfilmentWeightBps"`
	OnTimeWeightBps     int64 `json:"onTimeWeightBps"`
}

type skuSummaryDTO struct {
	Index               int    `json:"index"`
	ID                  string `json:"id"`
	ReferencePriceCents int64  `json:"referencePriceCents"`
	OpeningUnits        int64  `json:"openingUnits"`
	PromiseTicks        int64  `json:"promiseTicks"`
}

// scenarioSummaryDTO is the public part of a scenario. It has no field for the
// Disruption_Script, so the script cannot leak by accident.
type scenarioSummaryDTO struct {
	Name                 string          `json:"name"`
	HorizonTicks         int64           `json:"horizonTicks"`
	StartClock           string          `json:"startClock"`
	DecisionCadenceTicks int64           `json:"decisionCadenceTicks"`
	CommandBudget        int64           `json:"commandBudget"`
	SKUs                 []skuSummaryDTO `json:"skus"`
	ScoreWeights         scoreWeightsDTO `json:"scoreWeights"`
}

type assumptionDTO struct {
	Path       string `json:"path"`
	Value      int64  `json:"value"`
	Unit       string `json:"unit"`
	Default    int64  `json:"default"`
	Min        int64  `json:"min"`
	Max        int64  `json:"max"`
	Rationale  string `json:"rationale"`
	Overridden bool   `json:"overridden"`
}

type worldDTO struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Seed          uint64             `json:"seed"`
	EngineVersion string             `json:"engineVersion"`
	ScenarioHash  string             `json:"scenarioHash"`
	CreatedAt     string             `json:"createdAt"`
	Currency      string             `json:"currency"`
	Scenario      scenarioSummaryDTO `json:"scenario"`
	Assumptions   []assumptionDTO    `json:"assumptions"`
}

func worldToDTO(w app.World, sc sim.Scenario) worldDTO {
	sum := scenarioSummaryDTO{
		Name: sc.Name, HorizonTicks: sc.HorizonTicks, StartClock: sc.StartClock,
		DecisionCadenceTicks: sc.DecisionCadenceTicks, CommandBudget: sc.CommandBudget,
		SKUs: make([]skuSummaryDTO, 0, len(sc.SKUs)),
		ScoreWeights: scoreWeightsDTO{
			FinancialWeightBps: int64(sc.ScoreWeights.FinancialWeightBps), FulfilmentWeightBps: int64(sc.ScoreWeights.FulfilmentWeightBps),
			OnTimeWeightBps: int64(sc.ScoreWeights.OnTimeWeightBps),
		},
	}
	for i, k := range sc.SKUs {
		sum.SKUs = append(sum.SKUs, skuSummaryDTO{Index: i, ID: k.ID, ReferencePriceCents: int64(k.ReferencePriceCents), OpeningUnits: k.OpeningUnits, PromiseTicks: k.PromiseTicks})
	}
	as := sim.EffectiveAssumptions(&sc)
	out := worldDTO{
		ID: string(w.ID), Name: w.Name, Seed: uint64(w.Seed), EngineVersion: w.EngineVersion,
		ScenarioHash: hashHex(w.ScenarioHash), CreatedAt: timeString(w.CreatedAt), Currency: Currency,
		Scenario: sum, Assumptions: make([]assumptionDTO, 0, len(as)),
	}
	for _, a := range as {
		out.Assumptions = append(out.Assumptions, assumptionDTO{
			Path: a.Path, Value: a.Value, Unit: a.Unit, Default: a.Default, Min: a.Min, Max: a.Max, Rationale: a.Rationale, Overridden: a.Overridden,
		})
	}
	return out
}

type branchDTO struct {
	ID             string  `json:"id"`
	WorldID        string  `json:"worldId"`
	ParentBranchID *string `json:"parentBranchId"`
	ForkTick       int64   `json:"forkTick"`
	HeadTick       int64   `json:"headTick"`
	Label          string  `json:"label"`
	ManagerKind    string  `json:"managerKind"`
	CreatedAt      string  `json:"createdAt"`
}

func branchToDTO(b app.Branch) branchDTO {
	d := branchDTO{
		ID: string(b.ID), WorldID: string(b.WorldID), ForkTick: int64(b.ForkTick), HeadTick: int64(b.HeadTick),
		Label: b.Label, ManagerKind: string(b.ManagerKind), CreatedAt: timeString(b.CreatedAt),
	}
	if b.ParentBranchID != "" {
		p := string(b.ParentBranchID)
		d.ParentBranchID = &p
	}
	return d
}

// ---- metrics and frames ----

type reconciliationDTO struct {
	OpeningInventoryCents   int64 `json:"openingInventoryCents"`
	InventoryChangeCents    int64 `json:"inventoryChangeCents"`
	ProfitLessCashFlowCents int64 `json:"profitLessCashFlowCents"`
	AccrualToCashHolds      bool  `json:"accrualToCashHolds"`
	OpeningEquityCents      int64 `json:"openingEquityCents"`
	BalanceIdentityHolds    bool  `json:"balanceIdentityHolds"`
}

type metricsDTO struct {
	Tick                      int64             `json:"tick"`
	NetRevenueCents           int64             `json:"netRevenueCents"`
	CostOfGoodsSoldCents      int64             `json:"costOfGoodsSoldCents"`
	FreightExpenseCents       int64             `json:"freightExpenseCents"`
	VariableCostCents         int64             `json:"variableCostCents"`
	ContributionCents         int64             `json:"contributionCents"`
	LaborExpenseCents         int64             `json:"laborExpenseCents"`
	MarketingExpenseCents     int64             `json:"marketingExpenseCents"`
	OverheadExpenseCents      int64             `json:"overheadExpenseCents"`
	ServiceCreditExpenseCents int64             `json:"serviceCreditExpenseCents"`
	NetProfitCents            int64             `json:"netProfitCents"`
	NetCashFlowCents          int64             `json:"netCashFlowCents"`
	CashCents                 int64             `json:"cashCents"`
	InventoryValueCents       int64             `json:"inventoryValueCents"`
	OrdersDue                 int64             `json:"ordersDue"`
	OrdersDueOnTime           int64             `json:"ordersDueOnTime"`
	UnitsDue                  int64             `json:"unitsDue"`
	UnitsDueShipped           int64             `json:"unitsDueShipped"`
	ServiceLevelBps           int64             `json:"serviceLevelBps"`
	FillRateBps               int64             `json:"fillRateBps"`
	BacklogOrders             int64             `json:"backlogOrders"`
	BacklogUnits              int64             `json:"backlogUnits"`
	LateBacklogOrders         int64             `json:"lateBacklogOrders"`
	LateBacklogUnits          int64             `json:"lateBacklogUnits"`
	Reconciliation            reconciliationDTO `json:"reconciliation"`
}

func metricsToDTO(m sim.Metrics) metricsDTO {
	r := m.Reconciliation
	return metricsDTO{
		Tick: int64(m.Tick), NetRevenueCents: int64(m.NetRevenue), CostOfGoodsSoldCents: int64(m.CostOfGoodsSold),
		FreightExpenseCents: int64(m.FreightExpense), VariableCostCents: int64(m.VariableCost), ContributionCents: int64(m.Contribution),
		LaborExpenseCents: int64(m.LaborExpense), MarketingExpenseCents: int64(m.MarketingExpense), OverheadExpenseCents: int64(m.OverheadExpense),
		ServiceCreditExpenseCents: int64(m.ServiceCreditExpense), NetProfitCents: int64(m.NetProfit), NetCashFlowCents: int64(m.NetCashFlow),
		CashCents: int64(m.Cash), InventoryValueCents: int64(m.InventoryValue),
		OrdersDue: m.OrdersDue, OrdersDueOnTime: m.OrdersDueOnTime, UnitsDue: m.UnitsDue, UnitsDueShipped: m.UnitsDueShipped,
		ServiceLevelBps: int64(m.ServiceLevel), FillRateBps: int64(m.FillRate),
		BacklogOrders: m.BacklogOrders, BacklogUnits: m.BacklogUnits, LateBacklogOrders: m.LateBacklogOrders, LateBacklogUnits: m.LateBacklogUnits,
		Reconciliation: reconciliationDTO{
			OpeningInventoryCents: int64(r.OpeningInventory), InventoryChangeCents: int64(r.InventoryChange),
			ProfitLessCashFlowCents: int64(r.ProfitLessCashFlow), AccrualToCashHolds: r.AccrualToCashHolds,
			OpeningEquityCents: int64(r.OpeningEquity), BalanceIdentityHolds: r.BalanceIdentityHolds,
		},
	}
}

type skuFrameDTO struct {
	SKU          int64 `json:"sku"`
	OnHand       int64 `json:"onHand"`
	InboundPOs   int64 `json:"inboundPOs"`
	InboundUnits int64 `json:"inboundUnits"`
}

type disruptionFrameDTO struct {
	Kind            string `json:"kind"`
	Origin          string `json:"origin"`
	InputSeq        uint64 `json:"inputSeq"`
	Target          int64  `json:"target"`
	MagnitudeBps    int64  `json:"magnitudeBps"`
	OnsetTick       int64  `json:"onsetTick"`
	EndTick         int64  `json:"endTick"`
	TicksSinceOnset int64  `json:"ticksSinceOnset"`
}

type skuConstraintDTO struct {
	SKU                 int64 `json:"sku"`
	StockBoundOrders    int64 `json:"stockBoundOrders"`
	StockBoundUnits     int64 `json:"stockBoundUnits"`
	CapacityBoundOrders int64 `json:"capacityBoundOrders"`
	CapacityBoundUnits  int64 `json:"capacityBoundUnits"`
}

type reportDTO struct {
	Tick          int64              `json:"tick"`
	Capacity      int64              `json:"capacity"`
	CapacityUsed  int64              `json:"capacityUsed"`
	ShippedOrders int64              `json:"shippedOrders"`
	SKUs          []skuConstraintDTO `json:"skus"`
}

type markerDTO struct {
	Kind           string `json:"kind"`
	WindowReason   string `json:"windowReason,omitempty"`
	DisruptionKind string `json:"disruptionKind,omitempty"`
}

type tickFrameDTO struct {
	Tick            int64                `json:"tick"`
	StateHash       string               `json:"stateHash"`
	SKUs            []skuFrameDTO        `json:"skus"`
	WorkersOnShift  int64                `json:"workersOnShift"`
	TempsOnShift    int64                `json:"tempsOnShift"`
	TempsOnboarding int64                `json:"tempsOnboarding"`
	OrderQueueLen   int64                `json:"orderQueueLen"`
	Disruptions     []disruptionFrameDTO `json:"disruptions"`
	Report          reportDTO            `json:"report"`
	Markers         []markerDTO          `json:"markers"`
	Metrics         metricsDTO           `json:"metrics"`
}

func frameToDTO(f sim.TickFrame) tickFrameDTO {
	d := tickFrameDTO{
		Tick: int64(f.Tick), StateHash: hashHex(f.Hash), SKUs: make([]skuFrameDTO, 0, len(f.SKUs)),
		WorkersOnShift: f.WorkersOnShift, TempsOnShift: f.TempsOnShift, TempsOnboarding: f.TempsOnboarding, OrderQueueLen: f.OrderQueueLen,
		Disruptions: make([]disruptionFrameDTO, 0, len(f.Disruptions)), Markers: make([]markerDTO, 0, len(f.Markers)),
		Metrics: metricsToDTO(f.Metrics),
		Report: reportDTO{
			Tick: int64(f.Report.Tick), Capacity: f.Report.Capacity, CapacityUsed: f.Report.CapacityUsed,
			ShippedOrders: f.Report.ShippedOrders, SKUs: make([]skuConstraintDTO, 0, len(f.Report.SKUs)),
		},
	}
	for _, s := range f.SKUs {
		d.SKUs = append(d.SKUs, skuFrameDTO{SKU: s.SKU, OnHand: s.OnHand, InboundPOs: s.InboundPOs, InboundUnits: s.InboundUnits})
	}
	for _, x := range f.Disruptions {
		d.Disruptions = append(d.Disruptions, disruptionFrameDTO{
			Kind: disruptionKindName(x.Kind), Origin: disruptionOriginName(x.Origin), InputSeq: x.InputSeq, Target: x.Target,
			MagnitudeBps: int64(x.MagnitudeBps), OnsetTick: int64(sim.TickOf(x.Onset)), EndTick: int64(x.End) / sim.MinutesPerTick,
			TicksSinceOnset: x.TicksSinceOnset,
		})
	}
	for _, c := range f.Report.SKUs {
		d.Report.SKUs = append(d.Report.SKUs, skuConstraintDTO{
			SKU: c.SKU, StockBoundOrders: c.StockBoundOrders, StockBoundUnits: c.StockBoundUnits,
			CapacityBoundOrders: c.CapacityBoundOrders, CapacityBoundUnits: c.CapacityBoundUnits,
		})
	}
	for _, m := range f.Markers {
		switch m.Kind {
		case sim.MarkerDecisionWindow:
			d.Markers = append(d.Markers, markerDTO{Kind: "decision_window", WindowReason: windowReasonName(sim.WindowReason(m.Ref))})
		case sim.MarkerDisruptionOnset:
			d.Markers = append(d.Markers, markerDTO{Kind: "disruption_onset", DisruptionKind: disruptionKindName(uint8(m.Ref))})
		}
	}
	return d
}

// ---- observation ----

type obsSupplierDTO struct {
	Role              string `json:"role"`
	UnitCostCents     int64  `json:"unitCostCents"`
	LeadTimeTicks     int64  `json:"leadTimeTicks"`
	Available         bool   `json:"available"`
	AvailableFromTick int64  `json:"availableFromTick"`
}

// priceBoundsDTO is the allowed set_price range of one product.
type priceBoundsDTO struct {
	MinCents int64 `json:"minCents"`
	MaxCents int64 `json:"maxCents"`
}

type obsSKUDTO struct {
	SKU                 int64            `json:"sku"`
	ID                  string           `json:"id"`
	OnHand              int64            `json:"onHand"`
	InventoryValueCents int64            `json:"inventoryValueCents"`
	ReferencePriceCents int64            `json:"referencePriceCents"`
	PriceBounds         priceBoundsDTO   `json:"priceBounds"`
	PriceCents          int64            `json:"priceCents"`
	MarketingCents      int64            `json:"marketingCents"`
	InboundPOs          int64            `json:"inboundPOs"`
	InboundUnits        int64            `json:"inboundUnits"`
	Suppliers           []obsSupplierDTO `json:"suppliers"`
}

type obsOrderDTO struct {
	ID         uint64 `json:"id"`
	SKU        int64  `json:"sku"`
	Qty        int64  `json:"qty"`
	PromisedBy int64  `json:"promisedBy"`
}

type obsPurchaseOrderDTO struct {
	ID            uint64 `json:"id"`
	SKU           int64  `json:"sku"`
	Supplier      string `json:"supplier"`
	Qty           int64  `json:"qty"`
	UnitCostCents int64  `json:"unitCostCents"`
	DueAt         int64  `json:"dueAt"`
	Expedited     bool   `json:"expedited"`
}

type obsWorkforceDTO struct {
	Regular         int64 `json:"regular"`
	OvertimeHours   int64 `json:"overtimeHours"`
	WorkersOnShift  int64 `json:"workersOnShift"`
	TempsOnShift    int64 `json:"tempsOnShift"`
	TempsOnboarding int64 `json:"tempsOnboarding"`
}

type obsDisruptionDTO struct {
	Kind         string `json:"kind"`
	Origin       string `json:"origin"`
	InputSeq     uint64 `json:"inputSeq"`
	Target       int64  `json:"target"`
	SupplierRole string `json:"supplierRole,omitempty"`
	MagnitudeBps int64  `json:"magnitudeBps"`
	OnsetTick    int64  `json:"onsetTick"`
	EndTick      int64  `json:"endTick"`
}

type obsWindowDTO struct {
	Open           bool     `json:"open"`
	Reasons        []string `json:"reasons"`
	CadenceTicks   int64    `json:"cadenceTicks"`
	CommandBudget  int64    `json:"commandBudget"`
	HorizonTicks   int64    `json:"horizonTicks"`
	TicksRemaining int64    `json:"ticksRemaining"`
}

type observationDTO struct {
	Tick           int64                 `json:"tick"`
	CashCents      int64                 `json:"cashCents"`
	SKUs           []obsSKUDTO           `json:"skus"`
	OpenOrders     []obsOrderDTO         `json:"openOrders"`
	PurchaseOrders []obsPurchaseOrderDTO `json:"purchaseOrders"`
	Workforce      obsWorkforceDTO       `json:"workforce"`
	Disruptions    []obsDisruptionDTO    `json:"disruptions"`
	Metrics        metricsDTO            `json:"metrics"`
	Window         obsWindowDTO          `json:"window"`
}

func observationToDTO(o sim.Observation) observationDTO {
	d := observationDTO{
		Tick: int64(o.Tick), CashCents: int64(o.Cash), SKUs: make([]obsSKUDTO, 0, len(o.SKUs)),
		OpenOrders: make([]obsOrderDTO, 0, len(o.OpenOrders)), PurchaseOrders: make([]obsPurchaseOrderDTO, 0, len(o.PurchaseOrders)),
		Disruptions: make([]obsDisruptionDTO, 0, len(o.Disruptions)), Metrics: metricsToDTO(o.Metrics),
		Workforce: obsWorkforceDTO{
			Regular: o.Workforce.Regular, OvertimeHours: o.Workforce.OvertimeHours, WorkersOnShift: o.Workforce.WorkersOnShift,
			TempsOnShift: o.Workforce.TempsOnShift, TempsOnboarding: o.Workforce.TempsOnboarding,
		},
		Window: obsWindowDTO{
			Open: o.Window.Open, Reasons: windowReasons(o.Window.Reasons), CadenceTicks: o.Window.CadenceTicks,
			CommandBudget: o.Window.CommandBudget, HorizonTicks: o.Window.HorizonTicks, TicksRemaining: o.Window.TicksRemaining,
		},
	}
	for _, s := range o.SKUs {
		x := obsSKUDTO{
			SKU: s.SKU, ID: s.ID, OnHand: s.OnHand, InventoryValueCents: int64(s.InventoryValue), ReferencePriceCents: int64(s.ReferencePriceCents),
			PriceBounds: priceBoundsDTO{MinCents: int64(s.MinPriceCents), MaxCents: int64(s.MaxPriceCents)},
			PriceCents:  int64(s.PriceCents), MarketingCents: int64(s.MarketingCents), InboundPOs: s.InboundPOs, InboundUnits: s.InboundUnits,
			Suppliers: make([]obsSupplierDTO, 0, len(s.Suppliers)),
		}
		for _, u := range s.Suppliers {
			x.Suppliers = append(x.Suppliers, obsSupplierDTO{
				Role: supplierRoleName(u.Role), UnitCostCents: int64(u.UnitCostCents), LeadTimeTicks: u.LeadTimeTicks,
				Available: u.Available, AvailableFromTick: int64(u.AvailableFromTick),
			})
		}
		d.SKUs = append(d.SKUs, x)
	}
	for _, od := range o.OpenOrders {
		d.OpenOrders = append(d.OpenOrders, obsOrderDTO{ID: od.ID, SKU: od.SKU, Qty: od.Qty, PromisedBy: int64(od.PromisedBy)})
	}
	for _, po := range o.PurchaseOrders {
		d.PurchaseOrders = append(d.PurchaseOrders, obsPurchaseOrderDTO{
			ID: po.ID, SKU: po.SKU, Supplier: supplierRoleName(po.Supplier), Qty: po.Qty, UnitCostCents: int64(po.UnitCost),
			DueAt: int64(po.DueAt), Expedited: po.Expedited,
		})
	}
	for _, x := range o.Disruptions {
		od := obsDisruptionDTO{
			Kind: disruptionKindName(x.Kind), Origin: disruptionOriginName(x.Origin), InputSeq: x.InputSeq, Target: x.Target,
			MagnitudeBps: int64(x.MagnitudeBps), OnsetTick: int64(x.OnsetTick), EndTick: int64(x.EndTick),
		}
		if x.Kind == sim.DisruptionSupplierOutage {
			od.SupplierRole = supplierRoleName(x.Role)
		}
		d.Disruptions = append(d.Disruptions, od)
	}
	return d
}

// ---- ledger ----

type ledgerLineDTO struct {
	Account     string `json:"account"`
	DebitCents  int64  `json:"debitCents"`
	CreditCents int64  `json:"creditCents"`
}

type causeRefDTO struct {
	Kind string `json:"kind"`
	Ref  int64  `json:"ref"`
}

type ledgerEntryDTO struct {
	Index       int             `json:"index"`
	Tick        int64           `json:"tick"`
	SimMinute   int64           `json:"simMinute"`
	Description string          `json:"description"`
	Cause       causeRefDTO     `json:"cause"`
	Lines       []ledgerLineDTO `json:"lines"`
}

func ledgerEntryToDTO(index int, e sim.LedgerEntry) ledgerEntryDTO {
	d := ledgerEntryDTO{
		Index: index, Tick: int64(e.Tick), SimMinute: int64(e.At), Description: e.Description(),
		Cause: causeRefDTO{Kind: causeKindName(e.Cause), Ref: e.CauseRef}, Lines: make([]ledgerLineDTO, 0, len(e.Lines)),
	}
	for _, l := range e.Lines {
		d.Lines = append(d.Lines, ledgerLineDTO{Account: l.Account.String(), DebitCents: int64(l.Debit), CreditCents: int64(l.Credit)})
	}
	return d
}
