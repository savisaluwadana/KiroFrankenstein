package sim

import "fmt"

// Demand model constants for engine version 1.
const (
	// DemandDrawsPerSKUPerTick is the number of demand-stream draws consumed
	// per SKU per Tick. It is fixed and never depends on commands, prices or
	// disruptions (common random numbers, Req 7.2, INV-12).
	DemandDrawsPerSKUPerTick = 1
)

// hourOfDay returns the hour of day (0..23) of sim minute at. Sim minute 0 is
// 00:00 on the scenario's first day.
func hourOfDay(at SimMinute) int {
	return int((int64(at) / MinutesPerTick) % HourlyProfileLen)
}

// surgeBps returns the product, in basis points, of the magnitudes of every
// active demand_surge that applies to sku (10000 when none). A Target of -1
// applies to every SKU.
func surgeBps(ds []ActiveDisruption, sku int64) Bps {
	mult := Bps(BpsScale)
	for _, d := range ds {
		if d.Kind != DisruptionDemandSurge || (d.Target != -1 && d.Target != sku) {
			continue
		}
		mult = Bps(MulDiv(int64(mult), int64(d.MagnitudeBps), BpsScale))
	}
	return mult
}

// potentialArrivals is a pure function of the scenario, the hour profile, the
// surge multiplier and one raw stream draw:
// base * profile * surge * (1 + noise), with a single half-to-even rounding.
// The noise is uniform in +/- d.NoiseBps; the draw is consumed regardless.
func potentialArrivals(d DemandSpec, hour int, surge Bps, draw uint64) int64 {
	amp := int64(d.NoiseBps)
	noise := int64(draw%uint64(2*amp+1)) - amp
	a := d.BaseArrivalsPerTick * int64(d.HourlyProfile[hour])
	b := int64(surge) * (BpsScale + noise)
	return MulDiv(a, b, BpsScale*BpsScale*BpsScale)
}

// conversionBps returns the share of Potential_Arrivals that become orders:
// base - elasticity * (price-ref)/ref + marketing uplift, clamped to
// [0, 10000] (Req 7.3). It reads prices and spend, never the random stream.
func conversionBps(d DemandSpec, sku SKUSpec, price, marketing Cents) Bps {
	priceDelta := MulDiv(int64(price-sku.ReferencePriceCents), BpsScale, int64(sku.ReferencePriceCents))
	priceEffect := MulDiv(priceDelta, int64(d.PriceElasticityBps), BpsScale)
	uplift := MulDiv(int64(marketing), int64(d.MarketingEffectBps), int64(d.MarketingSpendUnitCents))
	c := int64(d.BaseConversionBps) - priceEffect + uplift
	if c < 0 {
		c = 0
	}
	if c > BpsScale {
		c = BpsScale
	}
	return Bps(c)
}

// demandArrivals handles EvDemandArrivals: for each SKU in index order it
// consumes exactly DemandDrawsPerSKUPerTick draws, derives Potential_Arrivals,
// converts them deterministically and creates the orders.
func (e *stepper) demandArrivals(ev Event) error {
	ds, err := e.stream(StreamDemand)
	if err != nil {
		return err
	}
	hour := hourOfDay(ev.At)
	for i, spec := range e.sc.SKUs {
		sku := int64(i)
		draw := ds.Next() // the only draw for this SKU; always taken
		surge := surgeBps(e.st.Disruptions, sku)
		potential := potentialArrivals(e.sc.Demand, hour, surge, draw)

		pr, ok := e.st.priceOf(sku)
		if !ok {
			return fmt.Errorf("sim: state has no price for sku %d", sku)
		}
		conv := conversionBps(e.sc.Demand, spec, pr.PriceCents, pr.MarketingCents)
		n := MulDiv(potential, int64(conv), BpsScale)
		e.createOrders(sku, n, pr.PriceCents, ev.At)
		e.journal = append(e.journal, Outcome{At: ev.At, Kind: OutcomeDemand, SKU: sku, Ref: int64(conv), Qty: potential, Aux: n})
	}
	return nil
}

// priceOf returns the SKUPrice of sku.
func (s State) priceOf(sku int64) (SKUPrice, bool) {
	for _, p := range s.Prices {
		if p.SKU == sku {
			return p, true
		}
	}
	return SKUPrice{}, false
}
