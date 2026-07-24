import type { Entry } from '../../api/types'
import type { DayHighlight, CashOutDay } from './types'

type DayData = {
  date: string
  income: number
  expense: number
  balance: number
}

function aggregateByDay(entries: Entry[]): Map<string, DayData> {
  const byDate = new Map<string, DayData>()

  for (const e of entries) {
    const date = e.TransactionDate.slice(0, 10)
    let day = byDate.get(date)
    if (!day) {
      day = { date, income: 0, expense: 0, balance: 0 }
      byDate.set(date, day)
    }
    if (e.Type === 'income' && e.Category === 'venda_balcao') {
      day.income += e.Amount
    } else {
      day.expense += e.Amount
    }
    day.balance = day.income - day.expense
  }

  return byDate
}

export function getHighlights(entries: Entry[]): {
  bestIncome: DayHighlight
  worstIncome: DayHighlight
  bestBalance: DayHighlight
  worstBalance: DayHighlight
} {
  const byDate = aggregateByDay(entries)
  const days = Array.from(byDate.values())

  const empty: DayHighlight = { date: '—', label: 'Sem dados', amount: 0 }

  if (days.length === 0) {
    return {
      bestIncome: empty,
      worstIncome: empty,
      bestBalance: empty,
      worstBalance: empty,
    }
  }

  const bestIncome = days.reduce((best, d) =>
    d.income > best.income ? d : best,
  )
  const worstIncome = days.reduce((worst, d) =>
    d.income < worst.income ? d : worst,
  )
  const bestBalance = days.reduce((best, d) =>
    d.balance > best.balance ? d : best,
  )
  const worstBalance = days.reduce((worst, d) =>
    d.balance < worst.balance ? d : worst,
  )

  return {
    bestIncome: toHighlight(bestIncome, 'income'),
    worstIncome: toHighlight(worstIncome, 'income'),
    bestBalance: toHighlight(bestBalance, 'balance'),
    worstBalance: toHighlight(worstBalance, 'balance'),
  }
}

function toHighlight(
  day: DayData,
  type: 'income' | 'balance',
): DayHighlight {
  const amount = type === 'income' ? day.income : day.balance
  const date = new Date(day.date + 'T12:00:00')
  const label = date.toLocaleDateString('pt-BR', {
    day: '2-digit',
    month: 'short',
  })
  return { date: day.date, label, amount }
}

export function getCashOutDays(entries: Entry[]): CashOutDay[] {
  const byDate = new Map<string, { total: number; items: Map<string, { amount: number; count: number }> }>()

  for (const e of entries) {
    if (e.Type !== 'expense') continue
    const date = e.TransactionDate.slice(0, 10)
    let bucket = byDate.get(date)
    if (!bucket) {
      bucket = { total: 0, items: new Map() }
      byDate.set(date, bucket)
    }
    bucket.total += e.Amount
    const existing = bucket.items.get(e.Category)
    if (existing) {
      existing.amount += e.Amount
      existing.count += 1
    } else {
      bucket.items.set(e.Category, { amount: e.Amount, count: 1 })
    }
  }

  return Array.from(byDate.entries())
    .map(([date, data]) => ({
      date,
      total: data.total,
      items: Array.from(data.items.entries())
        .map(([category, v]) => ({ category, amount: v.amount, count: v.count }))
        .sort((a, b) => b.amount - a.amount),
    }))
    .sort((a, b) => b.total - a.total)
    .slice(0, 5)
}
