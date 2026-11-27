package sim

import (
	"errors"
	"fmt"
)

// ErrMarketingLimit is returned when the engine applies a marketing spend
// above Demand.MaxMarketingSpendCentsPerHour.
var ErrMarketingLimit = errors.New("sim: marketing spend exceeds the per-hour maximum")

// SetMarketingSpend sets the marketing spend per Tick of one SKU from the
// effective Tick until changed; 0 stops it (Req 7.3, 10.9). The spend is
// posted as Marketing_Expense every Tick by marketingAccrual and raises the
// conversion of Potential_Arrivals through conversionBps. It reads no random
// stream, so the demand stream state is unaffected.
func SetMarketingSpend(sc *Scenario, st *State, sku int64, amount Cents) error {
	if amount < 0 {
		return fmt.Errorf("%w: %d cents", ErrBadMarketing, int64(amount))
	}
	if amount > sc.Demand.MaxMarketingSpendCentsPerHour {
		return fmt.Errorf("%w: %d > %d", ErrMarketingLimit, int64(amount), int64(sc.Demand.MaxMarketingSpendCentsPerHour))
	}
	for i := range st.Prices {
		if st.Prices[i].SKU == sku {
			st.Prices[i].MarketingCents = amount
			return nil
		}
	}
	return fmt.Errorf("%w: %d", ErrUnknownSKU, sku)
}

// ErrBadMarketing is the error for a negative marketing amount.
var ErrBadMarketing = errors.New("sim: marketing spend is invalid")

// validateMarketing checks a set_marketing_spend Command: the SKU exists, the
// amount is within 0..Demand.MaxMarketingSpendCentsPerHour, and the first
// Tick's spend is affordable. It returns that spend as the committed cash.
func validateMarketing(sc Scenario, obs Observation, c Command, available int64) (RejectCode, string, int64) {
	sku, ok := obs.sku(c.SKU)
	if !ok {
		return ReasonUnknownSKU, fmt.Sprintf("sku %d does not exist", c.SKU), 0
	}
	max := int64(sc.Demand.MaxMarketingSpendCentsPerHour)
	if c.AmountCents < 0 {
		return ReasonInvalidQuantity, fmt.Sprintf("marketing spend %d cents is below 0", int64(c.AmountCents)), 0
	}
	if int64(c.AmountCents) > max {
		return ReasonMarketingExceedsLimit, fmt.Sprintf("marketing spend of %d cents per hour for %s is above the limit of %d", int64(c.AmountCents), sku.ID, max), 0
	}
	if int64(c.AmountCents) > available {
		return ReasonInsufficientCash, fmt.Sprintf("first hour of marketing is %d cents but %d cents are available", int64(c.AmountCents), available), 0
	}
	return "", "", int64(c.AmountCents)
}
