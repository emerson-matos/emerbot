package analytics

import (
	"context"
	"sort"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

const (
	recentRegimeDays      = 21
	minRegimeObservations = daysInWeek
	backtestMonths        = 3
)

var backtestCutoffs = [...]int{5, 10, 15, 20}

// ProjectionExperiment is deliberately outside Analysis: it is an observed
// candidate, not an input to the operational projection or its snapshots.
type ProjectionExperiment struct {
	Current  ExperimentEstimate `json:"current"`
	Backtest ExperimentBacktest `json:"backtest"`
}

type ExperimentEstimate struct {
	Available    bool    `json:"available"`
	Official     int64   `json:"official"`
	Experimental int64   `json:"experimental"`
	RecentFactor float64 `json:"recentFactor"`
	Observations int     `json:"observations"`
}

type ExperimentSample struct {
	Month        string `json:"month"`
	CutoffDay    int    `json:"cutoffDay"`
	ActualClose  int64  `json:"actualClose"`
	Official     int64  `json:"official"`
	Experimental int64  `json:"experimental"`
}

type ExperimentBacktest struct {
	Samples       []ExperimentSample       `json:"samples"`
	OfficialMAE   int64                    `json:"officialMae"`
	RegimeMAE     int64                    `json:"regimeMae"`
	OfficialWins  int                      `json:"officialWins"`
	RegimeWins    int                      `json:"regimeWins"`
	WeekdayErrors []ExperimentWeekdayError `json:"weekdayErrors"`
}

// ExperimentWeekdayError is an out-of-sample daily forecast error. Each day
// is priced from the eight complete weeks that end before it, never from a
// baseline that already includes the day's own sale.
type ExperimentWeekdayError struct {
	Day          time.Weekday `json:"day"`
	MAE          int64        `json:"mae"`
	Observations int          `json:"observations"`
}

// AssembleProjectionExperiment reads transaction-basis revenue once, then all
// candidate cuts reuse that in-memory slice. It is intentionally not part of
// Assemble: observing a model must not slow the dashboard, notifier or bot.
func AssembleProjectionExperiment(ctx context.Context, store LedgerReader, userID string, now time.Time) (ProjectionExperiment, error) {
	from := experimentFrom(now)
	entries, err := rangeEntries(ctx, store, userID, from, now, pkgfinance.BasisTransaction)
	if err != nil {
		return ProjectionExperiment{}, err
	}
	return BuildProjectionExperiment(entries, now), nil
}

func experimentFrom(now time.Time) time.Time {
	oldest := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, now.Location()).AddDate(0, -backtestMonths, 0)
	cutoff := oldest.AddDate(0, 0, backtestCutoffs[0]-1)
	clock := newMonthClock(oldest.Format("2006-01"), cutoff)
	from, _ := clock.projectionWindow()
	return from.Time()
}

// BuildProjectionExperiment is pure so the same calculation used live is what
// the backtest evaluates. A cutoff is the end of its named calendar day: entry
// timestamps do not exist, therefore intraday historical simulation would be
// made-up precision.
func BuildProjectionExperiment(entries []domain.FinancialEntry, now time.Time) ProjectionExperiment {
	month := now.Format("2006-01")
	current := estimateAt(entries, month, now)
	backtest := ExperimentBacktest{}

	for offset := backtestMonths; offset >= 1; offset-- {
		first := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, now.Location()).AddDate(0, -offset, 0)
		month := first.Format("2006-01")
		clock := newMonthClock(month, first)
		actualClose := revenueThrough(entries, clock, clock.total)
		for _, cutoff := range backtestCutoffs {
			if cutoff > clock.total {
				continue
			}
			at := first.AddDate(0, 0, cutoff-1)
			estimate := estimateAt(entries, month, at)
			if !estimate.Available {
				continue
			}
			backtest.Samples = append(backtest.Samples, ExperimentSample{
				Month: month, CutoffDay: cutoff, ActualClose: actualClose,
				Official: estimate.Official, Experimental: estimate.Experimental,
			})
		}
	}

	for _, sample := range backtest.Samples {
		officialErr := absInt64(sample.Official - sample.ActualClose)
		regimeErr := absInt64(sample.Experimental - sample.ActualClose)
		backtest.OfficialMAE += officialErr
		backtest.RegimeMAE += regimeErr
		switch {
		case officialErr < regimeErr:
			backtest.OfficialWins++
		case regimeErr < officialErr:
			backtest.RegimeWins++
		}
	}
	if n := int64(len(backtest.Samples)); n > 0 {
		backtest.OfficialMAE /= n
		backtest.RegimeMAE /= n
	}
	backtest.WeekdayErrors = weekdayForecastErrors(entries, now)

	return ProjectionExperiment{Current: current, Backtest: backtest}
}

func weekdayForecastErrors(entries []domain.FinancialEntry, now time.Time) []ExperimentWeekdayError {
	type total struct {
		error int64
		count int
	}
	var totals [daysInWeek]total
	lastClosed := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	for day := lastClosed.AddDate(0, 0, -(recentRegimeDays - 1)); !day.After(lastClosed); day = day.AddDate(0, 0, 1) {
		end := domain.NewCalendarDate(day.AddDate(0, 0, -1))
		from := domain.NewCalendarDate(end.Time().AddDate(0, 0, -(projectionWindowWeeks*daysInWeek - 1)))
		baseline := projectionRates(entries, from, end).avg[int(day.Weekday())]
		if baseline <= 0 {
			continue
		}
		actual := revenueOnDate(entries, domain.NewCalendarDate(day))
		bucket := &totals[int(day.Weekday())]
		bucket.error += absInt64(actual - baseline)
		bucket.count++
	}

	errors := make([]ExperimentWeekdayError, 0, daysInWeek)
	for day, total := range totals {
		if total.count == 0 {
			continue
		}
		errors = append(errors, ExperimentWeekdayError{
			Day: time.Weekday(day), MAE: total.error / int64(total.count), Observations: total.count,
		})
	}
	return errors
}

func estimateAt(entries []domain.FinancialEntry, month string, now time.Time) ExperimentEstimate {
	clock := newMonthClock(month, now)
	from, to := clock.projectionWindow()
	rates := projectionRates(entries, from, to)
	asOf := clock.today
	actual := revenueThrough(entries, clock, asOf)
	todayRevenue := revenueOnDate(entries, domain.NewCalendarDate(clock.dayOf(asOf)))
	_, official := projectedClose(rates, 1, clock, actual, todayRevenue, asOf)
	factor, observations, ok := recentFactor(entries, rates, from, to)
	if !ok {
		return ExperimentEstimate{Official: official}
	}
	_, experimental := projectedClose(rates, factor, clock, actual, todayRevenue, asOf)
	return ExperimentEstimate{true, official, experimental, factor, observations}
}

func recentFactor(entries []domain.FinancialEntry, rates dailyRates, from, to domain.CalendarDate) (float64, int, bool) {
	start := to.Time().AddDate(0, 0, -(recentRegimeDays - 1))
	if start.Before(from.Time()) {
		start = from.Time()
	}
	amounts := map[string]int64{}
	for _, entry := range entries {
		if domain.IsRevenue(entry) && within(entry.TransactionDate, domain.NewCalendarDate(start), to) {
			amounts[entry.TransactionDate.String()] += entry.Amount
		}
	}
	ratios := make([]float64, 0, recentRegimeDays)
	for day := start; !day.After(to.Time()); day = day.AddDate(0, 0, 1) {
		baseline := rates.avg[int(day.Weekday())]
		if baseline > 0 {
			ratios = append(ratios, float64(amounts[day.Format("2006-01-02")])/float64(baseline))
		}
	}
	if len(ratios) < minRegimeObservations {
		return 0, len(ratios), false
	}
	sort.Float64s(ratios)
	middle := len(ratios) / 2
	median := ratios[middle]
	if len(ratios)%2 == 0 {
		median = (ratios[middle-1] + ratios[middle]) / 2
	}
	return median, len(ratios), true
}

func revenueThrough(entries []domain.FinancialEntry, clock monthClock, through int) int64 {
	if through <= 0 {
		return 0
	}
	var total int64
	for _, entry := range entries {
		if domain.IsRevenue(entry) && within(entry.TransactionDate, domain.NewCalendarDate(clock.first), domain.NewCalendarDate(clock.dayOf(through))) {
			total += entry.Amount
		}
	}
	return total
}

func revenueOnDate(entries []domain.FinancialEntry, date domain.CalendarDate) int64 {
	var total int64
	for _, entry := range entries {
		if domain.IsRevenue(entry) && entry.TransactionDate.String() == date.String() {
			total += entry.Amount
		}
	}
	return total
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
