/**
 * Chart colors reference CSS custom properties defined in index.css, so every
 * chart follows the active theme (light/dark) with zero re-render bookkeeping —
 * SVG fill/stroke resolve `var(--…)` natively.
 */
export const chartColor = {
  income: "var(--success)",
  expense: "var(--destructive)",
  today: "var(--accent)",
  grid: "var(--border)",
  axis: "var(--muted-foreground)",
} as const;

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
