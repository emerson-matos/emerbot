package payments

import "github.com/emerson/emerbot/packages/payments"

// The wire types below are deliberate DTOs rather than the canonical structs
// serialized directly, matching the convention in internal/finance
// (entryResponse): the HTTP contract is snake_case and is allowed to evolve
// separately from the domain, so renaming a field in packages/payments cannot
// silently break the frontend.

type saleResponse struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	ExternalID   string `json:"external_id"`
	SaleDate     string `json:"sale_date"`
	GrossAmount  int64  `json:"gross_amount"` // centavos
	NetAmount    int64  `json:"net_amount"`
	FeeAmount    int64  `json:"fee_amount"`
	Method       string `json:"method"`
	Brand        string `json:"brand"`
	Installments int    `json:"installments"`
}

type receivableResponse struct {
	Provider          string `json:"provider"`
	SaleID            string `json:"sale_id"`
	ExpectedDate      string `json:"expected_date"`
	Amount            int64  `json:"amount"` // centavos
	InstallmentNumber int    `json:"installment_number"`
	InstallmentTotal  int    `json:"installment_total"`
}

type forecastPointResponse struct {
	Date                string `json:"date"`
	ProjectedIncome     int64  `json:"projected_income"`
	ProjectedReceivable int64  `json:"projected_receivable"`
	ProjectedExpense    int64  `json:"projected_expense"`
	RunningBalance      int64  `json:"running_balance"`
}

func responseSale(s payments.Sale) saleResponse {
	return saleResponse{
		ID: string(s.ID), Provider: string(s.Provider), ExternalID: s.ExternalID,
		SaleDate: s.SaleDate.String(), GrossAmount: s.GrossAmount, NetAmount: s.NetAmount,
		FeeAmount: s.FeeAmount, Method: string(s.Method), Brand: s.Brand,
		Installments: s.Installments,
	}
}

func responseReceivable(r payments.ExpectedReceivable) receivableResponse {
	return receivableResponse{
		Provider: string(r.Provider), SaleID: string(r.SaleID),
		ExpectedDate: r.ExpectedDate.String(), Amount: r.Amount,
		InstallmentNumber: r.InstallmentNumber, InstallmentTotal: r.InstallmentTotal,
	}
}

func responsePoint(p ForecastPoint) forecastPointResponse {
	return forecastPointResponse{
		Date: p.Date, ProjectedIncome: p.ProjectedIncome,
		ProjectedReceivable: p.ProjectedReceivable, ProjectedExpense: p.ProjectedExpense,
		RunningBalance: p.RunningBalance,
	}
}

// responseSales and friends always return a non-nil slice so the JSON carries
// [] rather than null on an empty range — the frontend then needs no null guard.
func responseSales(sales []payments.Sale) []saleResponse {
	out := make([]saleResponse, 0, len(sales))
	for _, s := range sales {
		out = append(out, responseSale(s))
	}
	return out
}

func responseReceivables(recv []payments.ExpectedReceivable) []receivableResponse {
	out := make([]receivableResponse, 0, len(recv))
	for _, r := range recv {
		out = append(out, responseReceivable(r))
	}
	return out
}

func responsePoints(points []ForecastPoint) []forecastPointResponse {
	out := make([]forecastPointResponse, 0, len(points))
	for _, p := range points {
		out = append(out, responsePoint(p))
	}
	return out
}
