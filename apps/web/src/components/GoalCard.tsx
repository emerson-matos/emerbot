import { useEffect, useState } from 'react'
import { Target } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { api, formatBRL } from '../api/client'
import type { Goal, MonthlySummary } from '../api/client'

interface Props {
  month: string
  summary: MonthlySummary | null
}

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

export default function GoalCard({ month, summary }: Props) {
  const [goal, setGoal] = useState<Goal | null>(null)

  useEffect(() => {
    api.goals.get(month).then(res => {
      if (res.goal) setGoal(res.goal)
    }).catch(() => {})
  }, [month])

  const actualIncome = summary?.TotalIncome ?? 0
  const actualExpense = summary?.TotalExpense ?? 0
  const revPct = goal?.RevenueTarget && goal.RevenueTarget > 0
    ? Math.min(100, (actualIncome / goal.RevenueTarget) * 100) : 0
  const expPct = goal?.ExpenseTarget && goal.ExpenseTarget > 0
    ? Math.min(100, (actualExpense / goal.ExpenseTarget) * 100) : 0
  const revColor = revPct >= 100 ? 'var(--success)' : 'var(--info)'
  const expColor = expPct > 100 ? 'var(--destructive)' : expPct >= 80 ? 'var(--warning)' : 'var(--info)'

  return (
    <Card>
      <CardContent className="space-y-3">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <Target className="size-4 text-primary" aria-hidden />
          Meta do Mês
        </h3>
        {goal ? (
          <div className="space-y-3">
            <div>
              <div className="mb-1 flex justify-between text-xs">
                <span className="text-muted-foreground">Faturamento</span>
                <span className="font-medium tabular-nums">{formatBRL(actualIncome)} / {formatBRL(goal.RevenueTarget)}</span>
              </div>
              <ProgressBar pct={revPct} color={revColor} />
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
          </p>
        )}
      </CardContent>
    </Card>
  )
}
