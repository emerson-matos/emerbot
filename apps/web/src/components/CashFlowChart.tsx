import { useId, useMemo } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ReferenceDot,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { LineChart as LineChartIcon } from "lucide-react";
import { format, parseISO } from "date-fns";
import { ptBR } from "date-fns/locale";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { brlAxisTick, chartColor, tooltipProps } from "@/lib/chart";
import { formatBRL } from "@/lib/format";
import { todayISO } from "@/lib/entries";
import type { DayCash } from "../api/types";

interface Props {
  /**
   * The backend's projected curve (`cashPosition.forecast`), not the booked
   * one. Those are different lines and the difference is the whole month: the
   * pharmacy's bills are all recorded on the 1st and its sales as they happen,
   * so a curve of lançamentos alone dives past today every single month. This
   * chart drew that one and labelled its tail "projeção", while the card beside
   * it and the WhatsApp bot both quoted the credited figure — one month, two
   * pictures. See ADR-021.
   */
  data: DayCash[];
}

function median(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

// Fraction (from the top) at which R$ 0 sits inside [min, max]. SVG gradients
// map onto each path's own bounding box, so every series needs its offset
// derived from its own extent for the color flip to land exactly on zero.
function zeroOffsetFor(values: number[]): number {
  if (!values.length) return 1;
  const max = Math.max(...values);
  const min = Math.min(...values);
  if (max <= 0) return 0;
  if (min >= 0) return 1;
  return max / (max - min);
}

export default function CashFlowChart({ data }: Props) {
  const gradientId = useId();

  const { formatted, todayPoint, medianBalance, offsets } = useMemo(() => {
    const today = todayISO();

    const formatted = data.map((point) => {
      const balance = point.balance / 100;
      const label = format(parseISO(point.date), "dd/MM", {
        locale: ptBR,
      });

      return {
        ...point,
        label,
        balance,

        // Today belongs to both series so the solid line and the dashed one
        // meet instead of leaving a gap at the join.
        actual: point.date <= today ? balance : null,
        forecast: point.date >= today ? balance : null,
      };
    });

    // Median of the daily balances observed so far (forecast days would skew
    // the stat with projections, so they only count when nothing has happened
    // yet this month).
    const actualBalances = formatted
      .filter((p) => p.actual !== null)
      .map((p) => p.balance);
    const medianBalance = actualBalances.length
      ? median(actualBalances)
      : formatted.length
        ? median(formatted.map((p) => p.balance))
        : 0;

    const forecastBalances = formatted
      .filter((p) => p.forecast !== null)
      .map((p) => p.balance);
    const offsets = {
      // The line path spans only the series' values…
      actualStroke: zeroOffsetFor(actualBalances),
      forecastStroke: zeroOffsetFor(forecastBalances),
      // …while the area path always reaches down/up to the zero baseline.
      actualFill: zeroOffsetFor([...actualBalances, 0]),
    };

    return {
      formatted,
      todayPoint: formatted.find((p) => p.date === today),
      medianBalance,
      offsets,
    };
  }, [data]);

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between gap-2 text-sm">
          <span className="flex items-center gap-2">
            <LineChartIcon className="size-4 text-primary" />
            Fluxo de Caixa do Mês
          </span>
          <span className="text-xs font-medium text-muted-foreground">
            Mediana/dia:{" "}
            <span className="font-semibold text-foreground tabular-nums">
              {formatBRL(Math.round(medianBalance * 100))}
            </span>
          </span>
        </CardTitle>
      </CardHeader>

      <CardContent>
        <ResponsiveContainer width="100%" height={320}>
          <AreaChart
            data={formatted}
            margin={{ top: 24, right: 12, left: 0, bottom: 0 }}
          >
            <defs>
              {/* Success above the zero line, destructive below it. */}
              <linearGradient
                id={`${gradientId}-actual-stroke`}
                x1="0"
                y1="0"
                x2="0"
                y2="1"
              >
                <stop offset={offsets.actualStroke} stopColor={chartColor.income} />
                <stop offset={offsets.actualStroke} stopColor={chartColor.expense} />
              </linearGradient>
              <linearGradient
                id={`${gradientId}-forecast-stroke`}
                x1="0"
                y1="0"
                x2="0"
                y2="1"
              >
                <stop offset={offsets.forecastStroke} stopColor={chartColor.income} />
                <stop offset={offsets.forecastStroke} stopColor={chartColor.expense} />
              </linearGradient>
              <linearGradient
                id={`${gradientId}-fill`}
                x1="0"
                y1="0"
                x2="0"
                y2="1"
              >
                <stop
                  offset={offsets.actualFill}
                  stopColor={chartColor.income}
                  stopOpacity={0.18}
                />
                <stop
                  offset={offsets.actualFill}
                  stopColor={chartColor.expense}
                  stopOpacity={0.18}
                />
              </linearGradient>
            </defs>

            <CartesianGrid
              vertical={false}
              stroke={chartColor.grid}
              strokeDasharray="3 3"
              opacity={0.2}
            />

            <XAxis
              dataKey="label"
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 11, fill: chartColor.axis }}
              minTickGap={24}
            />

            <YAxis
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 11, fill: chartColor.axis }}
              tickFormatter={brlAxisTick}
            />

            {/* The two series are the same balance under different regimes, so
                the tooltip has to name which one the cursor is on — "Saldo" over
                a day that has not happened is a claim the number cannot back. */}
            <Tooltip
              {...tooltipProps}
              formatter={(value, name) => [
                formatBRL(Number(value ?? 0) * 100),
                name === "forecast" ? "Saldo projetado" : "Saldo",
              ]}
            />

            <ReferenceLine
              y={0}
              stroke={chartColor.grid}
              strokeWidth={1.5}
              strokeDasharray="4 4"
            />

            {todayPoint && (
              <>
                <ReferenceLine
                  x={todayPoint.label}
                  stroke={chartColor.today}
                  strokeDasharray="4 4"
                  label={{
                    value: "Hoje",
                    position: "insideTop",
                    dx: -24,
                    dy: 8,
                    fontSize: 16,
                    fill: chartColor.today,
                  }}
                />
                <ReferenceDot
                  x={todayPoint.label}
                  y={todayPoint.balance}
                  r={4}
                  fill={chartColor.today}
                  stroke="#fff"
                  strokeWidth={2}
                />
              </>
            )}

            <Area
              type="monotone"
              dataKey="actual"
              stroke={`url(#${gradientId}-actual-stroke)`}
              strokeWidth={2.5}
              fill={`url(#${gradientId}-fill)`}
              dot={false}
              connectNulls
            />

            <Area
              type="monotone"
              dataKey="forecast"
              stroke={`url(#${gradientId}-forecast-stroke)`}
              strokeWidth={2.5}
              strokeDasharray="6 4"
              fill="none"
              dot={false}
              connectNulls
            />
          </AreaChart>
        </ResponsiveContainer>

        <div className="mt-2 flex flex-wrap justify-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="size-2 rounded-full" style={{ background: chartColor.income }} />
            Acima de zero
          </span>
          <span className="flex items-center gap-1.5">
            <span className="size-2 rounded-full" style={{ background: chartColor.expense }} />
            Abaixo de zero
          </span>
          <span className="flex items-center gap-1.5">
            <span className="size-2 rounded-full" style={{ background: chartColor.today }} />
            Hoje
          </span>
          {/* What the dashed half actually is. It used to say "projeção" over
              the booked curve, which is the one thing it was not. */}
          <span className="flex items-center gap-1.5">
            <span className="h-px w-4 border-t-2 border-dashed border-muted-foreground" />
            Projetado: recebimento médio de cada dia da semana
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
