import { useEffect, useState } from 'react'
import { Card, CardContent } from '@/components/ui/card'
import { api, formatBRL } from '../api/client'
import type { Goal, MonthlySummary } from '../api/client'

interface Props {
  month: string
  summary: MonthlySummary | null
}

export default function GoalCard({ month, summary }: Props) {
  const [goal, setGoal] = useState<Goal | null>(null)

  useEffect(() => {
    api.goals.get(month).then(res => {
      if (res.goal) setGoal(res.goal)
    })
  }, [month])

  const actualIncome = summary?.TotalIncome ?? 0
  const actualExpense = summary?.TotalExpense ?? 0
  const revPct = goal?.RevenueTarget && goal.RevenueTarget > 0
    ? Math.min(100, (actualIncome / goal.RevenueTarget) * 100) : 0
  const expPct = goal?.ExpenseTarget && goal.ExpenseTarget > 0
    ? Math.min(100, (actualExpense / goal.ExpenseTarget) * 100) : 0
  const revColor = revPct >= 100 ? 'bg-green-500' : 'bg-blue-500'
  const expColor = expPct > 100 ? 'bg-red-500' : expPct >= 80 ? 'bg-yellow-500' : 'bg-blue-500'

  return (
    <Card>
      <CardContent className="p-4 sm:p-5 space-y-3">
        <h3 className="text-sm font-semibold text-card-foreground">🎯 Meta do Mês</h3>
        {goal ? (
          <div className="space-y-3">
            <div>
              <div className="flex justify-between text-xs mb-1">
                <span className="text-muted-foreground">Faturamento</span>
                <span className="font-medium">{formatBRL(actualIncome)} / {formatBRL(goal.RevenueTarget)}</span>
              </div>
              <div className="h-2 bg-muted rounded-full overflow-hidden">
                <div className={`h-full rounded-full transition-all ${revColor}`} style={{ width: `${Math.min(100, revPct)}%` }} />
              </div>
            </div>
            <div>
              <div className="flex justify-between text-xs mb-1">
                <span className="text-muted-foreground">Despesas</span>
                <span className="font-medium">{formatBRL(actualExpense)} / {formatBRL(goal.ExpenseTarget)}</span>
              </div>
              <div className="h-2 bg-muted rounded-full overflow-hidden">
                <div className={`h-full rounded-full transition-all ${expColor}`} style={{ width: `${Math.min(100, expPct)}%` }} />
              </div>
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground text-center py-2">Defina sua meta pelo WhatsApp com <span className="font-mono font-semibold text-foreground">/meta</span></p>
        )}
      </CardContent>
    </Card>
  )
}
