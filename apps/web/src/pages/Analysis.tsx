import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import {
  AlertTriangle,
  Calendar,
  CheckCircle2,
  Circle,
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
import WeekRhythm from '@/components/WeekRhythm'
import { useMonthlyAnalysis } from '../hooks/useMonthlyAnalysis'
import { formatBRL, formatMonthLabel } from '@/lib/format'
import { WEEKDAY_FULL_PT } from '@/lib/weekdays'
import { currentMonthKey } from '@/lib/entries'
import type { YearMonth, Analysis, FinancialHealthStatus, MonthTrend, Period, ProjectionBasis, ProjectionStatus, Recommendation } from '@/api/types'
import { FinancialHealthStatus as Status, RecommendationSeverity as RecSeverity } from '@/api/types'

const HEALTH_LABEL: Record<FinancialHealthStatus, string> = {
  [Status.Boa]: 'Boa',
  [Status.Atencao]: 'Atenção',
  [Status.Critico]: 'Crítico',
}

// The traffic light was three emoji, the one place the page left the lucide set
// — and emoji are drawn by the operating system, so the same status came out a
// different size and a different green on every machine. The written status
// sits beside it, so colour is never the only carrier.
const HEALTH_TONE: Record<FinancialHealthStatus, string> = {
  [Status.Boa]: 'var(--success)',
  [Status.Atencao]: 'var(--warning)',
  [Status.Critico]: 'var(--destructive)',
}

function HealthDot({ status }: { status: FinancialHealthStatus }) {
  const tone = HEALTH_TONE[status]
  return <Circle className="size-3.5 shrink-0" style={{ color: tone, fill: tone }} aria-hidden />
}

// "12 de jul." from a backend "YYYY-MM-DD". Parsed at noon so a negative UTC
// offset cannot pull the label back to the day before.
function dayMonthLabel(date: string): string {
  return new Date(`${date}T12:00:00`).toLocaleDateString('pt-BR', {
    day: '2-digit',
    month: 'short',
  })
}

/**
 * Row is the label-and-figure line the ledger cards are built from.
 *
 * Three cards were each writing the same flex/justify-between/tabular-nums by
 * hand, which is how the projection, the week and the cash position drifted
 * into three slightly different weights for the same kind of line.
 */
function Row({ label, value, valueClassName = '' }: {
  label: ReactNode
  value: ReactNode
  valueClassName?: string
}) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className={`text-sm font-medium tabular-nums ${valueClassName}`}>{value}</span>
    </div>
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

/**
 * Section is the one card shell every block on this page uses.
 *
 * Sections used to disappear when they had nothing to show, so the page came
 * back a different shape on every visit and a reader could not tell "there was
 * nothing to say" from "this failed to load". Each one now keeps its place and
 * says which of the two it is.
 *
 * The title is an `h2`: the page had one heading and then nine card titles that
 * were `div`s, so anyone navigating by headings got a page with no structure
 * under "Análise".
 */
function Section({
  title,
  icon: Icon,
  iconClassName = 'text-primary',
  action,
  empty,
  children,
}: {
  title: string
  icon: LucideIcon
  iconClassName?: string
  action?: ReactNode
  empty?: string
  children: ReactNode
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Icon className={`size-4 shrink-0 ${iconClassName}`} aria-hidden />
          <h2>{title}</h2>
        </CardTitle>
        {action && <CardAction>{action}</CardAction>}
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
      title="Insights do mês"
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

// Three levels, matching the severity the recommendation carries. Colouring by
// the boolean onTrack instead put "97% da meta" in red under a green
// "Ritmo suficiente" on the same screen.
const PROJECTION_TONE: Record<ProjectionStatus, string> = {
  '': 'text-muted-foreground',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-destructive',
}

function pluralDias(n: number): string {
  return n === 1 ? '1 dia' : `${n} dias`
}

function pluralFechados(n: number): string {
  return n === 1 ? '1 dia fechado' : `${n} dias fechados`
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
//
// The rest of the opening week is the same failure one size smaller, and the
// backend zeroes it for the same reason: days 1–2 of August are a Saturday and
// a Sunday, days 1–2 of July a Wednesday and a Thursday. The card read "↓ 22%
// vs mês passado até o dia 2" for a pharmacy trading exactly as it had the
// month before. period.comparableThroughDay is 0 through the 7th.
function trendLabel(trend: MonthTrend, period: Period): string {
  if (period.inProgress && period.throughDay === 0) return 'Mês começando — sem dia fechado'
  if (period.inProgress && period.comparableThroughDay === 0) return 'Primeira semana — sem base para comparar'
  if (trend.previous === 0) return 'Sem base no mês passado'
  const window = period.inProgress
    ? `vs mês passado até o dia ${period.comparableThroughDay}`
    : 'vs mês passado'
  return `${TREND_ARROW[trend.direction]} ${Math.abs(trend.change)}% ${window}`
}

// Four cards, not five: "Meta 77%" used to sit here as a bare percentage in the
// same blue as entradas de caixa, two unrelated figures wearing one colour. The
// goal is the subject of the card at the top of the page now, where it has the
// amounts around it that make the percentage mean something.
function KpiSection({ data, trends, period }: {
  data: Analysis['kpis']
  trends: Analysis['trends']
  period: Period
}) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <KpiCard
        tone={data.resultado >= 0 ? 'positive' : 'negative'}
        className="min-h-26"
      >
        <KpiCardContent icon={Wallet} tone={data.resultado >= 0 ? 'positive' : 'negative'}>
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Resultado</p>
          <p className="mt-1 text-2xl font-semibold tracking-tight tabular-nums" style={{ color: data.resultado >= 0 ? toneVar.positive : toneVar.negative }}>
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
          <p className="mt-1 text-2xl font-semibold tracking-tight tabular-nums" style={{ color: toneVar.positive }}>
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
          <p className="mt-1 text-2xl font-semibold tracking-tight tabular-nums" style={{ color: toneVar.info }}>
            {formatBRL(data.entradasCaixa)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">Todo dinheiro recebido</p>
        </KpiCardContent>
      </KpiCard>

      <KpiCard tone="negative" className="min-h-26">
        <KpiCardContent icon={TrendingDown} tone="negative">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Despesa</p>
          <p className="mt-1 text-2xl font-semibold tracking-tight tabular-nums" style={{ color: toneVar.negative }}>
            {formatBRL(data.despesa)}
          </p>
          {/* What is booked for the rest of the month, beside the figure and
              never inside it. This card used to show the two added together —
              the whole month's bills are booked on the 1st, so on the 3rd it
              read R$ 16.200,00 against R$ 200,00 actually spent. */}
          {data.despesaAgendada > 0 && (
            <p className="mt-1 text-xs text-muted-foreground">
              + {formatBRL(data.despesaAgendada)} a vencer
            </p>
          )}
          <p className="mt-1 text-xs text-muted-foreground">
            {trendLabel(trends.despesa, period)}
          </p>
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
          <HealthDot status={data.status} />
          <h2>Saúde financeira</h2>
          <span className="font-normal text-muted-foreground">· {HEALTH_LABEL[data.status]}</span>
        </CardTitle>
        <CardAction>
          <div className="text-right">
            <p className="text-3xl font-bold tracking-tight tabular-nums">{data.score}</p>
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

// What the projection is standing on, in the user's words. The backend decides
// which one applies (Projection.basis) — the card must not infer confidence
// from the amounts, since only the backend knows how much of the window traded.
//
// A closed month has no note: nothing was estimated, the figure *is* the month's
// faturamento, and captioning it "pela média das últimas 8 semanas" would
// present the one number that is not a forecast as though it were one.
const projectionBasisNote: Partial<Record<ProjectionBasis, string>> = {
  janela: 'Pela média de cada dia da semana nas últimas 8 semanas, com peso maior para as mais recentes.',
  parcial: 'Menos de uma semana de vendas registradas — ainda vai mudar bastante.',
  sem_base: 'Sem vendas registradas nas últimas 8 semanas para projetar.',
}

/**
 * GoalBar draws the month against its goal on one scale: what has been billed,
 * what the projection adds to it, and where the goal sits.
 *
 * The two amounts were a pair of label-and-figure rows, which asks the reader to
 * hold three numbers in their head to answer the only question they came with —
 * does the bar clear the mark or not.
 */
function GoalBar({ actual, remaining, target }: {
  actual: number
  remaining: number
  target: number
}) {
  const scale = Math.max(actual + remaining, target)
  if (scale <= 0) return null

  const width = (value: number) => `${Math.max(Math.min((value / scale) * 100, 100), 0)}%`

  return (
    <div className="space-y-2">
      <div className="relative">
        <div className="flex h-3 w-full overflow-hidden rounded-full bg-muted">
          <div style={{ width: width(actual), background: 'var(--primary)' }} />
          <div
            style={{
              width: width(remaining),
              background: 'color-mix(in oklch, var(--primary) 32%, transparent)',
            }}
          />
        </div>
        <span
          aria-hidden
          className="absolute -inset-y-1 w-0.5 -translate-x-1/2 rounded-full"
          style={{ left: width(target), background: 'var(--accent-strong)' }}
        />
      </div>
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs">
        <span className="flex items-baseline gap-1.5">
          <span className="size-2 rounded-full" style={{ background: 'var(--primary)' }} aria-hidden />
          <span className="text-muted-foreground">Faturado</span>
          <span className="font-medium tabular-nums">{formatBRL(actual)}</span>
        </span>
        <span className="flex items-baseline gap-1.5">
          <span
            className="size-2 rounded-full"
            style={{ background: 'color-mix(in oklch, var(--primary) 32%, transparent)' }}
            aria-hidden
          />
          <span className="text-muted-foreground">Estimativa restante</span>
          <span className="font-medium tabular-nums">+{formatBRL(remaining)}</span>
        </span>
      </div>
    </div>
  )
}

// ProjectionSection renders the backend's projection verbatim. It used to
// recompute the whole thing here — the projection, the gap and its own
// "necessário por dia útil" — off the weekday averages and the browser clock,
// which put a second, smaller daily target on the same screen as the one the
// health insight and the recommendation were quoting, and made the card
// disagree with what the WhatsApp bot reported for the same month.
//
// It leads the page: the owner opens Análise to ask whether the month will land
// on its goal, and this card used to be the fifth answer down, under the KPIs,
// the score, the insights and the weekday averages.
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

  const windowLabel = period.inProgress ? `(até o dia ${period.comparableThroughDay})` : '(fechado)'

  return (
    <Section
      title="Projeção do mês"
      icon={Target}
      action={projection.target > 0 ? (
        <div className="text-right">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Meta</p>
          <p className="text-lg font-semibold tracking-tight whitespace-nowrap text-accent-strong tabular-nums">
            {formatBRL(projection.target)}
          </p>
        </div>
      ) : undefined}
      empty={projection.target <= 0
        ? 'Defina uma meta de faturamento em Metas para acompanhar a projeção do mês.'
        : undefined}
    >
      <>
        <div>
          <p className="text-4xl font-semibold tracking-tight tabular-nums">
            {formatBRL(projection.projected)}
          </p>
          <p className={`mt-1 text-sm font-medium ${PROJECTION_TONE[projection.status]}`}>
            Equivale a {Math.round(projection.coverage * 100)}% da meta
          </p>
          {/* How the number was reached. The days still to come used to be
              priced from this month's own finished days, so on the 3rd every
              weekday the month had not reached yet counted as zero and the
              projection came in at a quarter of the goal — a figure the card
              presented with no qualification at all. */}
          {projectionBasisNote[projection.basis] && (
            <p className="mt-1 text-xs text-muted-foreground">
              {projectionBasisNote[projection.basis]}
            </p>
          )}
        </div>

        <GoalBar
          actual={projection.actual}
          remaining={projection.remaining}
          target={projection.target}
        />

        <div className="space-y-2 border-t pt-3">
          {period.inProgress && period.comparableThroughDay === 0 ? (
            /* Two different reasons, and the user is owed the right one. On the
               1st nothing has closed at all; through the 7th days have closed
               but they are not the same weekdays the previous month offers, so
               the two sides were never comparable. Both used to be drawn as a
               fall. */
            <p className="text-sm text-muted-foreground">
              {period.throughDay === 0
                ? 'O mês está começando — ainda não há dia fechado para comparar.'
                : 'A primeira semana do mês ainda não fechou — a comparação com o mês passado começa no dia 8.'}
            </p>
          ) : (
            <>
              <Row label={`Este mês ${windowLabel}`} value={formatBRL(faturamento.current)} />
              <Row label={`Mês passado ${windowLabel}`} value={formatBRL(faturamento.previous)} />
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
          <p className="flex items-center gap-1.5 border-t pt-3 text-sm text-success">
            <CheckCircle2 className="size-4 shrink-0" aria-hidden />
            Se mantiver a média, fecha no azul
          </p>
        ) : (
          <div className="space-y-2 border-t pt-3">
            <Row
              label="Faltam para a meta"
              value={formatBRL(projection.gap)}
              valueClassName="text-base text-destructive"
            />
            {/* Today's own target lives on the weekday strip below, drawn against
                what this weekday usually brings. When the backend cannot compute
                one, the month's remaining days share the shortfall evenly —
                every remaining day, not just business days: the shop trades on
                weekends too, and the label used to say "dia útil" over a figure
                divided by all of them. */}
            {!projection.todayTarget?.valid && (
              projection.neededPerDay > 0 ? (
                <Row
                  label={`Necessário por dia (${pluralDias(projection.daysRemaining)}, hoje incluído)`}
                  value={formatBRL(projection.neededPerDay)}
                  valueClassName="text-base"
                />
              ) : (
                <p className="text-sm text-muted-foreground">Mês encerrado — não há mais dias para recuperar.</p>
              )
            )}
          </div>
        )}
      </>
    </Section>
  )
}

function WeekComparisonSection({ data }: { data: Analysis['weekComparison'] }) {
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
    <Section title="Comportamento semanal" icon={Clock}>
      <div className="space-y-2">
        <Row label={`Esta semana (até ${todayPt})`} value={formatBRL(data.current)} />
        {/* An accumulated total over the finished days, not a mean — the same
            shape as the row below it, which is what makes the two comparable.
            "Média diária" printed three days of takings as one day's average. */}
        <Row
          label={`Ritmo até ontem (${pluralFechados(data.pace.days)})`}
          value={formatBRL(data.pace.current)}
        />
        <Row label="Semana passada (mesmos dias)" value={formatBRL(data.pace.previous)} />
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
    </Section>
  )
}

function CashOutSection({ data }: { data: Analysis['cashOutDays'] }) {
  return (
    <Section
      title="Dias com maior saída de caixa"
      icon={CircleDollarSign}
      iconClassName="text-destructive"
      empty={data.length === 0 ? 'Nenhuma despesa registrada neste mês.' : undefined}
    >
      <div className="space-y-4">
        {data.map((day) => (
          <div key={day.date} className="space-y-2">
            <div className="flex items-baseline justify-between gap-3">
              <span className="text-sm font-medium">{dayMonthLabel(day.date)}</span>
              <span className="text-sm font-semibold tabular-nums">{formatBRL(day.total)}</span>
            </div>
            <ul className="space-y-1">
              {day.items.map((item, i) => (
                <li key={i} className="flex items-baseline justify-between gap-3 text-xs text-muted-foreground">
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

// One quantity split into parts, so the ramp is one hue getting lighter as the
// slices get smaller — not five colours cycling. The old palette handed out
// gold, blue, gold, navy, red in rotation, which on the demo's ten categories
// repeated itself twice and implied a kinship between "Impostos" and
// "Fornecedor de Medicamentos" that does not exist. It also spent the brand
// gold down here, where the goal at the top of the page now needs it.
function compositionColor(index: number, total: number): string {
  const step = total > 1 ? (index / (total - 1)) * 55 : 0
  return `color-mix(in oklch, var(--primary) ${100 - step}%, transparent)`
}

function CompositionSection({ data }: { data: Analysis['expenseComposition'] }) {
  return (
    <Section
      title="Composição de despesas"
      icon={PieChart}
      empty={data.length === 0 ? 'Nenhuma despesa registrada neste mês.' : undefined}
    >
      <>
        {data.map((item, i) => (
          <div key={item.categoryId}>
            <div className="mb-1 flex items-baseline justify-between gap-3">
              <span className="text-sm">{item.categoryName}</span>
              <span className="text-sm font-semibold tabular-nums">{formatBRL(item.amount)}</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full transition-[width] duration-500 motion-reduce:transition-none"
                  style={{
                    width: `${item.percentage}%`,
                    background: compositionColor(i, data.length),
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
    <Section title="Posição de caixa" icon={Wallet}>
      <>
        <Row label="Saldo hoje" value={formatBRL(data.currentBalance)} />
        <Row
          label="Projeção fim do mês"
          value={formatBRL(data.endOfMonthProjection)}
          valueClassName={data.endOfMonthProjection >= 0 ? 'text-success' : 'text-destructive'}
        />
        {/* What the projection assumes, said out loud. The month's bills are all
            booked on the 1st and its sales are recorded as they happen, so a
            curve of lançamentos alone dives every month — the days ahead are
            credited with an ordinary day's receipts to make the figure mean
            what the label says. Without any trading history there is nothing to
            credit them with, and the card says that instead of alarming. */}
        <p className="text-xs text-muted-foreground">
          {data.expectsReceipts
            ? 'Contando o recebimento médio de cada dia da semana nos dias que faltam.'
            : 'Sem histórico de recebimento para projetar os dias que faltam — só o que já está lançado.'}
        </p>
        {data.expectsReceipts && data.daysUntilNegative !== null && (
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

// The skeleton draws the page's actual shape — the two lead cards, the KPI row
// and three pairs — so nothing moves when the data lands. It used to draw four
// KPI blocks over a row that renders five, and eight identical strips over a
// page that had stopped being a single column.
function LoadingSkeleton() {
  return (
    <>
      <Skeleton className="h-72" />
      <Skeleton className="h-64" />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-26" />
        ))}
      </div>
      {Array.from({ length: 3 }, (_, row) => (
        <div key={row} className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Skeleton className="h-44" />
          <Skeleton className="h-44" />
        </div>
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
  return (
    <>
      {/* The two questions the page exists to answer, in the order they are
          asked: does the month land on its goal, and what does today have to
          bring. Everything below breaks those two down. */}
      <ProjectionSection
        projection={analysis.projection}
        faturamento={analysis.trends.faturamento}
        period={analysis.period}
      />

      <Section
        title="A semana da farmácia"
        icon={Calendar}
      >
        <WeekRhythm days={analysis.weekdays} todayTarget={analysis.projection.todayTarget} />
      </Section>

      <KpiSection
        data={analysis.kpis}
        trends={analysis.trends}
        period={analysis.period}
      />

      {/* Paired, not stacked: nine cards in one column gave every block the same
          weight and made the page a scroll with no shape. */}
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <HealthSection data={analysis.health} />
        <RecommendationSection data={analysis.recommendations} />
      </div>
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <CashPositionSection data={analysis.cashPosition} />
        <WeekComparisonSection data={analysis.weekComparison} />
      </div>
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <CashOutSection data={analysis.cashOutDays} />
        <CompositionSection data={analysis.expenseComposition} />
      </div>
    </>
  )
}

export default function Analysis() {
  const month = currentMonthKey() as YearMonth
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
