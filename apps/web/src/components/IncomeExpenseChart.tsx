import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip,
  Legend, ResponsiveContainer,
} from 'recharts'
import { BarChart3 } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatBRL } from '@/lib/format'
import { chartColor, tooltipProps } from '@/lib/chart'
import { monthBars } from '@/lib/monthBars'
import { format } from 'date-fns';
import { useMonthlyTrend } from '@/api/queries';
import { useMonthlyAnalysis } from '@/hooks/useMonthlyAnalysis';
import { ptBR } from 'date-fns/locale';
import { currentMonthKey } from '@/lib/entries';
import type { YearMonth } from '@/api/types';

/**
 * The trailing three months, faturamento against despesa.
 *
 * The current month is drawn in two parts: what it has sold (green) and what
 * the backend projects it still will (amber, stacked on top). Without the
 * second part the chart lied by omission every early month — on the 5th, five
 * days of sales stood next to a red bar carrying the *whole* month's expenses,
 * because summary.TotalExpense counts every bill booked for it, the rent on
 * the 25th included. A month five days in read as a collapse.
 *
 * Stacking it makes both sides cover the same month again, and the seam
 * between the two segments is exactly where fact stops and forecast begins.
 * The projected amount is the backend's (Projection.projected) — the same
 * figure the KPI card and the bot quote, never re-derived here. See ADR-021.
 */
export default function IncomeExpenseChart() {
  const currentMonth = currentMonthKey()
  const [year, month] = currentMonth.split('-').map(Number)
  const months3 = [-2, -1, 0].map(offset =>
    format(new Date(year, month - 1 + offset, 1), 'yyyy-MM'),
  )
  const trendQueries = useMonthlyTrend(months3)
  // The analysis is already in cache on this page (GoalCard and the KPI cards
  // ask for it), so the projection costs no extra request. A month whose
  // analysis has not landed yet simply draws no amber segment rather than
  // holding the whole chart back.
  const projection = useMonthlyAnalysis(currentMonth as YearMonth).data?.projection

  const data = trendQueries.every(q => q.isSuccess)
    ? trendQueries.map((q, i) => ({
      month: format(new Date(months3[i] + '-01'), 'MMM', { locale: ptBR }),
      revenue: q.data!.TotalRevenue,
      expense: q.data!.TotalExpense,
    }))
    : []

  const chartData = monthBars(data, projection)

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <BarChart3 className="size-4 text-primary" aria-hidden />
          Faturamento × Despesas — 3 meses
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={chartData} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={chartColor.grid} vertical={false} />
            <XAxis dataKey="name" tick={{ fontSize: 12, fill: chartColor.axis }} tickLine={false} axisLine={false} />
            <YAxis
              tick={{ fontSize: 11, fill: chartColor.axis }}
              tickLine={false}
              axisLine={false}
              tickFormatter={v => `R$${(v / 1000).toFixed(0)}k`}
            />
            <Tooltip {...tooltipProps} formatter={v => formatBRL(Number(v) * 100)} />
            <Legend wrapperStyle={{ fontSize: 12 }} iconType="circle" />
            {/* One stack, so the amber sits on top of the green instead of
                beside it: together they are the month, not two months. A closed
                month's Projeção is 0 and Recharts draws nothing for it, which is
                why the bar keeps its rounded top. */}
            <Bar dataKey="Faturamento" stackId="faturamento" fill={chartColor.income} radius={[4, 4, 0, 0]} maxBarSize={44} />
            <Bar dataKey="Projeção" stackId="faturamento" fill={chartColor.projected} radius={[4, 4, 0, 0]} maxBarSize={44} />
            <Bar dataKey="Despesas" fill={chartColor.expense} radius={[4, 4, 0, 0]} maxBarSize={44} />
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}
