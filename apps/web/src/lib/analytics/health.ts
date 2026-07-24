import type { Entry, MonthlySummary } from '../../api/types'
import {
  FinancialHealthStatus,
  InsightType,
  InsightSeverity,
  type FinancialHealth,
  type Insight,
  type WeekComparison,
  type GoalProgress,
} from './types'
import { formatBRL } from '@/lib/format'

export function getHealth(
  entries: Entry[],
  summary: MonthlySummary,
  previousSummary: MonthlySummary | null,
  weekComparison: WeekComparison,
  goals: GoalProgress,
): FinancialHealth {
  const { TotalIncome, TotalExpense, Balance } = summary
  const messages: Insight[] = []

  const positiveDays = countPositiveDays(entries)
  const totalDays = countDaysWithEntries(entries)

  if (Balance > 0) {
    messages.push({
      type: InsightType.GoodPerformance,
      severity: InsightSeverity.Info,
      title: 'Resultado positivo',
      description: 'Receitas maiores que despesas',
    })
  }

  if (totalDays > 0) {
    messages.push({
      type: InsightType.GoodPerformance,
      severity: InsightSeverity.Info,
      title: `${positiveDays} dos ${totalDays} dias`,
      description: 'Dias fecharam no azul',
    })
  }

  if (TotalIncome > 0) {
    const pct = Math.round((TotalExpense / TotalIncome) * 100)
    messages.push({
      type: InsightType.GoodPerformance,
      severity: InsightSeverity.Info,
      title: `Despesas representam ${pct}%`,
      description: 'das receitas',
    })
  }

  if (previousSummary) {
    const prevExpense = previousSummary.TotalExpense
    const prevIncome = previousSummary.TotalIncome

    if (prevIncome > 0 && TotalIncome > 0) {
      const incomeChange = ((TotalIncome - prevIncome) / prevIncome) * 100
      const expenseChange = prevExpense > 0
        ? ((TotalExpense - prevExpense) / prevExpense) * 100
        : 0

      if (expenseChange > 10 && incomeChange < expenseChange) {
        messages.push({
          type: InsightType.ExpenseGrowth,
          severity: InsightSeverity.Warning,
          title: 'Despesas cresceram',
          description: `${Math.round(expenseChange)}% acima do mês passado`,
          value: expenseChange,
        })
      }

      if (incomeChange < -10) {
        messages.push({
          type: InsightType.RevenueDrop,
          severity: InsightSeverity.Warning,
          title: 'Receitas cairam',
          description: `${Math.round(Math.abs(incomeChange))}% abaixo do mês passado`,
          value: incomeChange,
        })
      }
    }
  }

  if (Balance < 0) {
    messages.push({
      type: InsightType.LowCashFlow,
      severity: InsightSeverity.Critical,
      title: 'Fluxo negativo',
      description: 'Resultado negativo no mês',
    })
  }

  if (weekComparison.previousUpToDay !== 0) {
    const weekPct = ((weekComparison.current - weekComparison.previousUpToDay) / weekComparison.previousUpToDay) * 100
    if (weekPct > 5) {
      messages.push({
        type: InsightType.WeeklyImprovement,
        severity: InsightSeverity.Info,
        title: 'Ritmo subiu vs semana passada',
        description: `${Math.round(weekPct)}% acima`,
        value: weekPct,
      })
    } else if (weekPct < -5) {
      messages.push({
        type: InsightType.WeeklyDecline,
        severity: InsightSeverity.Warning,
        title: 'Ritmo caiu vs semana passada',
        description: `${Math.round(Math.abs(weekPct))}% abaixo`,
        value: weekPct,
      })
    }
  }

  if (goals.revenueTarget > 0 && goals.daysRemaining > 0 && goals.daysTotal > 0) {
    const elapsed = goals.daysTotal - goals.daysRemaining
    if (elapsed > 0) {
      const currentDailyRate = goals.revenueActual / elapsed
      const neededPerDay = (goals.revenueTarget - goals.revenueActual) / goals.daysRemaining
      const onTrack = currentDailyRate >= neededPerDay * 1.05

      if (onTrack) {
        messages.push({
          type: InsightType.GoalOnTrack,
          severity: InsightSeverity.Info,
          title: 'No ritmo para bater a meta',
          description: `Necessário ${formatBRL(neededPerDay)}/dia — você está acima`,
        })
      } else {
        messages.push({
          type: InsightType.GoalBehind,
          severity: InsightSeverity.Warning,
          title: 'Precisa acelerar para bater a meta',
          description: `Necessário ${formatBRL(neededPerDay)}/dia nos próximos ${goals.daysRemaining} dias`,
          value: neededPerDay,
        })
      }
    }
  }

  const status = getStatus(Balance, messages)

  return { status, messages }
}

function countPositiveDays(entries: Entry[]): number {
  const byDate = new Map<string, number>()

  for (const e of entries) {
    const date = e.TransactionDate.slice(0, 10)
    const current = byDate.get(date) ?? 0
    byDate.set(date, current + ((e.Type === 'income' && e.Category === 'venda_balcao') ? e.Amount : -e.Amount))
  }

  let count = 0
  for (const balance of byDate.values()) {
    if (balance > 0) count++
  }
  return count
}

function countDaysWithEntries(entries: Entry[]): number {
  const days = new Set<string>()
  for (const e of entries) {
    days.add(e.TransactionDate.slice(0, 10))
  }
  return days.size
}

function getStatus(
  balance: number,
  messages: Insight[],
): FinancialHealthStatus {
  if (balance < 0) return FinancialHealthStatus.Critico
  if (messages.some(m => m.severity === InsightSeverity.Critical)) return FinancialHealthStatus.Critico
  if (messages.some(m => m.severity === InsightSeverity.Warning)) return FinancialHealthStatus.Atencao
  return FinancialHealthStatus.Boa
}
