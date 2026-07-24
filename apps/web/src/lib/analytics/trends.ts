import type { MonthlySummary } from '../../api/types'
import type { Trends, MonthTrend } from './types'

export function getTrends(
  current: MonthlySummary,
  previous: MonthlySummary | null,
): Trends {
  return {
    receita: buildTrend(current.TotalIncome, previous?.TotalIncome ?? 0),
    despesa: buildTrend(current.TotalExpense, previous?.TotalExpense ?? 0),
    resultado: buildTrend(current.Balance, previous?.Balance ?? 0),
  }
}

function buildTrend(current: number, previous: number): MonthTrend {
  if (previous === 0) {
    return {
      current,
      previous,
      change: current > 0 ? 100 : 0,
      direction: current > 0 ? 'up' : 'stable',
    }
  }

  const change = ((current - previous) / Math.abs(previous)) * 100
  const rounded = Math.round(change)

  let direction: MonthTrend['direction']
  if (rounded > 2) direction = 'up'
  else if (rounded < -2) direction = 'down'
  else direction = 'stable'

  return {
    current,
    previous,
    change: rounded,
    direction,
  }
}
