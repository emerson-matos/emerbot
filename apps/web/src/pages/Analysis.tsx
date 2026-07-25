import { format } from 'date-fns'
import {
  AlertTriangle,
  Calendar,
  CheckCircle2,
  CircleDollarSign,
  Clock,
  Lightbulb,
  PieChart,
  Target,
  TrendingDown,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import KpiCard, { KpiCardContent, toneVar } from '@/components/KpiCard'
import { useMonthlyAnalysis } from '../hooks/useMonthlyAnalysis'
import { formatBRL } from '@/lib/format'
import type { YearMonth, Analysis, FinancialHealthStatus, Recommendation } from '@/lib/analytics/types'
import { FinancialHealthStatus as Status, RecommendationSeverity as RecSeverity } from '@/lib/analytics/types'

function capitalizeFirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

function HealthIcon({ status }: { status: FinancialHealthStatus }) {
  if (status === Status.Boa) return <span className="text-lg">🟢</span>
  if (status === Status.Atencao) return <span className="text-lg">🟡</span>
  return <span className="text-lg">🔴</span>
}

function formatMonthLabel(month: string): string {
  const [y, m] = month.split('-').map(Number)
  const date = new Date(y, m - 1, 1)
  return capitalizeFirst(
    date.toLocaleDateString('pt-BR', { month: 'long', year: 'numeric' }),
  )
}

function RecommendationItem({ recommendation }: { recommendation: Recommendation }) {
  const colors = {
    [RecSeverity.Success]: 'text-success',
    [RecSeverity.Warning]: 'text-warning',
    [RecSeverity.Danger]: 'text-destructive',
  }
  const color = colors[recommendation.severity]

  return (
    <div className="flex items-start gap-2 text-sm">
      <Lightbulb className={`mt-0.5 size-4 shrink-0 ${color}`} aria-hidden />
      <div>
        <span className="font-medium">{recommendation.title}</span>
        <span className="text-muted-foreground"> — {recommendation.message}</span>
      </div>
    </div>
  )
}

function RecommendationSection({ data }: { data: Analysis['recommendations'] }) {
  if (data.length === 0) return null

  return (
    <Card className="col-span-full">
      <CardContent className="space-y-3">
        <div className="flex items-center gap-2">
          <Lightbulb className="size-4 text-primary" aria-hidden />
          <p className="text-sm font-semibold">Recomendações</p>
        </div>
        <ul className="space-y-2">
          {data.map((rec, i) => (
            <li key={i}>
              <RecommendationItem recommendation={rec} />
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

function KpiSection({ data, goals, trends }: { data: Analysis['kpis']; goals: Analysis['goals']; trends: Analysis['trends'] }) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <KpiCard
        tone={data.resultado >= 0 ? 'positive' : 'negative'}
        className="min-h-26"
      >
        <KpiCardContent icon={Wallet} tone={data.resultado >= 0 ? 'positive' : 'negative'}>
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Resultado</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: data.resultado >= 0 ? toneVar.positive : toneVar.negative }}>
            {formatBRL(data.resultado)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {trends.resultado.direction === 'down' ? '↓' : trends.resultado.direction === 'up' ? '↑' : '—'} {Math.abs(trends.resultado.change)}% vs mês passado
          </p>
        </KpiCardContent>
      </KpiCard>

      <KpiCard tone="positive" className="min-h-26">
        <KpiCardContent icon={TrendingUp} tone="positive">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Receita</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.positive }}>
            {formatBRL(data.receita)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {trends.receita.direction === 'down' ? '↓' : trends.receita.direction === 'up' ? '↑' : '—'} {Math.abs(trends.receita.change)}% vs mês passado
          </p>
        </KpiCardContent>
      </KpiCard>

      <KpiCard tone="negative" className="min-h-26">
        <KpiCardContent icon={TrendingDown} tone="negative">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Despesa</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.negative }}>
            {formatBRL(data.despesa)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {trends.despesa.direction === 'down' ? '↓' : trends.despesa.direction === 'up' ? '↑' : '—'} {Math.abs(trends.despesa.change)}% vs mês passado
          </p>
        </KpiCardContent>
      </KpiCard>

      <KpiCard tone="info" className="min-h-26">
        <KpiCardContent icon={Target} tone="info">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Meta</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
            {goals.revenuePct}%
          </p>
          <p className="mt-1 text-xs text-muted-foreground">Faturamento</p>
        </KpiCardContent>
      </KpiCard>
    </div>
  )
}

function HealthSection({ data }: { data: Analysis['health'] }) {
  const infoCount = data.messages.filter(m => m.severity === 'info').length
  const total = data.messages.length
  const score = total > 0 ? Math.round((infoCount / total) * 100) : 50

  return (
    <Card className="col-span-full">
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <HealthIcon status={data.status} />
            <div>
              <p className="text-sm font-semibold">Saúde Financeira</p>
              <p className="text-xs text-muted-foreground capitalize">{data.status === Status.Boa ? 'Boa' : data.status === Status.Atencao ? 'Atenção' : 'Crítico'}</p>
            </div>
          </div>
          <div className="text-right">
            <p className="text-3xl font-bold tabular-nums">{score}</p>
            <p className="text-xs text-muted-foreground">pontos</p>
          </div>
        </div>
        <ul className="space-y-1.5">
          {data.messages.map((msg, i) => (
            <li key={i} className="flex items-start gap-2 text-sm">
              {msg.severity === 'info' && (
                <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" aria-hidden />
              )}
              {msg.severity === 'warning' && (
                <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden />
              )}
              {msg.severity === 'critical' && (
                <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden />
              )}
              <span>
                {msg.title}
                {msg.description && <span className="text-muted-foreground"> — {msg.description}</span>}
              </span>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

function WeekdaySection({ data }: { data: Analysis['weekdays'] }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Calendar className="size-4 text-primary" aria-hidden />
          Média por Dia da Semana
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-7">
          {[...data].sort((a, b) => {
            if (a.isToday) return -1
            if (b.isToday) return 1
            return a.day - b.day
          }).map((day) => (
            <div
              key={day.day}
              className={`rounded-lg p-2 text-center ${day.isToday ? 'col-span-2 sm:col-span-1 bg-primary/10 ring-1 ring-primary' : 'bg-muted/50'}`}
            >
              <p className="text-[10px] font-medium text-muted-foreground">{day.label}</p>
              <p className="mt-1 text-sm font-semibold tabular-nums">
                {day.avg > 0 ? formatBRL(day.avg) : '—'}
              </p>
              {day.count > 0 && (
                <p className="text-[10px] text-muted-foreground">{day.count}x</p>
              )}
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function ProjectionSection({ weekdays, kpis, goals }: { weekdays: Analysis['weekdays']; kpis: Analysis['kpis']; goals: Analysis['goals'] }) {
  if (!weekdays) return null

  const today = new Date()
  const year = today.getFullYear()
  const month = today.getMonth()
  const lastDay = new Date(year, month + 1, 0).getDate()
  const currentDay = today.getDate()

  if (currentDay >= lastDay) return null

  const weekdayMap = new Map(weekdays.map(w => [w.day, w.avg]))

  let remainingDays = 0
  let projected = 0
  for (let d = currentDay + 1; d <= lastDay; d++) {
    const date = new Date(year, month, d, 12)
    const dayOfWeek = date.getDay()
    const avg = weekdayMap.get(dayOfWeek) ?? 0
    if (avg > 0) remainingDays++
    projected += avg
  }

  const total = goals.revenueActual + projected
  const gap = goals.revenueTarget - total
  const hitGoal = total >= goals.revenueTarget
  const necessaryPerDay = remainingDays > 0 ? gap / remainingDays : 0

  const prevDiff = kpis.previousMonthIncomeUpToDay > 0
    ? Math.round(((goals.revenueActual - kpis.previousMonthIncomeUpToDay) / kpis.previousMonthIncomeUpToDay) * 100)
    : null

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Target className="size-4 text-primary" aria-hidden />
          Projeção do Mês
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-[1fr_auto] gap-x-4">
          <div className="min-w-0">
            <p className="text-sm text-muted-foreground">Projeção</p>
            <p className="truncate text-lg tabular-nums">
              {formatBRL(total)}
            </p>
          </div>

          <div className="text-right">
            <p className="text-sm text-muted-foreground">Meta</p>
            <p className="text-lg whitespace-nowrap tabular-nums">
              {formatBRL(goals.revenueTarget)}
            </p>
          </div>
        </div>
        {prevDiff !== null && (
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Até hoje</span>
              <span className="text-sm tabular-nums">{formatBRL(goals.revenueActual)}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">Mês passado (dia {currentDay})</span>
              <span className="text-sm tabular-nums">{formatBRL(kpis.previousMonthIncomeUpToDay)}</span>
            </div>
            <p className={`text-sm ${prevDiff < -50 ? 'text-destructive' : prevDiff > 50 ? 'text-success' : 'text-yellow-600'}`}>
              {prevDiff > 0 ? '↑' : '↓'} {Math.abs(prevDiff)}% vs mês passado
            </p>
          </div>
        )}
        {!hitGoal && (
          <div className="space-y-2 border-t pt-3">
            <div className="flex items-center justify-between">
              <p className="text-sm text-muted-foreground">Faltam</p>
              <p className="text-base text-destructive tabular-nums">{formatBRL(gap)}</p>
            </div>
            {necessaryPerDay > 0 && (
              <div className="flex items-center justify-between">
                <p className="text-sm text-muted-foreground">Necessário por dia útil</p>
                <p className="text-base tabular-nums">{formatBRL(necessaryPerDay)}</p>
              </div>
            )}
          </div>
        )}
        {hitGoal && (
          <div className="border-t pt-3">
            <p className="flex items-center gap-1.5 text-sm text-success">
              <CheckCircle2 className="size-4" aria-hidden />
              Se mantiver a média, fecha no azul
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function WeekComparisonSection({ data, recommendation }: { data: Analysis['weekComparison']; recommendation?: Recommendation }) {
  const pct = data.previousUpToDay !== 0
    ? Math.round(((data.current - data.previousUpToDay) / data.previousUpToDay) * 100)
    : 0

  const WEEKDAY_LABELS_PT = ['domingo', 'segunda', 'terça', 'quarta', 'quinta', 'sexta', 'sábado']
  const todayPt = WEEKDAY_LABELS_PT[new Date().getDay()]

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Clock className="size-4 text-primary" aria-hidden />
          Comportamento Semanal
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Esta semana (até {todayPt})</span>
            <span className="text-sm tabular-nums">{formatBRL(data.current)}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Semana passada (até {todayPt})</span>
            <span className="text-sm tabular-nums">{formatBRL(data.previousUpToDay)}</span>
          </div>
          <p className={`text-sm ${pct < -50 ? 'text-destructive' : pct > 50 ? 'text-success' : 'text-yellow-600'}`}>
            {pct > 0 ? '↑' : '↓'} {Math.abs(pct)}% vs semana anterior
          </p>
        </div>
        {recommendation && (
          <div className="border-t pt-2">
            <RecommendationItem recommendation={recommendation} />
          </div>
        )}
        {data.avg8Weeks !== undefined && (
          <div className="border-t pt-2">
            <p className="text-sm text-muted-foreground">
              Média 8 semanas: <span className="text-foreground">{formatBRL(data.avg8Weeks)}</span>
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function CashOutSection({ data }: { data: Analysis['cashOutDays'] }) {
  if (data.length === 0) return null

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <CircleDollarSign className="size-4 text-destructive" aria-hidden />
          Dias com Maior Saída de Caixa
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {data.map((day) => {
          const date = new Date(day.date + 'T12:00:00')
          const label = date.toLocaleDateString('pt-BR', { day: '2-digit', month: 'short' })
          return (
            <div key={day.date} className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">{label}</span>
                <span className="text-sm font-semibold tabular-nums">{formatBRL(day.total)}</span>
              </div>
              <ul className="space-y-1">
                {day.items.map((item, i) => (
                  <li key={i} className="flex items-center justify-between text-xs text-muted-foreground">
                    <span>{item.category.replace(/_/g, ' ')}</span>
                    <span className="tabular-nums">{formatBRL(item.amount)}</span>
                  </li>
                ))}
              </ul>
            </div>
          )
        })}
      </CardContent>
    </Card>
  )
}

function CompositionSection({ data }: { data: Analysis['expenseComposition'] }) {
  if (data.length === 0) return null

  const palette = ['var(--chart-4)', 'var(--chart-3)', 'var(--chart-2)', 'var(--chart-1)', 'var(--chart-5)']

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <PieChart className="size-4 text-primary" aria-hidden />
          Composição de Despesas
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {data.map((item, i) => (
          <div key={item.categoryId}>
            <div className="mb-1 flex items-baseline justify-between">
              <span className="text-sm">{item.categoryName}</span>
              <span className="text-sm font-semibold tabular-nums">{formatBRL(item.amount)}</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full transition-[width] duration-500"
                  style={{ width: `${item.percentage}%`, background: palette[i % palette.length] }}
                />
              </div>
              <span className="text-xs text-muted-foreground tabular-nums">{item.percentage}%</span>
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function CashPositionSection({ data }: { data: Analysis['cashPosition'] }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Wallet className="size-4 text-primary" aria-hidden />
          Posição de Caixa
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-sm text-muted-foreground">Saldo hoje</span>
          <span className="text-sm tabular-nums">{formatBRL(data.currentBalance)}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-sm text-muted-foreground">Projeção fim do mês</span>
          <span className={`text-sm tabular-nums ${data.endOfMonthProjection >= 0 ? 'text-success' : 'text-destructive'}`}>
            {formatBRL(data.endOfMonthProjection)}
          </span>
        </div>
        {data.daysUntilNegative !== null && (
          <div className="border-t pt-2">
            <p className="text-sm text-destructive">
              Saldo fica negativo em {data.daysUntilNegative} dia{data.daysUntilNegative > 1 ? 's' : ''}
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <div>
        <Skeleton className="h-8 w-48" />
        <Skeleton className="mt-2 h-4 w-32" />
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[1, 2, 3, 4].map((i) => (
          <Skeleton key={i} className="h-26" />
        ))}
      </div>
      {[1, 2, 3, 4, 5, 6].map((i) => (
        <Skeleton key={i} className="h-40" />
      ))}
    </div>
  )
}

export default function Analysis() {
  const now = new Date()
  const month = format(now, 'yyyy-MM') as YearMonth
  const analysis = useMonthlyAnalysis(month)

  if (!analysis) return <LoadingSkeleton />

  const weeklyRec = analysis.recommendations[0]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Análise</h1>
        <p className="mt-1 text-muted-foreground">{formatMonthLabel(month)}</p>
      </div>

      <HealthSection data={analysis.health} />
      <RecommendationSection data={analysis.recommendations} />
      <WeekdaySection data={analysis.weekdays} />
      <ProjectionSection weekdays={analysis.weekdays} kpis={analysis.kpis} goals={analysis.goals} />
      <WeekComparisonSection data={analysis.weekComparison} recommendation={weeklyRec} />
      <CashPositionSection data={analysis.cashPosition} />
      <CashOutSection data={analysis.cashOutDays} />
      <CompositionSection data={analysis.expenseComposition} />
      <KpiSection data={analysis.kpis} goals={analysis.goals} trends={analysis.trends} />
    </div>
  )
}
