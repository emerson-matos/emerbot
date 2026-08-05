import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const useMonthlyTrend = vi.hoisted(() => vi.fn());
const useMonthlyAnalysis = vi.hoisted(() => vi.fn());
vi.mock("@/api/queries", () => ({ useMonthlyTrend }));
vi.mock("@/hooks/useMonthlyAnalysis", () => ({ useMonthlyAnalysis }));

// Recharts measures its container and jsdom reports every element as 0×0, so
// the chart draws nothing without a fixed width.
vi.mock("recharts", async () => {
  const actual = await vi.importActual<typeof import("recharts")>("recharts");
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <actual.ResponsiveContainer width={800} height={220}>
        {children as React.ReactElement}
      </actual.ResponsiveContainer>
    ),
  };
});

import IncomeExpenseChart from "./IncomeExpenseChart";
import { monthBars } from "@/lib/monthBars";

/**
 * Two closed months and one five days in — the shape that made the current
 * month read as a collapse. Its R$ 5.394,96 of sales stood beside a red bar
 * carrying the whole month's expenses, scheduled ones included, because
 * summary.TotalExpense covers every bill booked for the month.
 */
const months = [
  { month: "mai", revenue: 3600000, expense: 3600000 },
  { month: "jun", revenue: 3600000, expense: 3600000 },
  { month: "jul", revenue: 539496, expense: 1913095 },
];

describe("monthBars", () => {
  it("stacks the month's projection on top of what it has sold", () => {
    // Projected to close at R$ 36.614,56 against R$ 5.394,96 booked.
    const bars = monthBars(months, { actual: 539496, projected: 3661456 });

    const jul = bars[2];
    expect(jul.Faturamento).toBe(5394.96);
    expect(jul["Projeção"]).toBeCloseTo(31219.6, 2);
    // The stack lands exactly on the backend's projected total — the same
    // figure the KPI card and the bot quote.
    expect(jul.Faturamento + jul["Projeção"]).toBeCloseTo(36614.56, 2);
  });

  it("projects nothing for the months that have closed", () => {
    const bars = monthBars(months, { actual: 539496, projected: 3661456 });

    // Drawing an amber segment on a finished month would invent a future for a
    // month that has none.
    expect(bars[0]["Projeção"]).toBe(0);
    expect(bars[1]["Projeção"]).toBe(0);
    expect(bars[0].Faturamento).toBe(36000);
  });

  it("projects nothing while the analysis has not landed", () => {
    // The months that did load still draw: a missing projection must not blank
    // the chart or hold it back.
    const bars = monthBars(months, undefined);

    expect(bars.map((b) => b["Projeção"])).toEqual([0, 0, 0]);
    expect(bars[2].Faturamento).toBe(5394.96);
  });

  it("never draws a segment below zero", () => {
    // A ledger with no trading history projects exactly what it sold, and a
    // negative segment would render as a bar growing down out of the axis.
    expect(monthBars(months, { actual: 539496, projected: 539496 })[2]["Projeção"]).toBe(0);
    expect(monthBars(months, { actual: 539496, projected: 400000 })[2]["Projeção"]).toBe(0);
  });
});

describe("IncomeExpenseChart", () => {
  it("renders the three months once every summary has loaded", () => {
    useMonthlyTrend.mockReturnValue(
      months.map((m) => ({
        isSuccess: true,
        data: { TotalRevenue: m.revenue, TotalExpense: m.expense },
      })),
    );
    useMonthlyAnalysis.mockReturnValue({
      data: { projection: { actual: 539496, projected: 3661456 } },
    });

    const { container } = render(<IncomeExpenseChart />);
    const text = container.textContent ?? "";

    // The amber series is named in the legend, so the colour is not the only
    // thing carrying "this part has not happened yet".
    expect(text).toContain("Projeção");
    expect(text).toContain("Faturamento");
    expect(text).toContain("Despesas");
  });
});
