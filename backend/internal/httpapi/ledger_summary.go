package httpapi

import (
	"net/http"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// figureComponent is one ledger account that makes up a headline money figure,
// with the sign it carries: the figure is the sum over components of
// sign times the account's balance on its normal side.
type figureComponent struct {
	account sim.AccountID
	sign    int64
}

// moneyFigure is a headline money figure and its composition. The definitions
// match sim.ComputeMetrics (INV-10).
type moneyFigure struct {
	name       string
	components []figureComponent
}

// moneyFigures lists the drill-down figures in display order.
var moneyFigures = []moneyFigure{
	{"cash", []figureComponent{{sim.AccountCash, 1}}},
	{"net_profit", []figureComponent{
		{sim.AccountRevenue, 1}, {sim.AccountCostOfGoodsSold, -1}, {sim.AccountFreightExpense, -1},
		{sim.AccountLaborExpense, -1}, {sim.AccountMarketingExpense, -1}, {sim.AccountOverheadExpense, -1},
		{sim.AccountServiceCreditExpense, -1},
	}},
	{"net_revenue", []figureComponent{{sim.AccountRevenue, 1}}},
	{"cost_of_goods_sold", []figureComponent{{sim.AccountCostOfGoodsSold, 1}}},
	{"freight", []figureComponent{{sim.AccountFreightExpense, 1}}},
	{"labor", []figureComponent{{sim.AccountLaborExpense, 1}}},
	{"marketing", []figureComponent{{sim.AccountMarketingExpense, 1}}},
	{"overhead", []figureComponent{{sim.AccountOverheadExpense, 1}}},
	{"service_credits", []figureComponent{{sim.AccountServiceCreditExpense, 1}}},
}

func figureByName(name string) (moneyFigure, bool) {
	for _, f := range moneyFigures {
		if f.name == name {
			return f, true
		}
	}
	return moneyFigure{}, false
}

// normalAmount is the line's amount on its account's normal side.
func normalAmount(l sim.LedgerLine) int64 {
	if l.Account.NormalSide() == sim.SideCredit {
		return int64(l.Credit - l.Debit)
	}
	return int64(l.Debit - l.Credit)
}

// contribution returns the entry's signed contribution to the figure and
// whether the entry has a line on any of its accounts.
func (f moneyFigure) contribution(e sim.LedgerEntry) (int64, bool) {
	var sum int64
	touched := false
	for _, l := range e.Lines {
		for _, c := range f.components {
			if l.Account == c.account {
				sum += c.sign * normalAmount(l)
				touched = true
			}
		}
	}
	return sum, touched
}

type figureComponentDTO struct {
	Account string `json:"account"`
	Sign    int    `json:"sign"`
}

type ledgerFigureDTO struct {
	Figure     string               `json:"figure"`
	SumCents   int64                `json:"sumCents"`
	EntryCount int                  `json:"entryCount"`
	Components []figureComponentDTO `json:"components"`
}

type ledgerAccountDTO struct {
	Account      string `json:"account"`
	NormalSide   string `json:"normalSide"`
	DebitCents   int64  `json:"debitCents"`
	CreditCents  int64  `json:"creditCents"`
	BalanceCents int64  `json:"balanceCents"`
	EntryCount   int    `json:"entryCount"`
}

type ledgerSummaryResponse struct {
	BranchID   string             `json:"branchId"`
	Tick       int64              `json:"tick"`
	Accounts   []ledgerAccountDTO `json:"accounts"`
	Figures    []ledgerFigureDTO  `json:"figures"`
	Currency   string             `json:"currency"`
	Disclaimer string             `json:"disclaimer"`
	Warnings   []warningDTO       `json:"warnings,omitempty"`
}

func (s *Server) getLedgerSummary(w http.ResponseWriter, r *http.Request) {
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
	st, err := s.svc.StateAt(r.Context(), app.BranchID(id), t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	warn, err := s.branchWarnings(r, app.BranchID(id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	entries := st.Ledger.Entries
	out := ledgerSummaryResponse{
		Warnings: warn, BranchID: id, Tick: int64(t), Currency: Currency, Disclaimer: sim.Disclaimer,
		Accounts: make([]ledgerAccountDTO, 0, sim.NumAccounts), Figures: make([]ledgerFigureDTO, 0, len(moneyFigures)),
	}
	for _, a := range sim.Accounts() {
		d := ledgerAccountDTO{Account: a.String(), NormalSide: "debit"}
		if a.NormalSide() == sim.SideCredit {
			d.NormalSide = "credit"
		}
		var net int64
		for _, e := range entries {
			if int64(e.Tick) > int64(t) || !hasAccount(e, a) {
				continue
			}
			d.EntryCount++
			for _, l := range e.Lines {
				if l.Account == a {
					d.DebitCents += int64(l.Debit)
					d.CreditCents += int64(l.Credit)
					net += normalAmount(l)
				}
			}
		}
		d.BalanceCents = net
		out.Accounts = append(out.Accounts, d)
	}
	for _, f := range moneyFigures {
		d := ledgerFigureDTO{Figure: f.name, Components: make([]figureComponentDTO, 0, len(f.components))}
		for _, c := range f.components {
			d.Components = append(d.Components, figureComponentDTO{Account: c.account.String(), Sign: int(c.sign)})
		}
		for _, e := range entries {
			if int64(e.Tick) > int64(t) {
				continue
			}
			if c, ok := f.contribution(e); ok {
				d.SumCents += c
				d.EntryCount++
			}
		}
		out.Figures = append(out.Figures, d)
	}
	writeJSON(w, http.StatusOK, out)
}
