package httpapi

import (
	"fmt"
	"net/http"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// ---- outcome journal ----

func outcomeKindName(k sim.OutcomeKind) string {
	switch k {
	case sim.OutcomeDemand:
		return "demand"
	case sim.OutcomeOrderCancelled:
		return "order_cancelled"
	case sim.OutcomeShipment:
		return "shipment"
	}
	return fmt.Sprintf("unknown_%d", uint8(k))
}

// journalEntryDTO is one outcome. Only the fields of its kind are set.
type journalEntryDTO struct {
	Index     int    `json:"index"`
	Tick      int64  `json:"tick"`
	SimMinute int64  `json:"simMinute"`
	Kind      string `json:"kind"`
	SKU       int64  `json:"sku"`
	// demand
	PotentialArrivals *int64 `json:"potentialArrivals,omitempty"`
	ConversionBps     *int64 `json:"conversionBps,omitempty"`
	OrdersCreated     *int64 `json:"ordersCreated,omitempty"`
	// order_cancelled and shipment
	OrderID *int64 `json:"orderId,omitempty"`
	Units   *int64 `json:"units,omitempty"`
	// order_cancelled
	AgeMinutes *int64 `json:"ageMinutes,omitempty"`
	// shipment
	MinutesLate *int64 `json:"minutesLate,omitempty"`
}

func journalEntryToDTO(index int, e app.JournalEntry) journalEntryDTO {
	o := e.Outcome
	d := journalEntryDTO{Index: index, Tick: int64(e.Tick), SimMinute: int64(o.At), Kind: outcomeKindName(o.Kind), SKU: o.SKU}
	switch o.Kind {
	case sim.OutcomeDemand:
		d.PotentialArrivals, d.ConversionBps, d.OrdersCreated = i64(o.Qty), i64(o.Ref), i64(o.Aux)
	case sim.OutcomeOrderCancelled:
		d.OrderID, d.Units, d.AgeMinutes = i64(o.Ref), i64(o.Qty), i64(o.Aux)
	case sim.OutcomeShipment:
		d.OrderID, d.Units, d.MinutesLate = i64(o.Ref), i64(o.Qty), i64(o.Aux)
	}
	return d
}

type journalResponse struct {
	BranchID   string            `json:"branchId"`
	From       int64             `json:"from"`
	To         int64             `json:"to"`
	Total      int               `json:"total"`
	Offset     int64             `json:"offset"`
	Entries    []journalEntryDTO `json:"entries"`
	Currency   string            `json:"currency"`
	Disclaimer string            `json:"disclaimer"`
	Warnings   []warningDTO      `json:"warnings,omitempty"`
}

func (s *Server) listJournal(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	from, to, err := rangeParams(r, app.MaxJournalTicks)
	if err != nil {
		s.fail(w, r, err)
		return
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
	all, err := s.svc.JournalRange(r.Context(), app.BranchID(id), sim.Tick(from), sim.Tick(to))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	warn, err := s.branchWarnings(r, app.BranchID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := journalResponse{
		Warnings: warn, BranchID: id, From: from, To: to, Total: len(all), Offset: offset, Entries: make([]journalEntryDTO, 0),
		Currency: Currency, Disclaimer: sim.Disclaimer,
	}
	for i, e := range all {
		if int64(i) >= offset && int64(len(out.Entries)) < limit {
			out.Entries = append(out.Entries, journalEntryToDTO(i, e))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- End_Of_Horizon_Statement ----

type commitmentDTO struct {
	PurchaseOrderID    uint64 `json:"purchaseOrderId"`
	SKU                int64  `json:"sku"`
	Supplier           string `json:"supplier"`
	Qty                int64  `json:"qty"`
	UnitCostCents      int64  `json:"unitCostCents"`
	CommittedCostCents int64  `json:"committedCostCents"`
	DueAtTick          int64  `json:"dueAtTick"`
}

type stockLineDTO struct {
	SKU        int64 `json:"sku"`
	OnHand     int64 `json:"onHand"`
	ValueCents int64 `json:"valueCents"`
}

type endOfHorizonDTO struct {
	Tick                          int64           `json:"tick"`
	AtHorizon                     bool            `json:"atHorizon"`
	Metrics                       metricsDTO      `json:"metrics"`
	BacklogRevenueRecognisedCents int64           `json:"backlogRevenueRecognisedCents"`
	BacklogServiceCreditCents     int64           `json:"backlogServiceCreditCents"`
	OpenPurchaseOrders            []commitmentDTO `json:"openPurchaseOrders"`
	CommittedUnits                int64           `json:"committedUnits"`
	CommittedCostCents            int64           `json:"committedCostCents"`
	Stock                         []stockLineDTO  `json:"stock"`
	StockUnits                    int64           `json:"stockUnits"`
	StockValueCents               int64           `json:"stockValueCents"`
	InventoryMatch                bool            `json:"inventoryMatch"`
	AccruedUnpaidWagesCents       int64           `json:"accruedUnpaidWagesCents"`
	BalanceIdentityHolds          bool            `json:"balanceIdentityHolds"`
}

func endOfHorizonToDTO(e sim.EndOfHorizon) endOfHorizonDTO {
	d := endOfHorizonDTO{
		Tick: int64(e.Tick), AtHorizon: e.AtHorizon, Metrics: metricsToDTO(e.Metrics),
		BacklogRevenueRecognisedCents: int64(e.BacklogRevenueRecognised), BacklogServiceCreditCents: int64(e.BacklogServiceCredit),
		OpenPurchaseOrders: make([]commitmentDTO, 0, len(e.OpenPurchaseOrders)),
		CommittedUnits:     e.CommittedUnits, CommittedCostCents: int64(e.CommittedCost),
		Stock:      make([]stockLineDTO, 0, len(e.Stock)),
		StockUnits: e.StockUnits, StockValueCents: int64(e.StockValue), InventoryMatch: e.InventoryMatch,
		AccruedUnpaidWagesCents: int64(e.AccruedUnpaidWages), BalanceIdentityHolds: e.BalanceIdentityHolds,
	}
	for _, c := range e.OpenPurchaseOrders {
		d.OpenPurchaseOrders = append(d.OpenPurchaseOrders, commitmentDTO{
			PurchaseOrderID: c.PurchaseOrderID, SKU: c.SKU, Supplier: supplierRoleName(c.Supplier), Qty: c.Qty,
			UnitCostCents: int64(c.UnitCost), CommittedCostCents: int64(c.CommittedCost), DueAtTick: int64(sim.TickOf(c.DueAt)),
		})
	}
	for _, l := range e.Stock {
		d.Stock = append(d.Stock, stockLineDTO{SKU: l.SKU, OnHand: l.OnHand, ValueCents: int64(l.Value)})
	}
	return d
}

type endOfHorizonResponse struct {
	BranchID   string          `json:"branchId"`
	StateHash  string          `json:"stateHash"`
	Statement  endOfHorizonDTO `json:"statement"`
	Currency   string          `json:"currency"`
	Disclaimer string          `json:"disclaimer"`
	Warnings   []warningDTO    `json:"warnings,omitempty"`
}

// getEndOfHorizon reads the statement at the `tick` query, or at the head when
// `tick` is absent.
func (s *Server) getEndOfHorizon(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	bid := app.BranchID(id)
	var t sim.Tick
	if _, present, qerr := queryInt(r, "tick", false); qerr != nil {
		s.fail(w, r, qerr)
		return
	} else if present {
		if t, err = tickParam(r); err != nil {
			s.fail(w, r, err)
			return
		}
	} else if t, err = s.svc.HeadTick(r.Context(), bid); err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.svc.EndOfHorizonAt(r.Context(), bid, t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	h, err := s.svc.HashAt(r.Context(), bid, t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	warn, err := s.branchWarnings(r, bid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, endOfHorizonResponse{
		Warnings: warn, BranchID: id, StateHash: hashHex(h), Statement: endOfHorizonToDTO(e), Currency: Currency, Disclaimer: sim.Disclaimer,
	})
}
