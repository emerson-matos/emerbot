/**
 * Chart colors reference CSS custom properties defined in index.css, so every
 * chart follows the active theme (light/dark) with zero re-render bookkeeping —
 * SVG fill/stroke resolve `var(--…)` natively.
 */
export const chartColor = {
  income: "var(--success)",
  expense: "var(--destructive)",
  today: "var(--accent)",
  /**
   * What has not happened yet. Amber rather than a lighter green on purpose:
   * a projection is a different *kind* of number from a realised one, not a
   * weaker shade of it, and a reader scanning a chart must be able to tell
   * where fact stops without reading a legend.
   */
  projected: "var(--warning)",
  /**
   * A running balance drawn over flows. Deliberately none of the three above:
   * green, amber and red already mean money moving, and the balance is a
   * different kind of quantity — a level, not a movement.
   */
  balance: "var(--primary)",
  grid: "var(--border)",
  axis: "var(--muted-foreground)",
} as const;

/**
 * A money tick for a chart axis, in thousands.
 *
 * The fraction digit is not decoration. Recharts picks tick values off the
 * data, and a week whose tallest bar is R$ 8.500,00 gets 0 / 2.500 / 5.000 /
 * 7.500 / 10.000 — which, rounded to whole thousands, prints an axis reading
 * R$0k, R$3k, R$5k, R$8k, R$10k. Two of those five labels name a number the
 * gridline is not at.
 */
export function brlAxisTick(value: number): string {
  const sign = value < 0 ? "-" : "";
  const thousands = Math.abs(value) / 1000;
  return `${sign}R$${thousands.toLocaleString("pt-BR", { maximumFractionDigits: 1 })}k`;
}

/** Ordered palette for categorical series (donuts, category bars). */
export const categoricalPalette = [
  "var(--chart-4)",
  "var(--chart-3)",
  "var(--chart-2)",
  "var(--chart-1)",
  "var(--chart-5)",
];

/** Shared Recharts <Tooltip> styling that respects the theme surface. */
export const tooltipProps = {
  cursor: { fill: "color-mix(in oklch, var(--foreground) 6%, transparent)" },
  contentStyle: {
    background: "var(--popover)",
    border: "1px solid var(--border)",
    borderRadius: "0.6rem",
    boxShadow:
      "0 8px 24px -12px color-mix(in oklch, var(--foreground) 30%, transparent)",
    fontSize: 13,
    color: "var(--popover-foreground)",
  },
  labelStyle: {
    color: "var(--muted-foreground)",
    fontSize: 12,
    marginBottom: 2,
  },
  itemStyle: { color: "var(--popover-foreground)" },
} as const;
