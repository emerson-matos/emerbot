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
import { formatBRL } from "../api/client";
import type { CashFlowPoint } from "../api/client";

interface Props {
  data: CashFlowPoint[];
}

export default function CashFlowChart({ data }: Props) {
  const gradientId = useId();

  const { formatted, todayPoint } = useMemo(() => {
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

    return {
      formatted,
      todayPoint: formatted.find((p) => p.Date === today),
    };
  }, [data]);

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm">
          <LineChartIcon className="size-4 text-primary" />
          Fluxo de Caixa do Mês
        </CardTitle>
      </CardHeader>

      <CardContent>
        <ResponsiveContainer width="100%" height={320}>
          <AreaChart
            data={formatted}
            margin={{ top: 24, right: 12, left: 0, bottom: 0 }}
          >
            <defs>
              <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                {/* positive */}
                <stop
                  offset="0%"
                  stopColor={chartColor.income}
                />

                {/* negative */}
                <stop
                  offset="100%"
                  stopColor={chartColor.expense}
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
              stroke={`url(#${gradientId})`}
              strokeWidth={2.5}
              fill={`url(#${gradientId})`}
              dot={false}
              connectNulls
            />

            <Area
              type="monotone"
              dataKey="forecast"
              stroke={`url(#${gradientId})`}
              strokeWidth={2.5}
              strokeDasharray="6 4"
              fill="none"
              dot={false}
              connectNulls
            />
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
