import { format } from 'date-fns'
import { Target } from 'lucide-react'
import { Link } from 'react-router-dom'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { formatBRL } from '@/lib/format'
import { useGoal } from '../api/queries'
import { useMonthlyAnalysis } from '../hooks/useMonthlyAnalysis'
import type { YearMonth } from '@/api/types'

function ProgressBar({ pct, color }: { pct: number; color: string }) {
  return (
    <div className="h-2 overflow-hidden rounded-full bg-muted">
      <div
        className="h-full rounded-full transition-[width] duration-500"
        style={{ width: `${Math.min(100, pct)}%`, background: color }}
      />
    </div>
  )
}

export default function GoalCard() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM') as YearMonth
  // Receita here is the same figure the backend computes for the Analysis
  // page and the WhatsApp bot (packages/finance/analytics) — this card used
  // to re-derive its own narrower one (counter sales only) straight from the
  // entries, which drifted from the number shown everywhere else.
  const analysisQuery = useMonthlyAnalysis(currentMonth)
  const goalQuery = useGoal(currentMonth)
  const analysis = analysisQuery.data ?? null
  const goal = goalQuery.data?.goal ?? null

  if (analysisQuery.isLoading || goalQuery.isLoading) {
    return <Card className="min-h-26"><CardContent className="flex grow items-center justify-center"><Skeleton className="size-full rounded-xl" /></CardContent></Card>
  }

  if (analysisQuery.isError || goalQuery.isError) {
    return <Card className="min-h-26"><CardContent className="flex grow items-center justify-center"><p className="text-xs text-destructive">Erro ao carregar meta do mês</p></CardContent></Card>
  }

  const actualIncome = analysis?.goals.incomeActual ?? 0
  const actualExpense = analysis?.goals.expenseActual ?? 0
  const incomePct = goal?.IncomeTarget && goal.IncomeTarget > 0
    ? Math.min(100, (actualIncome / goal.IncomeTarget) * 100) : 0
  const expPct = goal?.ExpenseTarget && goal.ExpenseTarget > 0
    ? Math.min(100, (actualExpense / goal.ExpenseTarget) * 100) : 0
  const incomeColor = incomePct >= 100 ? 'var(--success)' : 'var(--info)'
  const expColor = expPct > 100 ? 'var(--destructive)' : expPct >= 80 ? 'var(--warning)' : 'var(--info)'

  return (
    <Card className="min-h-26">
      <CardContent className="space-y-3">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <Target className="size-4 text-primary" aria-hidden />
          Meta do Mês
        </h3>
        {goal ? (
          <div className="space-y-3">
            <div>
              <div className="mb-1 flex justify-between text-xs">
                <span className="text-muted-foreground">Receita</span>
                <span className="font-medium tabular-nums">{formatBRL(actualIncome)} / {formatBRL(goal.IncomeTarget)}</span>
              </div>
              <ProgressBar pct={incomePct} color={incomeColor} />
            </div>
            <div>
              <div className="mb-1 flex justify-between text-xs">
                <span className="text-muted-foreground">Despesas</span>
                <span className="font-medium tabular-nums">{formatBRL(actualExpense)} / {formatBRL(goal.ExpenseTarget)}</span>
              </div>
              <ProgressBar pct={expPct} color={expColor} />
            </div>
          </div>
        ) : (
          <p className="py-2 text-center text-xs text-muted-foreground">
            Defina sua meta pelo WhatsApp com{' '}
            <span className="rounded bg-muted px-1 py-0.5 font-mono font-semibold text-foreground">/meta</span>
            {' '}ou <Link to="/metas" className="text-primary underline">defina pelo painel</Link>
          </p>
        )}
      </CardContent>
    </Card>
  )
}
