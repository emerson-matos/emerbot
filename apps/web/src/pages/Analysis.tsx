import type { ReactNode } from 'react'
import { format } from 'date-fns'
import type { LucideIcon } from 'lucide-react'
import {
  AlertTriangle,
  Calendar,
  CheckCircle2,
  CircleDollarSign,
  Clock,
  Banknote,
  Lightbulb,
  PieChart,
  Target,
  TrendingDown,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import EmptyState from '@/components/EmptyState'
import KpiCard, { KpiCardContent, toneVar } from '@/components/KpiCard'
import { useMonthlyAnalysis } from '../hooks/useMonthlyAnalysis'
import { formatBRL, formatMonthLabel } from '@/lib/format'
import type { YearMonth, Analysis, FinancialHealthStatus, MonthTrend, Period, Recommendation } from '@/api/types'
import { FinancialHealthStatus as Status, RecommendationSeverity as RecSeverity } from '@/api/types'

const HEALTH_LABEL: Record<FinancialHealthStatus, string> = {
  [Status.Boa]: 'Boa',
  [Status.Atencao]: 'Atenção',
  [Status.Critico]: 'Crítico',
}

function HealthIcon({ status }: { status: FinancialHealthStatus }) {
  if (status === Status.Boa) return <span className="text-lg">🟢</span>
  if (status === Status.Atencao) return <span className="text-lg">🟡</span>
  return <span className="text-lg">🔴</span>
}

// "12 de jul." from a backend "YYYY-MM-DD". Parsed at noon so a negative UTC
// offset cannot pull the label back to the day before.
function dayMonthLabel(date: string): string {
  return new Date(`${date}T12:00:00`).toLocaleDateString('pt-BR', {
    day: '2-digit',
    month: 'short',
  })
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

/**
 * Section is the one card shell every block on this page uses.
 *
 * Sections used to disappear when they had nothing to show, so the page came
 * back a different shape on every visit and a reader could not tell "there was
 * nothing to say" from "this failed to load". Each one now keeps its place and
 * says which of the two it is.
 */
function Section({
  title,
  icon: Icon,
  iconClassName = 'text-primary',
  empty,
  children,
}: {
  title: string
  icon: LucideIcon
  iconClassName?: string
  empty?: string
  children: ReactNode
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Icon className={`size-4 ${iconClassName}`} aria-hidden />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {empty ? <EmptyState icon={Icon} message={empty} className="py-6" /> : children}
      </CardContent>
    </Card>
  )
}

function RecommendationSection({ data }: { data: Analysis['recommendations'] }) {
  return (
    <Section
      title="Recomendações"
      icon={Lightbulb}
      empty={data.length === 0 ? 'Nada a ajustar por enquanto — o mês está seguindo o previsto.' : undefined}
    >
      <ul className="space-y-2">
        {data.map((rec, i) => (
          <li key={i}>
            <RecommendationItem recommendation={rec} />
          </li>
        ))}
      </ul>
    </Section>
  )
}

const TREND_ARROW = { up: '↑', down: '↓', stable: '—' } as const

// The dead band the backend uses for a week-over-week move (weekPacePct): a
// swing inside it is noise. The two percentages on this page used to colour
// themselves at ±50%, so a fall the health card was calling a warning showed
// up here in a neutral tone.
const TREND_DEAD_BAND_PCT = 5

function trendTone(pct: number): string {
  if (pct < -TREND_DEAD_BAND_PCT) return 'text-warning'
  if (pct > TREND_DEAD_BAND_PCT) return 'text-success'
  return 'text-muted-foreground'
}

function pluralDias(n: number): string {
  return n === 1 ? '1 dia' : `${n} dias`
}

// Keyed by the backend's weekday abbreviations (analytics.weekdayLabels).
const WEEKDAY_FULL_PT: Record<string, string> = {
  Dom: 'domingo',
  Seg: 'segunda',
  Ter: 'terça',
  Qua: 'quarta',
  Qui: 'quinta',
  Sex: 'sexta',
  Sáb: 'sábado',
}

// The backend compares both months over the same finished days, so a percentage
// from a month in progress must not be presented as a whole-month figure.
//
// A previous of zero has no percentage to report: the backend fills in a flat
// 100% rise there (see buildTrend), which these cards were printing as a real
// month-over-month move against a month that never traded.
//
// A month on its first day has no finished day at all — period.throughDay is 0
// while the month is in progress — and the backend zeroes both sides there.
// Saying so is the only honest label: this card used to read "↓ 100% vs mês
// passado até o dia 1" on every 1st, comparing an empty morning with a whole
// trading day.
function trendLabel(trend: MonthTrend, period: Period): string {
  if (period.inProgress && period.throughDay === 0) return 'Mês começando — sem dia fechado'
  if (trend.previous === 0) return 'Sem base no mês passado'
  const window = period.inProgress
    ? `vs mês passado até o dia ${period.throughDay}`
    : 'vs mês passado'
  return `${TREND_ARROW[trend.direction]} ${Math.abs(trend.change)}% ${window}`
}

function KpiSection({ data, goals, trends, period }: {
  data: Analysis['kpis']
  goals: Analysis['goals']
  trends: Analysis['trends']
  period: Period
}) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
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
            {trendLabel(trends.resultado, period)}
          </p>
        </KpiCardContent>
      </KpiCard>

      <KpiCard tone="positive" className="min-h-26">
        <KpiCardContent icon={TrendingUp} tone="positive">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Faturamento</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.positive }}>
            {formatBRL(data.faturamento)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {trendLabel(trends.faturamento, period)}
          </p>
        </KpiCardContent>
      </KpiCard>

      {/*
        Entradas de caixa sits next to faturamento so the two are read together
        and the gap between them is visible. It carries no trend on purpose: it
        is a liquidity figure, and a rise in it says nothing about the business
        doing better — a loan would move it.
      */}
      <KpiCard tone="info" className="min-h-26">
        <KpiCardContent icon={Banknote} tone="info">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Entradas de Caixa</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.info }}>
            {formatBRL(data.entradasCaixa)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">Todo dinheiro recebido</p>
        </KpiCardContent>
      </KpiCard>

      <KpiCard tone="negative" className="min-h-26">
        <KpiCardContent icon={TrendingDown} tone="negative">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Despesa</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums" style={{ color: toneVar.negative }}>
            {formatBRL(data.despesa)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {trendLabel(trends.despesa, period)}
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

function InsightIcon({ severity }: { severity: Analysis['health']['messages'][number]['severity'] }) {
  if (severity === 'info') return <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" aria-hidden />
  const tone = severity === 'critical' ? 'text-destructive' : 'text-warning'
  return <AlertTriangle className={`mt-0.5 size-4 shrink-0 ${tone}`} aria-hidden />
}

// The health card carries its own heading rather than Section's, because the
// traffic light and the score belong in it — but it builds that heading out of
// CardHeader/CardTitle like every other card, so the section titles down the
// page are one typeface and one weight.
function HealthSection({ data }: { data: Analysis['health'] }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <HealthIcon status={data.status} />
          Saúde Financeira
          <span className="font-normal text-muted-foreground">· {HEALTH_LABEL[data.status]}</span>
        </CardTitle>
        <CardAction>
          <div className="text-right">
            <p className="text-3xl font-bold tabular-nums">{data.score}</p>
            <p className="text-xs text-muted-foreground">pontos</p>
          </div>
        </CardAction>
      </CardHeader>
      <CardContent>
        {data.messages.length === 0 ? (
          <EmptyState icon={CheckCircle2} message="Ainda não há movimento suficiente neste mês para avaliar." className="py-6" />
        ) : (
          <ul className="space-y-1.5">
            {data.messages.map((msg, i) => (
              <li key={i} className="flex items-start gap-2 text-sm">
                <InsightIcon severity={msg.severity} />
                <span>
                  {msg.title}
                  {msg.description && <span className="text-muted-foreground"> — {msg.description}</span>}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function WeekdaySection({ data }: { data: Analysis['weekdays'] }) {
  const traded = data.some((day) => day.count > 0)

  return (
    <Section
      title="Média por Dia da Semana"
      icon={Calendar}
      empty={traded ? undefined : 'Nenhuma venda de balcão registrada neste mês.'}
    >
      {/* Weekday order, always. Today used to be sorted to the front, which on
          any day but a Sunday left a strip labelled Qua, Dom, Seg, Ter… — a
          week that reads as scrambled. Today is marked in place. */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-7">
        {[...data].sort((a, b) => a.day - b.day).map((day) => (
          <div
            key={day.day}
            className={`rounded-lg p-2 text-center ${day.isToday ? 'bg-primary/10 ring-1 ring-primary' : 'bg-muted/50'}`}
          >
            <p className="text-[10px] font-medium text-muted-foreground">{day.label}</p>
            <p className="mt-1 text-sm font-semibold tabular-nums">
              {day.count > 0 ? formatBRL(day.avg) : '—'}
            </p>
            {/* Days with a sale are counted; a bare "—" already says there
                were none, and "0x" underneath it read like a third figure. */}
            {day.count > 0 && (
              <p className="text-[10px] text-muted-foreground">
                {day.count} {day.count === 1 ? 'dia' : 'dias'}
              </p>
            )}
          </div>
        ))}
      </div>
    </Section>
  )
}

// ProjectionSection renders the backend's projection verbatim. It used to
// recompute the whole thing here — the projection, the gap and its own
// "necessário por dia útil" — off the weekday averages and the browser clock,
// which put a second, smaller daily target on the same screen as the one the
// health insight and the recommendation were quoting, and made the card
// disagree with what the WhatsApp bot reported for the same month.
function ProjectionSection({ projection, faturamento, period }: {
  projection: Analysis['projection']
  faturamento: MonthTrend
  period: Period
}) {
  // Both sides come from the faturamento trend, which the backend measures over
  // the same finished days of both months. This used to hold projection.actual —
  // the whole month including a today that had barely been traded — against last
  // month up to the same date in full, so the card reported a fall every morning
  // and a 100% one on the 1st.
  const prevDiff = faturamento.previous > 0
    ? Math.round(((faturamento.current - faturamento.previous) / faturamento.previous) * 100)
    : null

  return (
    <Section
      title="Projeção do Mês"
      icon={Target}
      empty={projection.target <= 0
        ? 'Defina uma meta de faturamento em Metas para acompanhar a projeção do mês.'
        : undefined}
    >
      <>
        <div className="grid grid-cols-[1fr_auto] gap-x-4">
          <div className="min-w-0">
            <p className="text-sm text-muted-foreground">Projeção</p>
            <p className="truncate text-lg tabular-nums">
              {formatBRL(projection.projected)}
            </p>
          </div>

          <div className="text-right">
            <p className="text-sm text-muted-foreground">Meta</p>
            <p className="text-lg whitespace-nowrap tabular-nums">
              {formatBRL(projection.target)}
            </p>
          </div>
        </div>
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Faturado até agora</span>
            <span className="text-sm tabular-nums">{formatBRL(projection.actual)}</span>
          </div>
          {period.inProgress && period.throughDay === 0 ? (
            <p className="text-sm text-muted-foreground">
              O mês está começando — ainda não há dia fechado para comparar.
            </p>
          ) : (
            <>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">
                  Este mês {period.inProgress ? `(até o dia ${period.throughDay})` : '(fechado)'}
                </span>
                <span className="text-sm tabular-nums">{formatBRL(faturamento.current)}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">
                  Mês passado {period.inProgress ? `(até o dia ${period.throughDay})` : '(fechado)'}
                </span>
                <span className="text-sm tabular-nums">{formatBRL(faturamento.previous)}</span>
              </div>
              {/* No previous month is not a 0% move: without a baseline there is
                  no percentage to report, and printing one invented a comparison
                  against a month that never traded. */}
              {prevDiff === null ? (
                <p className="text-sm text-muted-foreground">Sem faturamento no mês passado para comparar</p>
              ) : (
                <p className={`text-sm ${trendTone(prevDiff)}`}>
                  {prevDiff > 0 ? '↑' : '↓'} {Math.abs(prevDiff)}% vs mês passado
                </p>
              )}
            </>
          )}
        </div>
        {projection.onTrack ? (
          <div className="border-t pt-3">
            <p className="flex items-center gap-1.5 text-sm text-success">
              <CheckCircle2 className="size-4" aria-hidden />
              Se mantiver a média, fecha no azul
            </p>
          </div>
        ) : (
          <div className="space-y-2 border-t pt-3">
            <div className="flex items-center justify-between">
              <p className="text-sm text-muted-foreground">Faltam para a meta</p>
              <p className="text-base text-destructive tabular-nums">{formatBRL(projection.gap)}</p>
            </div>
            {projection.neededPerDay > 0 ? (
              <div className="flex items-center justify-between">
                {/* Every remaining day, not just business days: the shop trades
                    on weekends too, and the label used to say "dia útil" over a
                    figure divided by all of them. */}
                <p className="text-sm text-muted-foreground">
                  Necessário por dia ({pluralDias(projection.daysRemaining)}, hoje incluído)
                </p>
                <p className="text-base tabular-nums">{formatBRL(projection.neededPerDay)}</p>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">Mês encerrado — não há mais dias para recuperar.</p>
            )}
          </div>
        )}
      </>
    </Section>
  )
}

function WeekComparisonSection({ data, recommendation }: { data: Analysis['weekComparison']; recommendation?: Recommendation }) {
  // The pace pair, not the running totals: both sides cover the same finished
  // days of the week, so today — still being traded — is in neither. Comparing
  // this week including today against last week's matching weekday in full read
  // as a fall every morning, and as a 100% fall on a Monday.
  const pct = data.pace.days > 0 && data.pace.previous !== 0
    ? Math.round(((data.pace.current - data.pace.previous) / data.pace.previous) * 100)
    : null

  // The backend emits one label per elapsed day of this week, so the last one
  // is the day it actually measured through. Reading the browser clock instead
  // captions the card with a different day than the figures cover whenever the
  // viewer's timezone has already rolled over.
  const todayPt = WEEKDAY_FULL_PT[data.labels.at(-1) ?? ''] ?? 'hoje'

  return (
    <Section title="Comportamento Semanal" icon={Clock}>
      <>
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Esta semana (até {todayPt})</span>
            <span className="text-sm tabular-nums">{formatBRL(data.current)}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Ritmo até ontem ({pluralDias(data.pace.days)})</span>
            <span className="text-sm tabular-nums">{formatBRL(data.pace.current)}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Semana passada (mesmos dias)</span>
            <span className="text-sm tabular-nums">{formatBRL(data.pace.previous)}</span>
          </div>
          {data.pace.days === 0 ? (
            <p className="text-sm text-muted-foreground">A semana está começando — nenhum dia fechado ainda</p>
          ) : pct === null ? (
            <p className="text-sm text-muted-foreground">Sem vendas na semana passada para comparar</p>
          ) : (
            <p className={`text-sm ${trendTone(pct)}`}>
              {pct > 0 ? '↑' : '↓'} {Math.abs(pct)}% vs semana anterior
            </p>
          )}
        </div>
        {recommendation && (
          <div className="border-t pt-2">
            <RecommendationItem recommendation={recommendation} />
          </div>
        )}
      </>
    </Section>
  )
}

function CashOutSection({ data }: { data: Analysis['cashOutDays'] }) {
  return (
    <Section
      title="Dias com Maior Saída de Caixa"
      icon={CircleDollarSign}
      iconClassName="text-destructive"
      empty={data.length === 0 ? 'Nenhuma despesa registrada neste mês.' : undefined}
    >
      <div className="space-y-4">
        {data.map((day) => (
          <div key={day.date} className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">{dayMonthLabel(day.date)}</span>
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
        ))}
      </div>
    </Section>
  )
}

const COMPOSITION_PALETTE = ['var(--chart-4)', 'var(--chart-3)', 'var(--chart-2)', 'var(--chart-1)', 'var(--chart-5)']

function CompositionSection({ data }: { data: Analysis['expenseComposition'] }) {
  return (
    <Section
      title="Composição de Despesas"
      icon={PieChart}
      empty={data.length === 0 ? 'Nenhuma despesa registrada neste mês.' : undefined}
    >
      <>
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
                  style={{
                    width: `${item.percentage}%`,
                    background: COMPOSITION_PALETTE[i % COMPOSITION_PALETTE.length],
                  }}
                />
              </div>
              <span className="text-xs text-muted-foreground tabular-nums">{item.percentage}%</span>
            </div>
          </div>
        ))}
      </>
    </Section>
  )
}

function CashPositionSection({ data }: { data: Analysis['cashPosition'] }) {
  return (
    <Section title="Posição de Caixa" icon={Wallet}>
      <>
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
              Saldo fica negativo em {pluralDias(data.daysUntilNegative)}
            </p>
          </div>
        )}
      </>
    </Section>
  )
}

// SECTION_COUNT is how many cards the page renders below the KPI row. The
// skeleton draws that many so the layout does not jump when the data lands.
const SECTION_COUNT = 8

function LoadingSkeleton() {
  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-26" />
        ))}
      </div>
      {Array.from({ length: SECTION_COUNT }, (_, i) => (
        <Skeleton key={i} className="h-40" />
      ))}
    </>
  )
}

function ErrorCard({ onRetry }: { onRetry: () => void }) {
  return (
    <Card>
      <CardContent className="flex flex-col items-start gap-3 py-8">
        <div className="flex items-center gap-2">
          <AlertTriangle className="size-5 text-destructive" aria-hidden />
          <p className="font-medium">Não foi possível carregar a análise</p>
        </div>
        <p className="text-sm text-muted-foreground">
          Tente novamente em alguns instantes.
        </p>
        <Button variant="outline" size="sm" onClick={onRetry}>
          Tentar de novo
        </Button>
      </CardContent>
    </Card>
  )
}

function AnalysisBody({ analysis }: { analysis: Analysis }) {
  // recommendations[0] is the weekly one, which captions the week-comparison
  // card (see the backend's buildRecommendations). The list below carries the
  // rest — printing it in both places put the same sentence on screen twice.
  const [weeklyRec, ...otherRecs] = analysis.recommendations

  return (
    <>
      {/* The headline numbers first: the cards below break them down, and the
          summary of a page does not belong underneath its own detail. */}
      <KpiSection
        data={analysis.kpis}
        goals={analysis.goals}
        trends={analysis.trends}
        period={analysis.period}
      />
      <HealthSection data={analysis.health} />
      <RecommendationSection data={otherRecs} />
      <WeekdaySection data={analysis.weekdays} />
      <ProjectionSection
        projection={analysis.projection}
        faturamento={analysis.trends.faturamento}
        period={analysis.period}
      />
      <WeekComparisonSection data={analysis.weekComparison} recommendation={weeklyRec} />
      <CashPositionSection data={analysis.cashPosition} />
      <CashOutSection data={analysis.cashOutDays} />
      <CompositionSection data={analysis.expenseComposition} />
    </>
  )
}

export default function Analysis() {
  const now = new Date()
  const month = format(now, 'yyyy-MM') as YearMonth
  const { data: analysis, isError, refetch } = useMonthlyAnalysis(month)

  return (
    <div className="space-y-6">
      {/* The heading stays put through every state, so loading, a failure and
          a loaded month are the same page rather than three of them. */}
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Análise</h1>
        <p className="mt-1 text-muted-foreground first-letter:uppercase">{formatMonthLabel(month)}</p>
      </div>

      {/* A failed load has to say so: without this branch the skeleton never
          resolves and the page looks like it is loading forever. */}
      {isError ? (
        <ErrorCard onRetry={() => void refetch()} />
      ) : analysis ? (
        <AnalysisBody analysis={analysis} />
      ) : (
        <LoadingSkeleton />
      )}
    </div>
  )
}
