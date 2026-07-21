import { useEffect, useState } from 'react'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import type { LucideIcon } from 'lucide-react'
import {
  BarChart3, CheckCircle2, Target, TrendingDown, TrendingUp,
} from 'lucide-react'
import { formatBRL } from '@/lib/format'
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

type MetaTone = 'positive' | 'negative' | 'info' | 'neutral'

const metaToneVar: Record<MetaTone, string> = {
  positive: 'var(--success)',
  negative: 'var(--destructive)',
  info: 'var(--info)',
  neutral: 'var(--primary)',
}

function MetaTile({ title, value, subtitle, icon: Icon, tone }: {
  title: string
  value: string
  subtitle: string
  icon: LucideIcon
  tone: MetaTone
}) {
  const c = metaToneVar[tone]
  return (
    <Card className="relative overflow-hidden">
      <span aria-hidden className="absolute inset-y-0 left-0 w-1" style={{ background: c }} />
      <CardContent className="flex items-start justify-between gap-3 pl-5">
        <div className="min-w-0">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{title}</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: c }}>{value}</p>
          <p className="mt-1 text-xs text-muted-foreground">{subtitle}</p>
        </div>
        <span
          className="grid size-9 shrink-0 place-items-center rounded-lg"
          style={{ background: `color-mix(in oklch, ${c} 14%, transparent)`, color: c }}
        >
          <Icon className="size-[18px]" />
        </span>
      </CardContent>
    </Card>
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
  const [saved, setSaved] = useState(false)

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

  const monthsHit = months3.reduce((count, _, i) => {
    const g = goalsByMonth[i]
    const t = trendQueries[i].data
    return g && t && t.TotalIncome >= g.RevenueTarget ? count + 1 : count
  }, 0)
  const loadedIncomes = trendQueries.filter(q => q.isSuccess).map(q => q.data!.TotalIncome)
  const avgRevenue = loadedIncomes.length
    ? Math.round(loadedIncomes.reduce((s, v) => s + v, 0) / loadedIncomes.length)
    : 0

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Metas</h1>
        <p className="mt-1 text-muted-foreground">Defina as metas financeiras do mês</p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MetaTile
          title="Progresso Faturamento"
          value={`${revPct.toFixed(0)}%`}
          subtitle="da meta deste mês"
          icon={TrendingUp}
          tone="positive"
        />
        <MetaTile
          title="Progresso Despesas"
          value={`${expPct.toFixed(0)}%`}
          subtitle="do limite deste mês"
          icon={TrendingDown}
          tone="negative"
        />
        <MetaTile
          title="Meses na Meta"
          value={`${monthsHit}/${months3.length}`}
          subtitle="faturamento atingido"
          icon={Target}
          tone="info"
        />
        <MetaTile
          title="Faturamento Médio"
          value={formatBRL(avgRevenue)}
          subtitle="últimos meses"
          icon={BarChart3}
          tone="neutral"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Card>
          <CardContent className="space-y-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold">
              <TrendingUp className="size-4 text-success" aria-hidden />
              Meta de Faturamento
            </h3>
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">Progresso</span>
              <span className="font-medium tabular-nums">
                {formatBRL(actualIncome)} / {formatBRL(revenueTarget)}
              </span>
            </div>
            <ProgressBar pct={revPct} color={revColor} />
            <div className="space-y-2 pt-2">
              <label htmlFor="revenue-target" className="text-xs font-medium text-muted-foreground">
                Valor da meta (R$)
              </label>
              <Input
                id="revenue-target"
                type="number"
                min="0"
                step="0.01"
                value={revenueInput}
                onChange={e => { setRevenueInput(e.target.value); setSaved(false) }}
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold">
              <TrendingDown className="size-4 text-destructive" aria-hidden />
              Limite de Despesas
            </h3>
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">Progresso</span>
              <span className="font-medium tabular-nums">
                {formatBRL(actualExpense)} / {formatBRL(expenseTarget)}
              </span>
            </div>
            <ProgressBar pct={expPct} color={expColor} />
            <div className="space-y-2 pt-2">
              <label htmlFor="expense-target" className="text-xs font-medium text-muted-foreground">
                Valor limite (R$)
              </label>
              <Input
                id="expense-target"
                type="number"
                min="0"
                step="0.01"
                value={expenseInput}
                onChange={e => { setExpenseInput(e.target.value); setSaved(false) }}
              />
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="flex items-center gap-3">
        <Button
          onClick={() => saveGoal.mutate(
            { revenue_target: revenueTarget, expense_target: expenseTarget },
            { onSuccess: () => setSaved(true) },
          )}
          disabled={saveGoal.isPending}
        >
          Salvar Metas
        </Button>
        {saved && (
          <span className="flex items-center gap-1.5 text-sm text-success">
            <CheckCircle2 className="size-4" aria-hidden />
            Metas salvas
          </span>
        )}
      </div>

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
