/**
 * Plain-language names for assumption paths. The paths stay the identity of an
 * assumption (they are what overrides are keyed by); the label is display only.
 */
import { productName, supplierRoleName } from "@/lib/glossary";

const LABELS: Record<string, string> = {
  horizonTicks: "Length of the week",
  decisionCadenceTicks: "Hours between decision points",
  commandBudget: "Decisions allowed per decision point",
  snapshotIntervalTicks: "Hours between saved checkpoints",
  tieToleranceHundredths: "Score gap counted as a tie",
  "scoreWeights.financialWeightBps": "Score weight: profit",
  "scoreWeights.fulfilmentWeightBps": "Score weight: orders filled",
  "scoreWeights.onTimeWeightBps": "Score weight: orders on time",
  profitFloorCents: "Profit that scores zero",
  profitTargetCents: "Profit that scores full marks",
  openingCashCents: "Starting cash",
  "workforce.regularWorkers": "Packing staff",
  "workforce.unitsPerWorkerHour": "Packing speed per person",
  "workforce.maxOvertimePerWorker": "Most overtime hours per person",
  "workforce.maxTempWorkers": "Most temporary workers",
  "workforce.tempOnboardingTicks": "Hours before a temp can work",
  "wages.regularPerHourCents": "Regular wage per hour",
  "wages.overtimePerHourCents": "Overtime wage per hour",
  "wages.tempPerHourCents": "Temporary worker wage per hour",
  "costs.outboundShipCentsPerUnit": "Shipping cost per item",
  "costs.expediteFeeCents": "Rush delivery fee",
  "costs.expediteReductionTicks": "Hours saved by a rush delivery",
  "costs.hourlyOverheadCents": "Overhead per hour",
  "costs.serviceCreditPerLateOrderCents": "Refund per late order",
  "demand.baseArrivalsPerTick": "Shoppers arriving per hour",
  "demand.baseConversionBps": "Share of shoppers who buy",
  "demand.priceElasticityBps": "How strongly price changes demand",
  "demand.marketingEffectBps": "How much marketing lifts buying",
  "demand.cancelThresholdTicks": "Hours before a waiting customer cancels",
  "demand.noiseBps": "Hour-to-hour randomness in demand",
  "demand.marketingSpendUnitCents": "Marketing spend per step of effect",
  "demand.maxMarketingSpendCentsPerHour": "Most marketing spend per hour",
  "pricing.minPriceBps": "Lowest allowed price (share of reference)",
  "pricing.maxPriceBps": "Highest allowed price (share of reference)",
};

const SKU_FIELDS: Record<string, string> = {
  referencePriceCents: "price",
  openingUnits: "starting stock",
  openingUnitCostCents: "starting cost per item",
  promiseTicks: "delivery promise (hours)",
};

const SUPPLIER_FIELDS: Record<string, string> = {
  unitCostCents: "cost per item",
  leadTimeTicks: "delivery time (hours)",
};

/** Friendly name for an assumption path, or null when the glossary has none. */
export function assumptionLabel(path: string): string | null {
  const known = LABELS[path];
  if (known !== undefined) return known;
  const parts = path.split(".");
  if (parts[0] === "skus" && parts.length === 3) {
    const field = SKU_FIELDS[parts[2] ?? ""];
    if (field !== undefined) return `${productName(parts[1] ?? "")}: ${field}`;
  }
  if (parts[0] === "suppliers" && parts.length === 4) {
    const field = SUPPLIER_FIELDS[parts[3] ?? ""];
    const role = parts[2];
    if (field !== undefined && (role === "primary" || role === "backup")) {
      return `${productName(parts[1] ?? "")}, ${supplierRoleName(role).toLowerCase()}: ${field}`;
    }
  }
  if (path === "demand.hourlyProfile") return "Shopping pattern across the day";
  return null;
}

/** Label to show. Without a glossary entry only Engineering Mode falls back to the raw path. */
export function displayLabel(path: string, engineering: boolean): string {
  const label = assumptionLabel(path);
  if (label !== null) return label;
  return engineering ? path : "Other setting";
}
