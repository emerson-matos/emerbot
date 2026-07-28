import { useEffect, useState } from 'react'
import { format } from 'date-fns'
import { ptBR } from 'date-fns/locale'
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
import KpiCard, { KpiCardContent, toneVar } from '@/components/KpiCard'
import { useGoal, useSaveGoalMutation } from '../api/queries'
import { useMonthlyAnalysis } from '../hooks/useMonthlyAnalysis'
import type { YearMonth } from '@/api/types'

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
  const currentMonth = format(now, 'yyyy-MM') as YearMonth

  const goalQuery = useGoal(currentMonth)
  const goal = goalQuery.data?.goal ?? null
  const saveGoal = useSaveGoalMutation(currentMonth)

  // The backend assembles goal progress and the trailing 3-month history in
  // one pass (packages/finance/analytics) — Faturamento here reads the same
  // figure as the Analysis page and the WhatsApp bot, instead of this page
  // re-deriving its own narrower one from raw entries.
  const analysisQuery = useMonthlyAnalysis(currentMonth)
  const analysis = analysisQuery.data ?? null
  const history = analysis?.history ?? []

  const progressLoading = analysisQuery.isLoading || goalQuery.isLoading
  const progressError = analysisQuery.isError || goalQuery.isError
  const trendLoading = analysisQuery.isLoading
  const trendError = analysisQuery.isError

  const [incomeInput, setIncomeInput] = useState('')
  const [expenseInput, setExpenseInput] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (goal) {
      setIncomeInput(String(goal.IncomeTarget / 100))
      setExpenseInput(String(goal.ExpenseTarget / 100))
    }
  }, [goal])


  const actualIncome = analysis?.goals.incomeActual ?? 0
  const actualExpense = analysis?.goals.expenseActual ?? 0

  const incomeTarget = Math.round(Number(incomeInput) * 100)
  const expenseTarget = Math.round(Number(expenseInput) * 100)
  const incomePct = incomeTarget > 0 ? Math.min(100, (actualIncome / incomeTarget) * 100) : 0
  const expPct = expenseTarget > 0 ? Math.min(100, (actualExpense / expenseTarget) * 100) : 0
  const incomeColor = incomePct >= 100 ? 'var(--success)' : 'var(--info)'
  const expColor = expPct > 100 ? 'var(--destructive)' : expPct >= 80 ? 'var(--warning)' : 'var(--info)'

  const monthsHit = history.filter(h => h.incomeTarget !== null && h.income >= h.incomeTarget).length
  const avgIncome = history.length
    ? Math.round(history.reduce((s, h) => s + h.income, 0) / history.length)
    : 0

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Metas</h1>
        <p className="mt-1 text-muted-foreground">Defina as metas financeiras do mês</p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          tone="positive"
          isLoading={progressLoading}
          isError={progressError}
          errorMessage="Erro ao carregar progresso"
          className="min-h-26"
        >
          <KpiCardContent icon={TrendingUp} tone="positive">
            <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Progresso Faturamento</p>
            <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.positive }}>
              {incomePct.toFixed(0)}%
            </p>
            <p className="mt-1 text-xs text-muted-foreground">da meta deste mês</p>
          </KpiCardContent>
        </KpiCard>

        <KpiCard
          tone="negative"
          isLoading={progressLoading}
          isError={progressError}
          errorMessage="Erro ao carregar progresso"
          className="min-h-26"
        >
          <KpiCardContent icon={TrendingDown} tone="negative">
            <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Progresso Despesas</p>
            <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.negative }}>
              {expPct.toFixed(0)}%
            </p>
            <p className="mt-1 text-xs text-muted-foreground">do limite deste mês</p>
          </KpiCardContent>
        </KpiCard>

        <KpiCard
          tone="info"
          isLoading={trendLoading}
          isError={trendError}
          errorMessage="Erro ao carregar histórico"
          className="min-h-26"
        >
          <KpiCardContent icon={Target} tone="info">
            <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Meses na Meta</p>
            <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
              {monthsHit}/{history.length}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">faturamento atingido</p>
          </KpiCardContent>
        </KpiCard>

        <KpiCard
          tone="primary"
          isLoading={trendLoading}
          isError={trendError}
          errorMessage="Erro ao carregar histórico"
          className="min-h-26"
        >
          <KpiCardContent icon={BarChart3} tone="primary">
            <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Faturamento Médio</p>
            <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.primary }}>
              {formatBRL(avgIncome)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">últimos meses</p>
          </KpiCardContent>
        </KpiCard>
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
                {formatBRL(actualIncome)} / {formatBRL(incomeTarget)}
              </span>
            </div>
            <ProgressBar pct={incomePct} color={incomeColor} />
            <div className="space-y-2 pt-2">
              <label htmlFor="income-target" className="text-xs font-medium text-muted-foreground">
                Valor da meta (R$)
              </label>
              <Input
                id="income-target"
                type="number"
                min="0"
                step="0.01"
                value={incomeInput}
                onChange={e => { setIncomeInput(e.target.value); setSaved(false) }}
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
            { income_target: incomeTarget, expense_target: expenseTarget },
            { onSuccess: () => setSaved(true) },
          )}
          className="grow"
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
              {history.map(h => (
                <TableRow key={h.month}>
                  <TableCell>
                    {capitalizeFirst(format(new Date(h.month + '-01'), "MMMM 'de' yyyy", { locale: ptBR }))}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {formatBRL(h.income)} / {h.incomeTarget !== null ? formatBRL(h.incomeTarget) : '—'}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {formatBRL(h.expense)} / {h.expenseTarget !== null ? formatBRL(h.expenseTarget) : '—'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
