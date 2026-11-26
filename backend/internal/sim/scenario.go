package sim

// SupportedSchemaVersion is the only Scenario schema version this engine reads.
const SupportedSchemaVersion = 1

// Scenario is the parsed, not yet validated, scenario document (design.md,
// "Scenario and assumptions"). Every numeric field is an integer: money in
// cents, rates in basis points, durations in Ticks (hours) or minutes.
// Fields declared as pointers are optional in the document.
type Scenario struct {
	SchemaVersion          int64                     `json:"schemaVersion"`
	Name                   string                    `json:"name"`
	HorizonTicks           int64                     `json:"horizonTicks"`
	StartClock             string                    `json:"startClock"`
	DecisionCadenceTicks   int64                     `json:"decisionCadenceTicks"`
	CommandBudget          int64                     `json:"commandBudget"`
	SnapshotIntervalTicks  int64                     `json:"snapshotIntervalTicks"`
	TieToleranceHundredths int64                     `json:"tieToleranceHundredths"`
	ScoreWeights           ScoreWeights              `json:"scoreWeights"`
	ProfitFloorCents       Cents                     `json:"profitFloorCents"`
	ProfitTargetCents      Cents                     `json:"profitTargetCents"`
	OpeningCashCents       Cents                     `json:"openingCashCents"`
	SKUs                   []SKUSpec                 `json:"skus"`
	Suppliers              []SupplierSpec            `json:"suppliers"`
	Workforce              WorkforceSpec             `json:"workforce"`
	Wages                  WagesSpec                 `json:"wages"`
	Costs                  CostsSpec                 `json:"costs"`
	Demand                 DemandSpec                `json:"demand"`
	AssumptionMeta         map[string]AssumptionMeta `json:"assumptionMeta"`
	DisruptionScript       []DisruptionSpec          `json:"disruptionScript"`
}

// ScoreWeights are the Composite_Score weights in basis points.
type ScoreWeights struct {
	FinancialWeightBps  Bps `json:"financialWeightBps"`
	FulfilmentWeightBps Bps `json:"fulfilmentWeightBps"`
	OnTimeWeightBps     Bps `json:"onTimeWeightBps"`
}

// SKUSpec describes one stock-keeping unit.
type SKUSpec struct {
	ID                   string `json:"id"`
	ReferencePriceCents  Cents  `json:"referencePriceCents"`
	OpeningUnits         int64  `json:"openingUnits"`
	OpeningUnitCostCents Cents  `json:"openingUnitCostCents"`
	PromiseTicks         int64  `json:"promiseTicks"`
}

// SupplierSpec describes one supplier role for one SKU.
type SupplierSpec struct {
	SKUID         string `json:"skuId"`
	Role          string `json:"role"`
	UnitCostCents Cents  `json:"unitCostCents"`
	LeadTimeTicks int64  `json:"leadTimeTicks"`
}

// WorkforceSpec holds warehouse capacity assumptions, including the temporary
// worker onboarding delay (Req 9.9).
type WorkforceSpec struct {
	RegularWorkers       int64 `json:"regularWorkers"`
	UnitsPerWorkerHour   int64 `json:"unitsPerWorkerHour"`
	MaxOvertimePerWorker int64 `json:"maxOvertimePerWorker"`
	MaxTempWorkers       int64 `json:"maxTempWorkers"`
	TempOnboardingTicks  int64 `json:"tempOnboardingTicks"`
}

// WagesSpec holds hourly wages in cents, including the temporary wage (Req 9.9).
type WagesSpec struct {
	RegularPerHourCents  Cents `json:"regularPerHourCents"`
	OvertimePerHourCents Cents `json:"overtimePerHourCents"`
	TempPerHourCents     Cents `json:"tempPerHourCents"`
}

// CostsSpec holds shipping, expedite, overhead and service-credit costs.
type CostsSpec struct {
	OutboundShipCentsPerUnit       Cents `json:"outboundShipCentsPerUnit"`
	ExpediteFeeCents               Cents `json:"expediteFeeCents"`
	ExpediteReductionTicks         int64 `json:"expediteReductionTicks"`
	HourlyOverheadCents            Cents `json:"hourlyOverheadCents"`
	ServiceCreditPerLateOrderCents Cents `json:"serviceCreditPerLateOrderCents"`
}

// DemandSpec holds the demand assumptions. HourlyProfile has one
// basis-point-of-base value per hour of day (length checked by the validator).
// BaseConversionBps is the share of Potential_Arrivals that become orders at
// the reference price with no marketing spend. MarketingEffectBps is the
// conversion uplift, in basis points, per MarketingSpendUnitCents of hourly
// marketing spend on a SKU (see demand.go).
type DemandSpec struct {
	HourlyProfile        []Bps `json:"hourlyProfile"`
	BaseArrivalsPerTick  int64 `json:"baseArrivalsPerTick"`
	BaseConversionBps    Bps   `json:"baseConversionBps"`
	PriceElasticityBps   Bps   `json:"priceElasticityBps"`
	MarketingEffectBps   Bps   `json:"marketingEffectBps"`
	CancelThresholdTicks int64 `json:"cancelThresholdTicks"`
}

// AssumptionMeta is the metadata every Assumption must carry (Req 1.7). The
// assumptionMeta map is keyed by the dotted document path of the assumption,
// for example "workforce.tempOnboardingTicks". Completeness is checked by the
// validator, not the parser.
type AssumptionMeta struct {
	Unit      string `json:"unit"`
	Default   int64  `json:"default"`
	Min       int64  `json:"min"`
	Max       int64  `json:"max"`
	Rationale string `json:"rationale"`
}

// DisruptionSpec is one entry of the Disruption_Script. MagnitudeBps and Target
// are optional because their use depends on Kind.
type DisruptionSpec struct {
	Kind          string  `json:"kind"`
	OnsetTick     int64   `json:"onsetTick"`
	DurationTicks int64   `json:"durationTicks"`
	MagnitudeBps  *Bps    `json:"magnitudeBps,omitempty"`
	Target        *string `json:"target,omitempty"`
}
