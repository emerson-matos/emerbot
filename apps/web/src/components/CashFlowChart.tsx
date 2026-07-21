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
import { chartColor, tooltipProps } from "@/lib/chart";
import { formatBRL } from "@/lib/format";
import type { CashFlowPoint } from "../api/types";

interface Props {
  data: CashFlowPoint[];
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
    const today = format(new Date(), "yyyy-MM-dd");

    const formatted = data.map((point) => {
      const balance = point.RunningBalance / 100;
      const label = format(parseISO(point.Date), "dd/MM", {
        locale: ptBR,
      });

      return {
        ...point,
        label,
        balance,

        actual: point.Date <= today ? balance : null,
        forecast: point.Date >= today ? balance : null,
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
      todayPoint: formatted.find((p) => p.Date === today),
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
              tickFormatter={(v: number) => {
                const abs = Math.abs(v);

                if (abs >= 1000) {
                  return `${v < 0 ? "-" : ""}R$${(abs / 1000).toFixed(0)}k`;
                }

                return `${v < 0 ? "-" : ""}R$${abs.toFixed(0)}`;
              }}
            />

            <Tooltip
              {...tooltipProps}
              formatter={(value) => [
                formatBRL(Number(value ?? 0) * 100),
                "Saldo",
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

        <div className="mt-2 flex justify-center gap-4 text-xs text-muted-foreground">
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
            Hoje / projeção
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
