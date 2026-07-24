import type { MonthlySummary } from '../../api/types'
import type { GoalProgress, GoalInput, MonthlySnapshot, YearMonth } from './types'

export function getGoalProgress(
  summary: MonthlySummary,
  goal: GoalInput | null,
  now: Date,
  vendaBalcaoIncome: number,
): GoalProgress {
  const daysInMonth = new Date(
    now.getFullYear(),
    now.getMonth() + 1,
    0,
  ).getDate()
  const daysRemaining = daysInMonth - now.getDate()

  if (!goal) {
    return {
      revenueTarget: 0,
      revenueActual: vendaBalcaoIncome,
      revenuePct: 0,
      expenseTarget: 0,
      expenseActual: summary.TotalExpense,
      expensePct: 0,
      daysRemaining,
      daysTotal: daysInMonth,
    }
  }

  const revenuePct = goal.revenueTarget > 0
    ? Math.min(100, Math.round((vendaBalcaoIncome / goal.revenueTarget) * 100))
    : 0

  const expensePct = goal.expenseTarget > 0
    ? Math.min(100, Math.round((summary.TotalExpense / goal.expenseTarget) * 100))
    : 0

  return {
    revenueTarget: goal.revenueTarget,
    revenueActual: vendaBalcaoIncome,
    revenuePct,
    expenseTarget: goal.expenseTarget,
    expenseActual: summary.TotalExpense,
    expensePct,
    daysRemaining,
    daysTotal: daysInMonth,
  }
}

export function getHistory(
  summaries: MonthlySummary[],
  goals: GoalInput[],
  monthRange: string[],
): MonthlySnapshot[] {
  return monthRange.map((month, i) => {
    const summary = summaries[i]
    const goal = goals[i]
    const date = new Date(month + '-01T12:00:00')
    const label = date.toLocaleDateString('pt-BR', {
      month: 'short',
      year: 'numeric',
    })

    return {
      month: month as YearMonth,
      label,
      income: summary?.TotalIncome ?? 0,
      incomeTarget: goal?.revenueTarget ?? null,
      expense: summary?.TotalExpense ?? 0,
      expenseTarget: goal?.expenseTarget ?? null,
    }
  })
}
