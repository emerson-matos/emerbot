// Package analytics turns raw ledger data into the insights the dashboard,
// the WhatsApp notifier and the AI bot all speak in: health status, trends,
// weekday averages, week-over-week pace, cash position and recommendations.
//
// It used to live in the frontend (apps/web/src/lib/analytics), which meant
// only the dashboard could see any of it — the notifier and the bot were blind
// to everything but a single month's totals. The logic, thresholds and
// Portuguese copy are ported from there verbatim so the dashboard keeps
// rendering exactly what it rendered before.
//
// Every amount is centavos (int64), matching the rest of the codebase.
package analytics

// HealthStatus is the traffic light shown at the top of the analysis.
type HealthStatus string

const (
	HealthBoa     HealthStatus = "boa"
	HealthAtencao HealthStatus = "atencao"
	HealthCritico HealthStatus = "critico"
)

// InsightType names the rule that produced an insight, so consumers can style
// or filter without matching on the (translatable) title.
type InsightType string

const (
	InsightExpenseGrowth     InsightType = "expense_growth"
	InsightRevenueDrop       InsightType = "revenue_drop"
	InsightLowCashFlow       InsightType = "low_cash_flow"
	InsightGoalBehind        InsightType = "goal_behind"
	InsightGoodPerformance   InsightType = "good_performance"
	InsightWeeklyImprovement InsightType = "weekly_improvement"
	InsightWeeklyDecline     InsightType = "weekly_decline"
	InsightGoalOnTrack       InsightType = "goal_on_track"
	// InsightMonthStart stands in for every retrospective insight on the first
	// day of a month, where none of them can be computed honestly — see
	// buildHealth.
	InsightMonthStart InsightType = "month_start"
)

// There is deliberately no cash-runway insight: a balance about to go negative
// is reported as a Recommendation ("Saldo fica negativo em breve"), because it
// comes with something to do about it.

// InsightSeverity drives both the colour in the dashboard and the overall
// HealthStatus (see status).
type InsightSeverity string

const (
	SeverityInfo     InsightSeverity = "info"
	SeverityWarning  InsightSeverity = "warning"
	SeverityCritical InsightSeverity = "critical"
)

// Insight is a single sentence about the month, already written in Portuguese.
type Insight struct {
	Type        InsightType     `json:"type"`
	Severity    InsightSeverity `json:"severity"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	// Value carries the raw number behind the insight (a percentage, or an
	// amount in centavos) when there is one. Nil rather than 0 so "no number"
	// stays distinguishable from "zero percent".
	Value *float64 `json:"value,omitempty"`
}

// Health is the status plus every insight that fired this month.
type Health struct {
	Status HealthStatus `json:"status"`
	// Score is the 0–100 number shown next to the traffic light: a clean month
	// is 100 and each problem costs what its severity is worth (see
	// healthScore). It is computed here rather than in each consumer so the
	// number and the status can never tell different stories.
	Score    int       `json:"score"`
	Messages []Insight `json:"messages"`
}

// TrendDirection is the month-over-month movement, with a ±2% dead band so
// noise doesn't read as a trend.
type TrendDirection string

const (
	TrendUp     TrendDirection = "up"
	TrendDown   TrendDirection = "down"
	TrendStable TrendDirection = "stable"
)

// MonthTrend compares one metric against the previous month. Change is a
// whole percentage.
type MonthTrend struct {
	Current   int64          `json:"current"`
	Previous  int64          `json:"previous"`
	Change    int            `json:"change"`
	Direction TrendDirection `json:"direction"`
}

// Trends bundles the three headline metrics, each compared against the
// previous month over the same finished days.
//
// The window they were measured over is Analysis.Period — it is a fact about
// the whole analysis, not about the trends alone, and every consumer must
// label a percentage with it rather than present it as a whole-month figure.
type Trends struct {
	// Faturamento is the growth reading, so it tracks sales only (see
	// domain.IsRevenue) — a loan is not the business growing. Resultado is the
	// broad money-in-minus-money-out movement.
	Faturamento MonthTrend `json:"faturamento"`
	Despesa     MonthTrend `json:"despesa"`
	Resultado   MonthTrend `json:"resultado"`
}

// Period says how far into the analysed month this Analysis stands. It is the
// one place the split between "what already happened" and "what is still
// ahead" is written down, and every figure in the Analysis honours it: nothing
// retrospective counts today, and nothing forward-looking writes today off.
type Period struct {
	// ThroughDay is the last day of the month with complete data — yesterday
	// while the month is running, the month's last day once it is closed. Zero
	// on the first day of a month: nothing has finished, so every retrospective
	// figure here is empty and there is no percentage to quote. Consumers must
	// render that as "the month is starting", never as a fall to zero.
	ThroughDay int `json:"throughDay"`
	// DaysRemaining counts the days still to trade, today included — today is a
	// day the pharmacy can still sell on. Zero for a closed month.
	DaysRemaining int `json:"daysRemaining"`
	DaysTotal     int `json:"daysTotal"`
	// InProgress is true when the analysed month is the one we are in. It is
	// what tells "compared through the 14th, mid-month" from a closed month
	// compared whole.
	InProgress bool `json:"inProgress"`
}

// WeekdayStat is the counter-sales average for one day of the week across the
// analysed month. Day is 0=Sunday, matching JavaScript's getDay().
type WeekdayStat struct {
	Day     int    `json:"day"`
	Label   string `json:"label"`
	Avg     int64  `json:"avg"`
	Total   int64  `json:"total"`
	Count   int    `json:"count"`
	IsToday bool   `json:"isToday"`
}

// DayHighlight names a single standout day. Date is "YYYY-MM-DD", or
// NoDataDate when the month has no entries at all.
type DayHighlight struct {
	Date   string `json:"date"`
	Label  string `json:"label"`
	Amount int64  `json:"amount"`
}

// NoDataDate is the placeholder Date a highlight carries when there is nothing
// to highlight, so consumers can tell "no data" from a real day without
// guessing at the label.
const NoDataDate = "—"

// Highlights are the best and worst days of the month, by income and by
// balance.
type Highlights struct {
	BestIncome   DayHighlight `json:"bestIncome"`
	WorstIncome  DayHighlight `json:"worstIncome"`
	BestBalance  DayHighlight `json:"bestBalance"`
	WorstBalance DayHighlight `json:"worstBalance"`
}

// CashOutItem is one category's slice of a heavy spending day.
type CashOutItem struct {
	Category string `json:"category"`
	Amount   int64  `json:"amount"`
	Count    int    `json:"count"`
}

// CashOutDay is a day with notable outgoings, broken down by category.
type CashOutDay struct {
	Date  string        `json:"date"`
	Total int64         `json:"total"`
	Items []CashOutItem `json:"items"`
}

// ExpenseComposition is one category's share of the month's expenses.
type ExpenseComposition struct {
	CategoryID   string `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	Amount       int64  `json:"amount"`
	Percentage   int    `json:"percentage"`
}

// GoalProgress tracks the month against its revenue and expense targets.
// RevenueActual is faturamento (see domain.IsRevenue), which is what the target
// is set against — a month must not read as "goal reached" because a loan came
// in. It is therefore both narrower and broader than KPIs.EntradasCaixa:
// narrower because it excludes non-sales, broader because an unpaid sale still
// counts toward the goal.
type GoalProgress struct {
	RevenueTarget int64 `json:"revenueTarget"`
	RevenueActual int64 `json:"revenueActual"`
	RevenuePct    int   `json:"revenuePct"`
	ExpenseTarget int64 `json:"expenseTarget"`
	ExpenseActual int64 `json:"expenseActual"`
	ExpensePct    int   `json:"expensePct"`
	DaysRemaining int   `json:"daysRemaining"`
	DaysTotal     int   `json:"daysTotal"`
}

// MonthlySnapshot is one bar in the trailing three-month history chart. The
// target fields are nil for a month with no goal set, which the chart draws
// differently from a target of zero.
//
// Revenue is faturamento, the same basis as RevenueTarget, so the bar and its
// target line measure the same thing.
type MonthlySnapshot struct {
	Month         string `json:"month"`
	Label         string `json:"label"`
	Revenue       int64  `json:"revenue"`
	RevenueTarget *int64 `json:"revenueTarget"`
	Expense       int64  `json:"expense"`
	ExpenseTarget *int64 `json:"expenseTarget"`
}

// WeekComparison measures this week's faturamento against last week's, and
// projects the rest of this week from the resulting daily rate.
type WeekComparison struct {
	// Current is Monday through today, Previous the whole of last week. Both
	// are totals for the chart; the *comparison* lives in Pace.
	Current  int64    `json:"current"`
	Previous int64    `json:"previous"`
	Pace     WeekPace `json:"pace"`

	ProjectedWeekly int64 `json:"projectedWeekly"`
	// The month-level projection lives on Projection, not here: this one used
	// to carry a second one derived from last week's flat daily rate, which
	// disagreed with the projection the dashboard drew from the weekday
	// averages.
	MonthlyTarget int64    `json:"monthlyTarget"`
	Labels        []string `json:"labels"`
}

// WeekPace is this week against last week over the same *finished* days —
// Monday through yesterday on both sides. It is the only week-over-week reading
// the insights and the recommendation may use: comparing this week including a
// morning still being traded against last week's matching weekday in full
// reported "ritmo caiu" every morning, and on a Monday reported a 100% fall.
type WeekPace struct {
	Current  int64 `json:"current"`
	Previous int64 `json:"previous"`
	// Days is how many finished days of this week both sides cover. Zero on a
	// Monday, where the week has nothing to compare yet.
	Days int `json:"days"`
}

// ProjectionBasis says how much the projection knows. The days still to come
// are priced from the trailing weeks (see projectionRates), and a window with
// barely anything in it must not produce a figure that reads as confidently as
// one built on two months of trading.
type ProjectionBasis string

const (
	// ProjectionFromWindow is the ordinary case: a week or more of trading days
	// inside the window. Nothing to qualify.
	ProjectionFromWindow ProjectionBasis = "janela"
	// ProjectionPartial is fewer than seven days traded across the whole window
	// — a pharmacy in its first weeks on the app. The figure is real but it will
	// still move a lot.
	ProjectionPartial ProjectionBasis = "parcial"
	// ProjectionNoBasis is nothing traded in the window at all. There is no
	// projection to speak of, and consumers must say that rather than print a
	// figure that is only the month's actual takings wearing a different label.
	ProjectionNoBasis ProjectionBasis = "sem_base"
	// ProjectionClosed is a month that has already ended: nothing was estimated,
	// Projected is the month's own faturamento. It is a basis of its own rather
	// than one of the above because a closed month's window is full of trading
	// and would otherwise report "janela" — labelling a realised total as an
	// eight-week estimate, on the one figure that is not an estimate at all.
	ProjectionClosed ProjectionBasis = "fechado"
)

// Projection is where the month lands and what it would take to close the gap
// to the income goal. Every amount is faturamento (see isFaturamento),
// matching how the target is set.
type Projection struct {
	// Actual is what has come in so far, Remaining what the days left are
	// expected to bring at their weekday averages, Projected the sum.
	Actual    int64 `json:"actual"`
	Remaining int64 `json:"remaining"`
	Projected int64 `json:"projected"`
	Target    int64 `json:"target"`
	// Gap is what the projection still misses the target by, 0 once it clears
	// it — so it never reads as a shortfall when there is none.
	Gap int64 `json:"gap"`
	// OnTrack is Projected reaching Target. It is the one verdict on whether
	// the month closes: the health insight, the weekly recommendation and the
	// dashboard card all read it rather than each deciding for themselves.
	OnTrack       bool `json:"onTrack"`
	DaysRemaining int  `json:"daysRemaining"`
	// NeededPerDay is what each day left has to bring, measured from Actual, to
	// reach Target. 0 when the target is already met or there is nothing to
	// pace against.
	NeededPerDay int64 `json:"neededPerDay"`
	// Basis is how much trading the projection was built from. Consumers render
	// it as a qualifier; they must not re-derive one from the amounts.
	Basis ProjectionBasis `json:"basis"`
}

// Pacing reports whether there is anything to pace against: a target, and days
// left to reach it in. A month with no goal, or one on its last day, has no
// per-day ask to make.
func (p Projection) Pacing() bool {
	return p.Target > 0 && p.DaysRemaining > 0
}

// RecommendationSeverity drives the recommendation's colour.
type RecommendationSeverity string

const (
	RecSuccess RecommendationSeverity = "success"
	RecWarning RecommendationSeverity = "warning"
	RecDanger  RecommendationSeverity = "danger"
)

// Recommendation is an actionable next step, in Portuguese.
type Recommendation struct {
	Severity RecommendationSeverity `json:"severity"`
	Title    string                 `json:"title"`
	Message  string                 `json:"message"`
}

// CashPosition summarizes the month's daily balance projection.
type CashPosition struct {
	CurrentBalance       int64 `json:"currentBalance"`
	EndOfMonthProjection int64 `json:"endOfMonthProjection"`
	// DaysUntilNegative is nil when the balance never goes negative within the
	// projection — distinct from 0, which means it goes negative today.
	DaysUntilNegative   *int   `json:"daysUntilNegative"`
	LowestProjected     int64  `json:"lowestProjected"`
	LowestProjectedDate string `json:"lowestProjectedDate"`
}

// KPIs are the headline numbers.
type KPIs struct {
	Resultado int64 `json:"resultado"`
	// Faturamento is what the pharmacy sold this month, by the day of each sale
	// (see finance.RevenueDate). Every performance reading on this page is
	// measured against it.
	Faturamento int64 `json:"faturamento"`
	// EntradasCaixa is every centavo that actually arrived this month, by the
	// day it arrived — loans, aportes and yields included. It is a liquidity
	// number, not a performance one, and it is *expected* to differ from
	// Faturamento. Two ways, in fact: it counts inflows that were not sales,
	// and it excludes sales that have not been paid yet.
	EntradasCaixa int64 `json:"entradasCaixa"`
	Despesa       int64 `json:"despesa"`

	// There is deliberately no "days remaining" or "last month up to today"
	// here. The first is Period.DaysRemaining, so the whole analysis counts the
	// days the same way; the second is Trends.Faturamento, whose Current and
	// Previous are already last month and this month over the same finished
	// days. Both used to be computed a second time here, off today's day
	// number, and disagreed with the figures shown beside them.
}

// SchemaVersion is the shape of the Analysis JSON. It exists because the
// notifier persists a whole Analysis as a daily snapshot
// (finance.InsightSnapshot) and the dashboard-api unmarshals *yesterday's*
// stored JSON into *today's* struct to diff the two. A renamed field would
// silently read as zero on the old side, so "faturamento went from R$ 0 to
// R$ 45.000" would be reported as real movement on every deploy day.
//
// Bump it whenever a field is renamed, removed, or changes meaning. Consumers
// must refuse to compare across versions rather than guess — see
// apps/dashboard-api/internal/finance/snapshot.go.
//
// 3: added Period; dropped kpis.daysRemaining and
// kpis.previousMonthRevenueUpToDay (Period and Trends carry them now);
// weekComparison.previousUpToDay became weekComparison.pace; every
// retrospective figure now stops at yesterday instead of at today.
//
// 4: projection.remaining and projection.projected changed basis — the days
// still to come are priced from a trailing eight-week window instead of from
// the analysed month's own finished days, so they no longer collapse toward
// zero at the start of a month. Added projection.basis. Without the bump the
// deploy day would diff a window-based projection against a month-based one and
// report the difference as the month having moved.
const SchemaVersion = 4

// Analysis is the full picture of one month — the payload of
// GET /analysis/monthly, and the input every consumer renders from.
type Analysis struct {
	// Schema is SchemaVersion at the time this Analysis was built. Zero means
	// a snapshot stored before versioning existed, which is never comparable.
	Schema             int                  `json:"schemaVersion"`
	Month              string               `json:"month"`
	Period             Period               `json:"period"`
	KPIs               KPIs                 `json:"kpis"`
	Health             Health               `json:"health"`
	Trends             Trends               `json:"trends"`
	Weekdays           []WeekdayStat        `json:"weekdays"`
	WeekComparison     WeekComparison       `json:"weekComparison"`
	Highlights         Highlights           `json:"highlights"`
	CashOutDays        []CashOutDay         `json:"cashOutDays"`
	ExpenseComposition []ExpenseComposition `json:"expenseComposition"`
	Goals              GoalProgress         `json:"goals"`
	Projection         Projection           `json:"projection"`
	History            []MonthlySnapshot    `json:"history"`
	CashPosition       CashPosition         `json:"cashPosition"`
	Recommendations    []Recommendation     `json:"recommendations"`
}
