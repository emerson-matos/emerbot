import type { Analysis, AnalysisInput, WeekComparison } from './types'
import { getHealth } from './health'
import { getRecommendations } from './recommendations'
import { getTrends } from './trends'
import { getWeekdayStats } from './weekday'
import { getHighlights, getCashOutDays } from './highlights'
import { getExpenseComposition } from './composition'
import { getGoalProgress, getHistory } from './goals'
import { getCashPosition } from './cashPosition'

const WEEKDAY_LABELS = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb']

export function buildMonthlyAnalysis(input: AnalysisInput): Analysis {
  const { entries, previousEntries, summaries, goals, cashFlowPoints, month, now } = input
  const currentSummary = summaries[0]
  const previousSummary = summaries[1] ?? null
  const currentGoal = goals[0] ?? null

  const currentDay = now.getDate()
  const previousMonthIncomeUpToDay = previousEntries
    .filter(e => {
      if (e.Type !== 'income' || e.Category !== 'venda_balcao') return false
      const day = new Date(e.TransactionDate).getDate()
      return day <= currentDay
    })
    .reduce((sum, e) => sum + e.Amount, 0)

  const vendaBalcaoIncome = entries
    .filter(e => e.Type === 'income' && e.Category === 'venda_balcao')
    .reduce((sum, e) => sum + e.Amount, 0)

  const kpis = {
    resultado: currentSummary?.Balance ?? 0,
    receita: currentSummary?.TotalIncome ?? 0,
    despesa: currentSummary?.TotalExpense ?? 0,
    daysRemaining: now.getDate() > 0
      ? new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate() - now.getDate()
      : 0,
    previousMonthIncomeUpToDay,
  }

  const weekComparison = getWeekComparison(
    entries,
    now,
    vendaBalcaoIncome,
    currentGoal?.revenueTarget ?? 0,
  )
  const goals_ = getGoalProgress(
    currentSummary ?? { Month: month, TotalIncome: 0, TotalExpense: 0, Balance: 0 },
    currentGoal,
    now,
    vendaBalcaoIncome,
  )

  const health = getHealth(
    entries,
    currentSummary ?? { Month: month, TotalIncome:0, TotalExpense: 0, Balance: 0 },
    previousSummary,
    weekComparison,
    goals_,
  )
  const trends = getTrends(
    currentSummary ?? { Month: month, TotalIncome: 0, TotalExpense: 0, Balance: 0 },
    previousSummary,
  )
  const weekdays = getWeekdayStats(entries, now)
  const highlights = getHighlights(entries)
  const cashOutDays = getCashOutDays(entries)
  const expenseComposition = getExpenseComposition(entries)
  const history = getHistory(summaries, goals, getMonthRange(month))
  const cashPosition = getCashPosition(cashFlowPoints, now)
  const recommendations = getRecommendations({ weekComparison, goals: goals_, trends, cashPosition })

  return {
    kpis,
    health,
    trends,
    weekdays,
    weekComparison,
    highlights,
    cashOutDays,
    expenseComposition,
    goals: goals_,
    history,
    cashPosition,
    recommendations,
  }
}

function getWeekComparison(
  entries: import('../../api/types').Entry[],
  now: Date,
  currentIncome: number,
  monthlyTarget: number,
): WeekComparison {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const dayOfWeek = today.getDay()
  const mondayOffset = dayOfWeek === 0 ? -6 : 1 - dayOfWeek
  const thisMonday = new Date(today)
  thisMonday.setDate(today.getDate() + mondayOffset)
  const thisMondayStr = thisMonday.toISOString().slice(0, 10)

  const lastMonday = new Date(thisMonday)
  lastMonday.setDate(thisMonday.getDate() - 7)
  const lastMondayStr = lastMonday.toISOString().slice(0, 10)

  const lastSunday = new Date(thisMonday)
  lastSunday.setDate(thisMonday.getDate() - 1)
  const lastSundayStr = lastSunday.toISOString().slice(0, 10)

  let current = 0
  let previous = 0
  let previousUpToDay = 0

  for (const e of entries) {
    if (e.Type !== 'income' || e.Category !== 'venda_balcao') continue
    const date = e.TransactionDate.slice(0, 10)
    if (date >= thisMondayStr && date <= now.toISOString().slice(0, 10)) {
      current += e.Amount
    } else if (date >= lastMondayStr && date <= lastSundayStr) {
      previous += e.Amount
    }
  }

  // Previous week up to same day of week
  const lastSameDay = new Date(lastMonday)
  lastSameDay.setDate(lastMonday.getDate() + (dayOfWeek === 0 ? 6 : dayOfWeek - 1))
  const lastSameDayStr = lastSameDay.toISOString().slice(0, 10)
  for (const e of entries) {
    if (e.Type !== 'income' || e.Category !== 'venda_balcao') continue
    const date = e.TransactionDate.slice(0, 10)
    if (date >= lastMondayStr && date <= lastSameDayStr) {
      previousUpToDay += e.Amount
    }
  }

  const labels: string[] = []
  for (let i = 0; i < 7; i++) {
    const d = new Date(thisMonday)
    d.setDate(thisMonday.getDate() + i)
    if (d > now) break
    labels.push(WEEKDAY_LABELS[d.getDay()])
  }

  const avgPerDay = previous / 7
  const remainingDays = dayOfWeek === 0 ? 0 : 7 - dayOfWeek
  const projectedWeekly = current + (avgPerDay * remainingDays)
  const daysInMonth = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()
  const dailyRate = projectedWeekly / 7
  const daysRemaining = daysInMonth - now.getDate()
  const projectedMonthly = currentIncome + (dailyRate * daysRemaining)

  return { current, previous, previousUpToDay, projectedWeekly, projectedMonthly, monthlyTarget, labels }
}

function getMonthRange(currentMonth: string): string[] {
  const [y, m] = currentMonth.split('-').map(Number)
  return [-2, -1, 0].map(offset => {
    const date = new Date(y, m - 1 + offset, 1)
    return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`
  })
}
