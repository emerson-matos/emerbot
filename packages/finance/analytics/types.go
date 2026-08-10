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

import "time"

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
	// ComparableThroughDay is the last day of the month the month-over-month
	// figures were measured through, on both sides. It closes on whole weeks
	// while the month runs, so it trails ThroughDay by up to six days and is
	// zero for the whole first week — the first N days of two months hold
	// different weekdays unless N is a multiple of seven, and a percentage over
	// mismatched weekdays measures the calendar.
	//
	// Every percentage in Trends, and every "vs mês passado" line, is over this
	// window and no other. Zero means there is no comparison to render at all:
	// consumers must say the month has not closed a week yet, never draw a fall
	// to zero. Figures about this month alone — the result, the traffic light,
	// the days with movement — keep using ThroughDay.
	ComparableThroughDay int `json:"comparableThroughDay"`
	// DaysRemaining counts the days still to trade, today included — today is a
	// day the pharmacy can still sell on. Zero for a closed month.
	DaysRemaining int `json:"daysRemaining"`
	DaysTotal     int `json:"daysTotal"`
	// InProgress is true when the analysed month is the one we are in. It is
	// what tells "compared through the 14th, mid-month" from a closed month
	// compared whole.
	InProgress bool `json:"inProgress"`
}

// WeekdayStat is the Gaussian-weighted average faturamento for one day of the
// week over the trailing 8-week window. Count is the number of distinct weeks
// that saw revenue (not the number of entries), and Basis qualifies how much
// data the average stands on.
//
// Day is the identity of the weekday and the only one this struct carries. It
// used to ship an abbreviated Portuguese Label beside it, which the browser then
// used as a *map key* to recover the full name — so the language of the backend
// decided whether a tooltip in the browser said "terça" or fell back to printing
// "Ter", and adding a second locale meant changing a Go array and hoping every
// lookup table downstream agreed. A weekday is 0–6; the words for it belong to
// whatever is doing the talking.
type WeekdayStat struct {
	// Day is time.Weekday: 0=Sunday, matching JavaScript's getDay().
	Day     time.Weekday    `json:"day"`
	Avg     int64           `json:"avg"`
	Count   int             `json:"count"`
	IsToday bool            `json:"isToday"`
	Basis   ProjectionBasis `json:"basis"`
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

// CategoryComposition is one category's share of a total — of the month's
// expenses in Analysis.ExpenseComposition, of its faturamento in
// Analysis.RevenueComposition.
//
// One type for both because it is one shape and one rendering: a name, an
// amount and a percentage of whatever it decomposes. What differs is which
// entries went into the fold, and that is the fold's business (see
// composition.go), not the row's.
type CategoryComposition struct {
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
	MonthlyTarget int64 `json:"monthlyTarget"`
	// Days is the week so far, one entry per day that has actually happened, as
	// time.Weekday. It used to be abbreviated Portuguese ("Dom", "Seg", …) that
	// the browser fed back into a lookup table to recover the full name; the
	// chart axis and the "hoje é …" sentence therefore both depended on Go's
	// spelling, and a missing key silently printed the abbreviation.
	Days []time.Weekday `json:"days"`
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
	// Change and Direction are the same reading MonthTrend carries, over a week
	// instead of a month and with a wider dead band (see weekDeadBandPct). They
	// are here because the health insight and the dashboard card both used to
	// derive them, and only the insight applied the dead band — the card printed
	// "↑ 3% vs semana anterior" for a week the insights treated as flat.
	//
	// A Previous of zero still reads as a flat 100% up, as it does for a month:
	// consumers check Previous and say "sem base" rather than quoting it.
	Change    int            `json:"change"`
	Direction TrendDirection `json:"direction"`
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

// DayTargetScale says how a day's target compares to its historical weekday
// average. The backend decides this so the frontend renders a colour without
// deriving thresholds from the amounts.
type DayTargetScale string

const (
	// PaceBelow means the amount is below the historical average. Since ADR-025
	// a *target* can no longer be: the plan is floored at the weekday's own
	// rhythm, so this is now produced only for a day that has closed, where it
	// grades what the day sold. It is emphatically not dead: "essa quarta veio
	// 20% abaixo do que uma quarta costuma dar" is the reading it exists for.
	PaceBelow DayTargetScale = "below"
	// PaceOnTrack means the target is within ±5% of the historical average —
	// neither a stretch nor a slack day.
	PaceOnTrack DayTargetScale = "on_track"
	// PaceAbove means the day's target exceeds the historical average — the
	// pharmacy needs to sell more than it usually does on this weekday.
	PaceAbove DayTargetScale = "above"
)

// DayTargetState says whether the day has an ask, and when it does not, which
// of the reasons it is.
//
// It replaced a bare `valid bool`, which could not answer the only question a
// reader has when the card is missing: why. Those reasons are different
// sentences — "a meta do mês já foi batida" is good news, "não há histórico
// para calcular" is a gap in the data, "a farmácia não abre domingo" is a fact
// about the business, and rendering all of them as the same blank space made
// the good news indistinguishable from the failure. The bot has the same
// problem in prose: asked "quanto preciso vender hoje?", it must be able to say
// which of these it is instead of going quiet or, worse, inventing a number.
type DayTargetState string

const (
	// DayTargetOK is a real ask: the amounts on DayTarget are meaningful.
	DayTargetOK DayTargetState = "ok"
	// DayTargetClosedMonth is a month that has already ended — there is no
	// day inside it left to sell on.
	DayTargetClosedMonth DayTargetState = "mes_fechado"
	// DayTargetNoGoal is a month with no revenue target: nothing to pace
	// against, so no share of it to ask for the day.
	DayTargetNoGoal DayTargetState = "sem_meta"
	// DayTargetGoalMet is the target already reached. It is no longer produced:
	// since ADR-025 a month past its goal is asked for its ordinary rhythm
	// rather than for nothing, so the day comes back DayTargetOK with a
	// TargetFromAverage source and the good news travels as Plan.GapFactor
	// below 1. It survives in this enum because stored snapshots (see
	// SchemaVersion) still carry it and consumers still have to word it.
	//
	// What it used to do was read as permission to close the shop: "a meta já
	// foi batida" arrived as the same blank space as "não há histórico", and a
	// pharmacy that beat its goal on the 20th was asked for nothing at all for
	// eleven days.
	DayTargetGoalMet DayTargetState = "meta_batida"
	// DayTargetNoHistory is a trailing window with nothing in it to price any
	// remaining day from.
	DayTargetNoHistory DayTargetState = "sem_historico"
	// DayTargetClosedWeekday is a weekday that never traded across the whole
	// window — the pharmacy does not open on it, so asking it for a share of the
	// goal would be asking a closed door.
	DayTargetClosedWeekday DayTargetState = "dia_sem_movimento"
	// DayTargetMonthOver is a day past the analysed month's last one. It belongs
	// to a month with its own goal, its own gap and its own plan, and none of
	// them are knowable from here — so it is named as such rather than priced
	// off this month's arithmetic.
	DayTargetMonthOver DayTargetState = "mes_acaba_hoje"
	// DayTargetFutureMonth is a date in a month that has not started. It is told
	// apart from a closed one because monthClock cannot: a month that is not the
	// one Now falls in reports inProgress = false either way, so September asked
	// about in August would otherwise answer "mes_fechado" — a month that has
	// already ended, which is the opposite of the truth.
	DayTargetFutureMonth DayTargetState = "mes_futuro"
	// DayTargetClosedDay is a day that has finished. There is no ask on it and
	// there never will be again, which is not a gap in the data and not bad
	// news — it is the day having a result instead of a target. The figures that
	// do mean something (Realized, Historical) are filled in.
	DayTargetClosedDay DayTargetState = "dia_fechado"
)

// DayTargetSource says what a day's ask is made of: the share of the month's
// gap that fell to it, or the day of the week's own average holding the floor
// under that share — and, when it is the floor, why.
//
// It exists because the floor makes different situations produce the same
// number. A Wednesday asked for exactly R$ 1.000,00 is a month in step when the
// gap works out at the rhythm, and a month already past its goal when the gap
// works out at nothing, and the reader is owed which: one is "mantenha o
// ritmo", the other is "você já chegou lá". Before the floor those differed in
// the amount itself — the second one had no amount at all — and now they do
// not. An unnamed regime is exactly what DayBasis was added to prevent on the
// other axis.
type DayTargetSource string

const (
	// TargetFromGap is an ask above the weekday's average: the month is behind
	// and this day carries its share of catching up. Delta and DeltaPercent are
	// positive, Status is PaceAbove, and consumers must word it as a demand —
	// never as performance (see compareToUsual).
	TargetFromGap DayTargetSource = "plano"
	// TargetFromAverage is the floor with the month still chasing its goal: the
	// gap's share came out at or below this weekday's average, so the day is
	// asked for its average and nothing more. Target equals Historical, Delta is
	// zero and Status is PaceOnTrack.
	TargetFromAverage DayTargetSource = "media"
	// TargetGoalMet is the floor with nothing left to chase: the month's goal is
	// already reached and the ask is purely the pharmacy's own rhythm. The
	// figures are identical to TargetFromAverage and the sentence is not — this
	// is the one that is good news, and it is the reading that used to arrive as
	// an absent target (see DayTargetGoalMet).
	TargetGoalMet DayTargetSource = "meta_batida"
)

// DayBasis says what kind of thing a day's figures are: something that
// happened, something happening, or something being asked for.
//
// The three are not interchangeable and the distinction is not cosmetic. The
// line already runs through this package — ADR-017 drew it as "what is measured
// ends yesterday, what is asked for starts today" — but until now it was only
// implicit in *which* field a consumer happened to read. Once a day's ask can
// be requested for any date, the reader has to be told which side of the line
// the answer came from, or "R$ 1.052,00 na quarta" reads the same whether the
// Wednesday has happened or not.
type DayBasis string

const (
	// DayRealized is a day that has closed. Its Realized is fact and it carries
	// no Target: reconstructing the ask a past day would have had means
	// re-running the analysis as it stood that morning, and quoting today's plan
	// over a day nobody can sell on any more is a target invented backwards. A
	// closed day is reported, not asked.
	DayRealized DayBasis = "realizado"
	// DayInProgress is today: an ask that still stands, beside what the till has
	// taken so far. Both cover the whole day, so they are directly comparable —
	// which is the one comparison someone closing the day needs.
	DayInProgress DayBasis = "em_curso"
	// DayProjected is a day still to come. Its Target assumes every day before
	// it lands on its own — the plan is one distribution of the gap, so a slice
	// of it means nothing except alongside the slices ahead of it.
	DayProjected DayBasis = "projetado"
)

// DayTarget is the recommended revenue target for one day still to be traded.
//
// Rather than evenly distributing the remaining goal across the calendar,
// it allocates the day's share proportionally to the expected weekday demand.
// This keeps Sunday targets naturally lower than Saturday targets while
// remaining consistent with the monthly projection model. A factor of 1.08
// means the pharmacy needs to sell 8% above its usual Monday rhythm to close
// the gap; 1.00 means an ordinary Monday is enough, and there is no factor
// below it — the ask is floored at the weekday's own average (ADR-025).
//
// Internally this is computed using Gaussian-smoothed weekday averages.
//
// It is deliberately the only per-day ask the analysis produces. The one it
// replaced divided what was missing by the days left, which asks a Sunday for
// as much as a Monday — on a pharmacy whose Sundays bring a third of a Monday
// that is not a target, it is a number nobody can hit, printed every Sunday.
//
// It used to exist only for today, which is the one day nobody needs it for
// after closing time. Asked "como estamos para amanhã?" the bot had this
// month's plan in hand and no share of it for tomorrow, so it answered with the
// weekday's plain historical average — what a Wednesday *usually* brings, which
// is not what this Wednesday is being asked for. See ADR-020.
//
// Then it existed for today and tomorrow, one field each, which is the same
// mistake with one more day on it: the question after "e amanhã?" is "e no
// sábado?". The day is a parameter now, and Basis says which regime the answer
// came back in — see ADR-021.
type DayTarget struct {
	// State says whether there is an ask on this day and, when there is not,
	// why. DayTargetOK is the only value under which Target below means
	// anything; every other value leaves it zero.
	//
	// Realized is deliberately outside that rule: what a day sold is fact
	// whether or not there was a goal to measure it against.
	State DayTargetState `json:"state"`
	// Basis is what kind of figure this is — a day that closed, the day being
	// traded, or a day still ahead. Consumers must render it; a target and a
	// result read identically without it.
	Basis DayBasis `json:"basis"`
	// Date is the calendar day the ask is for, "YYYY-MM-DD". Empty when there
	// is no such day to name — a closed month has no today, and a date outside
	// the analysed month is not this month's to price.
	Date string `json:"date,omitempty"`
	// Day is the weekday the ask is for — time.Weekday, 0=Sunday. Like
	// WeekdayStat.Day it travels as a number: naming it is the job of whatever
	// speaks to the user, in whatever language it speaks.
	Day time.Weekday `json:"day"`
	// Realized is what the day has actually sold. It is the whole point of a
	// closed day and the missing half of an open one: Target is a whole-day
	// figure measured from the morning, so without this there is no way to say
	// whether the day met it. Zero for a day still ahead.
	Realized int64 `json:"realized"`
	// Historical is what this weekday usually brings, over a whole day; Target
	// is what it has to bring; Delta and DeltaPercent are the difference.
	//
	// On a day still ahead the difference can no longer be negative: the plan is
	// floored at Historical (ADR-025), so the ask is the average or more. On a
	// day that has closed it is Realized against Historical and may be either
	// way — that is the day's result, not an ask.
	Historical   int64          `json:"historical"`
	Target       int64          `json:"target"`
	Delta        int64          `json:"delta"`
	DeltaPercent float64        `json:"deltaPercent"`
	Factor       float64        `json:"factor"`
	Status       DayTargetScale `json:"status"`
	// Source is which of the two the ask came out of — the month's gap, or the
	// weekday average underneath it. Empty on a day with no ask at all. See
	// DayTargetSource for why the amounts cannot answer this.
	Source DayTargetSource `json:"source,omitempty"`
}

// Asked reports whether there is a target to show. It reads better than
// comparing against the state at every call site, and it is deliberately the
// only thing collapsing the states into a boolean — consumers that render an
// absence must say *which* absence, and for that they need State itself.
func (t DayTarget) Asked() bool { return t.State == DayTargetOK }

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
	// PlannedClose is where the month lands if every day still to come sells
	// what Plan asks of it — as against Projected, which is where it lands if
	// they merely trade as usual. Equal to Projected when there is no plan.
	//
	// The two answer different questions and only one of them was published,
	// which left "se eu bater a meta todo dia, fecho em quanto?" to be answered
	// with the month's goal — a figure that could sit *below* the projection on
	// a pharmacy running ahead of it. With the floor in place this is
	// max(Projected, Target): the plan never closes the month below what the
	// averages alone would have brought. See ADR-025.
	PlannedClose int64 `json:"plannedClose"`
	// Gap is what the projection still misses the target by, 0 once it clears
	// it — so it never reads as a shortfall when there is none.
	Gap int64 `json:"gap"`
	// OnTrack is Projected reaching Target. It is the one verdict on whether
	// the month closes: the health insight, the weekly recommendation and the
	// dashboard card all read it rather than each deciding for themselves.
	OnTrack       bool `json:"onTrack"`
	DaysRemaining int  `json:"daysRemaining"`
	// Basis is how much trading the projection was built from. Consumers render
	// it as a qualifier; they must not re-derive one from the amounts.
	Basis ProjectionBasis `json:"basis"`
	// Coverage is Projected over Target: 1.10 overshoots by 10%, 0.80 lands 20%
	// short. 0 when there is no target, where Status is ProjNoTarget. Status is
	// the verdict drawn from it. Consumers render both; like Basis, they must
	// not re-derive one from the amounts — a percentage rounded in the browser
	// beside a colour read off OnTrack put "97% da meta" in red under a green
	// "Ritmo suficiente".
	Coverage float64          `json:"coverage"`
	Status   ProjectionStatus `json:"status"`
	// TodayTarget scales today's historical weekday average by the factor that
	// would close the remaining gap if applied to every remaining day uniformly,
	// and carries what today has sold so far beside it.
	//
	// It is the only day this struct names, and deliberately so: ADR-019 needs
	// today's ask to arrive without being asked for, or the model derives one.
	// Every *other* day is a parameter — see Analysis.DayTargetOn and the
	// get_meta_do_dia tool. A field per day is not an answer to "e no sábado?".
	TodayTarget DayTarget `json:"todayTarget"`
	// Plan is how the gap is spread over the days the month has left, and it is
	// published rather than kept inside the build because it is what lets any
	// day be priced from an assembled Analysis — including one read back from a
	// stored snapshot.
	//
	// It is deliberately not derived from TodayTarget. Those two answer
	// different questions and diverge in a case that is not rare: on a Sunday a
	// pharmacy does not open, TodayTarget is "dia_sem_movimento" with no factor
	// at all, while the plan behind it is perfectly fine and still prices every
	// other day of the week. Reading the plan off today would spread today's
	// closed door across the whole month.
	Plan Plan `json:"plan"`
}

// Plan is one distribution of what is missing over the days that can still
// sell, floored at what those days usually bring: every remaining day asked for
// its own weekday average times a shared Factor, and Factor is never below 1.
// A Factor of 1.08 asks each day for 8% above its usual rhythm; a Factor of 1
// asks each for exactly its rhythm.
//
// The floor changed the invariant, and the new one is the point of ADR-025:
//
//	Σ média[dia_da_semana(d)] × Factor  ==  max(what is missing, Σ média[...])
//
// The plan asks for the gap or for the pharmacy's own rhythm, whichever is
// larger. It used to distribute the gap exactly — which closed the month and
// nothing else, so a month running ahead asked its days for less than they
// habitually sell, and a month past its goal asked for nothing at all. Neither
// is a target: a target is a floor under the day, never a ceiling over it.
//
// What it keeps from ADR-019 is the shape of the distribution — each day at its
// own weekday rhythm, never a flat daily average that asks a Sunday worth
// R$ 600,00 for R$ 1.200,00.
//
// State is DayTargetOK when there is a distribution at all, and otherwise names
// why there is none — no goal, no trading history, a month that has ended. A
// goal already met is no longer one of them: it is a plan at Factor 1. Factor
// means nothing unless State is DayTargetOK.
type Plan struct {
	State  DayTargetState `json:"state"`
	Factor float64        `json:"factor"`
	// GapFactor is what the gap alone would have asked for, before the floor:
	// above 1 the month is behind and Factor equals it, at or below 1 the month
	// is in step or ahead and Factor is 1. Zero means the goal is already met.
	//
	// It is published because it is the only thing that separates the two months
	// a floored ask cannot tell apart — see DayTargetSource — and because a
	// consumer that wants to say "o mês está adiantado" must not infer it from
	// Target equalling Historical, which is also what a month in perfect step
	// looks like.
	GapFactor float64 `json:"gapFactor"`
}

// AccelerationPct returns how much the projected revenue needs to grow, as a
// fraction of the projection itself, to reach the target. Zero when the
// projection already meets the target or when there is no projection to grow
// from.
func (p Projection) AccelerationPct() float64 {
	if p.Projected <= 0 {
		return 0
	}
	gap := p.Target - p.Projected
	if gap <= 0 {
		return 0
	}
	return float64(gap) / float64(p.Projected)
}

// Pacing reports whether there is anything to pace against: a target, and days
// left to reach it in. A month with no goal, or one on its last day, has no
// per-day ask to make.
func (p Projection) Pacing() bool {
	return p.Target > 0 && p.DaysRemaining > 0
}

// ProjectionStatus is the qualitative verdict on whether the current pace will
// close the gap to the income goal.
type ProjectionStatus string

const (
	// ProjNoTarget is the zero value: there is no goal to cover, so there is no
	// verdict to give. A month with no target is not a month falling short of
	// one, and must not render as danger.
	ProjNoTarget ProjectionStatus = ""
	// ProjSuccess means the projection reaches or exceeds the target.
	ProjSuccess ProjectionStatus = "success"
	// ProjWarning means the projection falls short but remains within reach.
	ProjWarning ProjectionStatus = "warning"
	// ProjDanger means the projection falls far short of the target.
	ProjDanger ProjectionStatus = "danger"
)

// The coverage cuts, in one place: the card's colour, the recommendation's
// title, its wording and the digest all come off these.
const (
	// coverageOnTarget — the last 5% is inside the noise of an eight-week
	// average, and calling it a shortfall asks a pharmacist to act on rounding.
	coverageOnTarget = 0.95
	// coverageWithinReach — above this the days left can still absorb the
	// shortfall.
	coverageWithinReach = 0.80
	// coverageOutOfReach — below this no plausible acceleration closes the gap,
	// and the copy says so instead of asking for one.
	coverageOutOfReach = 0.50
)

// projectionStatus maps coverage to the verdict. The thresholds are
// concentrated here so every consumer — dashboard, WhatsApp, health insights —
// agrees on the same reading.
func projectionStatus(coverage float64) ProjectionStatus {
	switch {
	case coverage >= coverageOnTarget:
		return ProjSuccess
	case coverage >= coverageWithinReach:
		return ProjWarning
	default:
		return ProjDanger
	}
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
//
// CurrentBalance is booked fact — money in the account today. Every other figure
// here is a projection: the days from today on are credited with what an
// ordinary day of that weekday receives, on top of what they have booked, while
// expenses stand exactly as scheduled. Without that the curve was all of the
// month's bills against none of its sales, and it announced a negative balance
// within days at the start of every month.
type CashPosition struct {
	CurrentBalance       int64 `json:"currentBalance"`
	EndOfMonthProjection int64 `json:"endOfMonthProjection"`
	// DaysUntilNegative is nil when the balance never goes negative within the
	// projection — distinct from 0, which means it goes negative today.
	DaysUntilNegative   *int   `json:"daysUntilNegative"`
	LowestProjected     int64  `json:"lowestProjected"`
	LowestProjectedDate string `json:"lowestProjectedDate"`
	// ExpectsReceipts is whether the projection had any trading history to
	// credit the days ahead with. False means the forward figures are bills
	// against nothing — true of a pharmacy that has never recorded an inflow,
	// and consumers must not present that as a balance heading for zero.
	ExpectsReceipts bool `json:"expectsReceipts"`
	// Commitments answers the only question the month's scheduled expenses
	// actually raise. It is derived from the curve below, not from the amount:
	// what makes R$ 20.000,00 of bills a problem is the balance failing to
	// cover them, and this says whether it does.
	Commitments CommitmentCoverage `json:"commitments"`
	// NextDay is tomorrow's own line of the runway. Nil when there is no
	// tomorrow inside the forecast — a closed month, or the month's last day.
	//
	// Everything else on this struct describes the month: where it ends, where
	// it dips lowest, how long until it crosses zero. Asked "como estamos para
	// amanhã?", the only cash figures the bot had were those, so it answered
	// with a month-end balance and the whole month's scheduled expenses — both
	// true, neither about tomorrow, and the second one alarming out of context.
	// A day the pharmacy is about to open is a day it can be told about.
	//
	// It is a view of Forecast, not a second reading: the same entry, named
	// because the bot's payload has no clock to find it with.
	NextDay *DayCash `json:"nextDay,omitempty"`
	// Forecast is the whole curve, one entry per day of the month, and it is the
	// series every other figure here is read off — EndOfMonthProjection is its
	// last Balance, LowestProjected its smallest.
	//
	// It is published because the dashboard was drawing a different curve. The
	// chart plotted the *booked* running balance and labelled the days ahead
	// "projeção", which is the curve this file exists to correct: the month's
	// bills are all booked on the 1st and its sales are recorded as they happen,
	// so the tail dives every month. The page therefore drew a collapse while
	// the card beside it and the bot both said the month ends in the black.
	// Drawing it needs the series, and the browser must not rebuild it — that is
	// how the projection ended up computed twice and differently before.
	//
	// It is deliberately absent from ToolPayload: thirty-one days of series
	// costs tokens on every question a chat asks, and the bot needs a day, not a
	// curve.
	Forecast []DayCash `json:"forecast"`
}

// CommitmentCoverage says whether the month's booked bills are paid for by the
// balance that is coming.
//
// It exists because a total on its own is not a finding. Asked "como estamos?",
// the bot reported "o volume de despesas agendadas (R$ 19.130,95) é um ponto de
// atenção" under the health verdict — on a month whose runway never went near
// zero, and whose health the backend had graded without counting a single
// scheduled bill (ADR-017). Every number in that sentence was true and the
// conclusion was invented, because the payload offered the amount with nothing
// to weigh it against.
//
// A commitment is a liquidity question, never a performance one. This is the
// answer to it, and it is derived from the projected curve rather than from the
// amount: bills are only heavy relative to what is coming in.
type CommitmentCoverage string

const (
	// CommitmentsCovered is a projected balance that never goes under water.
	// The bills are real and they are paid for; there is nothing to warn about.
	CommitmentsCovered CommitmentCoverage = "coberto"
	// CommitmentsUncovered is a curve that dips below zero somewhere in the
	// month. This is the case worth raising, and CashPosition.LowestProjected
	// and DaysUntilNegative say how badly and when.
	CommitmentsUncovered CommitmentCoverage = "descoberto"
	// CommitmentsUnknown is a ledger with no trading history to credit the days
	// ahead with. The curve is then bills against nothing, so it cannot answer
	// — and a consumer must say that rather than report the bills as unpayable.
	// It is the same guard ExpectsReceipts carries, named for this question.
	CommitmentsUnknown CommitmentCoverage = "sem_historico"
)

// DayCash is one day of the runway, split into what is known and what is
// expected — because they are answerable in different ways. ScheduledOut is a
// bill someone can call about; ExpectedIn is a rhythm nobody controls.
type DayCash struct {
	Date string `json:"date"`
	// Balance is the projected balance at the end of the day: everything
	// booked up to it, plus the receipts the days from today on are expected
	// to bring on top of what they have booked. Same basis as
	// EndOfMonthProjection, one day out instead of at the end.
	Balance int64 `json:"balance"`
	// ScheduledIn and ScheduledOut are what is already booked to land and to
	// leave on the day — a crediário instalment falling due, the rent.
	ScheduledIn  int64 `json:"scheduledIn"`
	ScheduledOut int64 `json:"scheduledOut"`
	// ExpectedIn is what an ordinary day of that weekday still brings beyond
	// what it has booked, and it is zero when the day has already booked more
	// than its weekday usually receives. Zero also when there is no trading
	// history at all — see ExpectsReceipts, which is what tells the two apart.
	ExpectedIn int64 `json:"expectedIn"`
}

// KPIs are the headline numbers: the state of the month *now*, today included.
// They are not a measurement of it — that is Trends, over its own labelled
// window — and they are emphatically not the whole month. Every figure here
// covers the days that have arrived and no others.
type KPIs struct {
	// Resultado is what came in minus what went out over the days so far. It
	// used to be summary.ExpectedBalance, which is the whole month's expected
	// inflow minus the whole month's expense — every bill of the month, booked
	// on the 1st, against sales nobody had made yet. On the 3rd it read minus
	// R$ 14.800,00 for a month that was actually R$ 1.200,00 up.
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
	// Despesa is what has actually gone out, by effective date, over the days so
	// far. What is booked for the rest of the month is DespesaAgendada and is
	// deliberately a separate number.
	Despesa int64 `json:"despesa"`
	// DespesaAgendada is what is already booked for the days still to come. It
	// is a commitment, not a spend: the rent due on the 25th is real and worth
	// seeing, but adding it to Despesa reported it as money gone, and
	// subtracting it inside Resultado reported the month as lost on its 3rd day.
	DespesaAgendada int64 `json:"despesaAgendada"`

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
// zero at the start of a month. Added projection.basis.
//
// The diff never reads projection directly — it compares health.status and the
// three KPIs — but health.status derives from projection.onTrack, so without the
// bump the deploy day would report a phantom "saúde melhorou" on every ledger
// whose projection crossed its target under the new basis.
//
// Note what a bump costs: the handler refuses to *serve* a snapshot of another
// version, not merely to compare it, so GET /analysis answers 404 from the
// deploy until the notifier writes the next one (or someone calls POST
// /analysis). Nothing consumes that endpoint today — the dashboard reads
// /analysis/monthly, which is computed live — but a future consumer would see a
// gap of up to a day.
//
// 5: month-over-month figures changed window — trends and every "vs mês
// passado" line now close on whole weeks (period.comparableThroughDay) instead
// of on the last finished day, so they are empty through the 7th rather than
// comparing mismatched weekdays. cashPosition's forward figures changed basis:
// the days from today on are credited with an ordinary day's receipts instead
// of standing as bills against nothing. Added period.comparableThroughDay and
// cashPosition.expectsReceipts. health.status reads the same trends, so the
// same phantom-movement argument as 4 applies here.
//
// 6: kpis.resultado and kpis.despesa cover the days that have arrived instead
// of the whole month including what is only booked for later in it. Added
// kpis.despesaAgendada, which carries what they used to include. The diff reads
// both of these directly, and this is exactly the reading it was getting wrong:
// booking the month's rent moved kpis.despesa with nothing having left the
// account, and the digest reported it as the month's spending having risen.
// Also in 6: recommendations[0] is the projection verdict rather than the
// weekly-pace one, and consumers read the list whole instead of skipping it.
// Added projection.coverage, projection.status and projection.todayTarget, all
// verdicts the consumers used to derive for themselves or not have at all.
//
// 7: weekdays no longer carry a Portuguese label and weekComparison.labels
// became weekComparison.days — every weekday travels as time.Weekday (0=Sunday)
// and is named by whatever renders it. projection.todayTarget.valid became
// .state, which says *why* there is no ask today instead of collapsing five
// reasons and a success into one false. Nothing the diff reads changed meaning,
// but the fields it would read on an old snapshot are gone, and reading a
// missing `state` as "" would grade every stored day as an absent target.
//
// 8: the per-day ask stopped being exclusive to today. Added
// projection.plan — the distribution the asks are slices of, published so any
// day can be priced from an assembled Analysis (Analysis.DayTargetOn), this one
// or one read back from a snapshot. Added dayTarget.date, dayTarget.realized
// and dayTarget.basis, which says whether a day's figures are a result, a day
// in progress or a bet: the same three numbers otherwise read identically
// either side of today. Added cashPosition.nextDay and cashPosition.forecast.
// See ADR-020 and ADR-021.
//
// Additions alone would not normally move this number. Two things here do. A v7
// snapshot read into this struct yields day targets whose `basis` is "", and ""
// is the one thing DayBasis has no meaning for — every real value names a
// regime, and an unnamed one is exactly what the field exists to prevent.
// And cashPosition's forward figures now stand beside the curve they are read
// off, so a stored snapshot that has the scalars and not the series is a
// half-answer to the page that draws it.
//
// It is one step rather than two because nothing between 7 and here was ever
// deployed: ADR-020 announced this bump and ADR-021 reshaped what it carries
// before any of it reached main. A version that no stored snapshot was ever
// written under is not a version, and leaving a gap in this list would suggest
// there are snapshots out there wearing it.
// 9: the rest of the month stopped counting as spent. goals.expenseActual,
// expenseComposition, cashOutDays and highlights all covered the whole month,
// including bills merely booked for days still ahead, while kpis.despesa had
// covered only the days that have arrived since 6. So one Analysis carried two
// despesas — the KPI row's and everything else's — and the bot read both out of
// the same payload: "gastei R$ 3.000,00" beside "usei 60% do teto (R$
// 9.000,00)" and a composition totalling the second. Highlights had the same
// hole and it showed worst: a day still in the future, carrying a booked bill
// and no sales, was reported as the month's worst day at R$ 0,00.
//
// No field is added or renamed — four of them change meaning, which is what
// this number is for. The diff does not read them, but the dashboard serves the
// stored snapshot, so without the bump a v8 snapshot written this morning would
// keep drawing the whole-month figures beside a KPI row that no longer agrees
// with them. What is booked for later is kpis.despesaAgendada, unchanged, and
// whether there is money for it is cashPosition.commitments (ADR-022).
//
// This one *is* a step of its own: unlike the run up to 8, v8 shipped and
// snapshots were written under it.
//
// 11: a day's ask stopped being allowed below what that day of the week
// usually brings. projection.plan.factor is floored at 1, so
// projection.todayTarget.target — and every day get_meta_do_dia prices — is now
// the greater of the gap's share and the weekday average, and
// todayTarget.delta can no longer be negative. Added projection.plannedClose
// (where the month lands if the asks are met), projection.plan.gapFactor (what
// the gap alone asked for, which is what still says the month is ahead) and
// dayTarget.source. See ADR-025.
//
// The bump is for the change of meaning, not the additions. A v10 snapshot read
// into this struct yields day targets whose `source` is empty and whose target
// may sit below `historical` — a combination this version's consumers are
// entitled to treat as impossible, and the wording keyed off it ("mantendo a
// média") would be printed over an ask that is not the average at all. The
// stored snapshot is what the dashboard serves, so the morning after a deploy
// would show yesterday's under-average asks in this version's words.
//
// 10: added revenueComposition — the month's faturamento split by category —
// and the category names in both compositions now come from the user's own
// catalog instead of the default definitions alone, because the catalog became
// theirs to extend from WhatsApp (ADR-024). cashOutDays still carries raw
// slugs; naming those is a separate change, and its consumers already know it.
//
// An addition alone would not move this number, and the rename would not either
// — ExpenseComposition became CategoryComposition in Go, with the JSON field
// untouched. What moves it is that a v9 snapshot read into this struct has no
// revenueComposition at all, and an empty composition is how this package says
// "nothing was sold this month". The dashboard serves the stored snapshot, so
// without the bump a month of atacado and balcão would render as a month with
// no sales in it — the exact reading the section was added to prevent.
const SchemaVersion = 11

// Analysis is the full picture of one month — the payload of
// GET /analysis/monthly, and the input every consumer renders from.
type Analysis struct {
	// Schema is SchemaVersion at the time this Analysis was built. Zero means
	// a snapshot stored before versioning existed, which is never comparable.
	Schema             int                   `json:"schemaVersion"`
	Month              string                `json:"month"`
	Period             Period                `json:"period"`
	KPIs               KPIs                  `json:"kpis"`
	Health             Health                `json:"health"`
	Trends             Trends                `json:"trends"`
	Weekdays           []WeekdayStat         `json:"weekdays"`
	WeekComparison     WeekComparison        `json:"weekComparison"`
	Highlights         Highlights            `json:"highlights"`
	CashOutDays        []CashOutDay          `json:"cashOutDays"`
	ExpenseComposition []CategoryComposition `json:"expenseComposition"`
	// RevenueComposition splits the month's faturamento by category — which
	// kinds of sale it was made of. It is the answer to the question a single
	// faturamento figure cannot take: a pharmacy that sells at atacado and at
	// balcão wants to know which of the two moved, and until this existed the
	// only breakdown in the analysis was of the money going out. See ADR-024.
	RevenueComposition []CategoryComposition `json:"revenueComposition"`
	Goals              GoalProgress          `json:"goals"`
	Projection         Projection            `json:"projection"`
	History            []MonthlySnapshot     `json:"history"`
	CashPosition       CashPosition          `json:"cashPosition"`
	Recommendations    []Recommendation      `json:"recommendations"`
}
