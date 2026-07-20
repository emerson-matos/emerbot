import { useEffect, useState } from 'react'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { Target } from 'lucide-react'
import { formatBRL } from '../api/client'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
  Table, TableHeader, TableBody, TableRow, TableHead, TableCell,
} from '@/components/ui/table'
import { useGoal, useMonthlySummary, useMonthlyTrend, useSaveGoalMutation } from '../api/queries'

// CSS `capitalize` (text-transform) uppercases every word, which is wrong
// for a multi-word Portuguese date like "abril de 2026" (→ "Abril De 2026").
// Capitalize only the leading letter instead.
function capitalizeFirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
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

export default function Goals() {
  const now = new Date()
  const currentMonth = format(now, 'yyyy-MM')
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(now.getFullYear(), now.getMonth() + offset, 1), 'yyyy-MM'),
  )

  const { data } = useGoal(currentMonth)
  const goal = data?.goal ?? null
  const summaryQuery = useMonthlySummary(currentMonth)
  const saveGoal = useSaveGoalMutation(currentMonth)

  const trendQueries = useMonthlyTrend(months3)
  const goal0 = useGoal(months3[0]).data?.goal ?? null
  const goal1 = useGoal(months3[1]).data?.goal ?? null
  const goal2 = useGoal(months3[2]).data?.goal ?? null
  const goalsByMonth = [goal0, goal1, goal2]

  const [revenueInput, setRevenueInput] = useState('')
  const [expenseInput, setExpenseInput] = useState('')

  useEffect(() => {
    if (goal) {
      setRevenueInput(String(goal.RevenueTarget / 100))
      setExpenseInput(String(goal.ExpenseTarget / 100))
    }
  }, [goal])


  const summary = summaryQuery.data ?? null
  const actualIncome = summary?.TotalIncome ?? 0
  const actualExpense = summary?.TotalExpense ?? 0

  const revenueTarget = Math.round(Number(revenueInput) * 100)
  const expenseTarget = Math.round(Number(expenseInput) * 100)
  const revPct = revenueTarget > 0 ? Math.min(100, (actualIncome / revenueTarget) * 100) : 0
  const expPct = expenseTarget > 0 ? Math.min(100, (actualExpense / expenseTarget) * 100) : 0
  const revColor = revPct >= 100 ? 'var(--success)' : 'var(--info)'
  const expColor = expPct > 100 ? 'var(--destructive)' : expPct >= 80 ? 'var(--warning)' : 'var(--info)'

  return (
    <div className="space-y-6">
      <Card>
        <CardContent className="space-y-5">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <Target className="size-4 text-primary" aria-hidden />
            Meta do Mês
          </h3>

          <div className="space-y-2">
            <label htmlFor="revenue-target" className="text-xs font-medium text-muted-foreground">
              Meta de faturamento (R$)
            </label>
            <Input
              id="revenue-target"
              type="number"
              min="0"
              step="0.01"
              value={revenueInput}
              onChange={e => setRevenueInput(e.target.value)}
            />
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">Faturamento</span>
              <span className="font-medium tabular-nums">
                {formatBRL(actualIncome)} / {formatBRL(revenueTarget)}
              </span>
            </div>
            <ProgressBar pct={revPct} color={revColor} />
          </div>

          <div className="space-y-2">
            <label htmlFor="expense-target" className="text-xs font-medium text-muted-foreground">
              Meta de despesas (R$)
            </label>
            <Input
              id="expense-target"
              type="number"
              min="0"
              step="0.01"
              value={expenseInput}
              onChange={e => setExpenseInput(e.target.value)}
            />
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">Despesas</span>
              <span className="font-medium tabular-nums">
                {formatBRL(actualExpense)} / {formatBRL(expenseTarget)}
              </span>
            </div>
            <ProgressBar pct={expPct} color={expColor} />
          </div>

          <Button
            onClick={() => saveGoal.mutate({ revenue_target: revenueTarget, expense_target: expenseTarget })}
            disabled={saveGoal.isPending}
          >
            Salvar Metas
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <h3 className="text-sm font-semibold">Histórico de Metas</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Mês</TableHead>
                <TableHead>Faturamento</TableHead>
                <TableHead>Despesas</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {months3.map((monthStr, i) => {
                const monthGoal = goalsByMonth[i]
                const trend = trendQueries[i].data
                const income = trend?.TotalIncome ?? 0
                const expense = trend?.TotalExpense ?? 0
                return (
                  <TableRow key={monthStr}>
                    <TableCell>
                      {capitalizeFirst(format(new Date(monthStr + '-01'), "MMMM 'de' yyyy", { locale: ptBR }))}
                    </TableCell>
                    <TableCell className="tabular-nums">
                      {formatBRL(income)} / {monthGoal ? formatBRL(monthGoal.RevenueTarget) : '—'}
                    </TableCell>
                    <TableCell className="tabular-nums">
                      {formatBRL(expense)} / {monthGoal ? formatBRL(monthGoal.ExpenseTarget) : '—'}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
