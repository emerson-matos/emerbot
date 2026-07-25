import { describe, expect, it } from "vitest";
import { makeCashFlowPoint, makeEntry, makeSummary } from "@/test/factories";
import { buildMonthlyAnalysis } from "./build";
import type { AnalysisInput } from "./types";

/**
 * `summaries` and `goals` run oldest-first across the trailing three-month
 * window, so the month under analysis sits in the last slot — matching what
 * useMonthlyAnalysis builds from months3.
 *
 * `now` is Thursday 19 Feb 2026, built locally: the helper derives the week
 * window from local getDay()/getDate(), and mid-month keeps both the current
 * and previous week inside February.
 */
const now = new Date(2026, 1, 19, 12, 0, 0);

const sale = (date: string, amount: number) =>
  makeEntry({ TransactionDate: date, Amount: amount });

function makeInput(overrides: Partial<AnalysisInput> = {}): AnalysisInput {
  return {
    month: "2026-02",
    now,
    entries: [
      sale("2026-02-17", 1000), // this week (Tue)
      sale("2026-02-19", 2000), // this week (Thu, today)
      sale("2026-02-10", 500), // last week, on or before Thu
      sale("2026-02-13", 300), // last week, after Thu
      makeEntry({
        TransactionDate: "2026-02-18",
        Amount: 400,
        Type: "expense",
        Category: "aluguel",
      }),
    ],
    previousEntries: [
      sale("2026-01-05", 700), // day 5 <= today's day-of-month
      sale("2026-01-25", 900), // day 25 > today's day-of-month
      makeEntry({
        TransactionDate: "2026-01-06",
        Amount: 100,
        Type: "expense",
        Category: "aluguel",
      }),
    ],
    summaries: [
      makeSummary({
        Month: "2025-12",
        TotalIncome: 6000,
        TotalExpense: 2000,
        Balance: 4000,
      }),
      makeSummary({
        Month: "2026-01",
        TotalIncome: 8000,
        TotalExpense: 3000,
        Balance: 5000,
      }),
      makeSummary({
        Month: "2026-02",
        TotalIncome: 10000,
        TotalExpense: 4000,
        Balance: 6000,
      }),
    ],
    goals: [
      { revenueTarget: 5000, expenseTarget: 2000 },
      { revenueTarget: 9000, expenseTarget: 4000 },
      { revenueTarget: 20000, expenseTarget: 8000 },
    ],
    cashFlowPoints: [
      makeCashFlowPoint({ Date: "2026-02-19", RunningBalance: 5000 }),
      makeCashFlowPoint({ Date: "2026-02-28", RunningBalance: 9000 }),
    ],
    ...overrides,
  };
}

describe("buildMonthlyAnalysis kpis", () => {
  it("takes the headline figures from the current summary", () => {
    const { kpis } = buildMonthlyAnalysis(makeInput());

    expect(kpis.receita).toBe(10000);
    expect(kpis.despesa).toBe(4000);
    expect(kpis.resultado).toBe(6000);
  });

  it("counts the days left in the calendar month", () => {
    const { kpis } = buildMonthlyAnalysis(makeInput());

    // February 2026 has 28 days; the 19th leaves 9.
    expect(kpis.daysRemaining).toBe(9);
  });

  it("sums last month's counter sales only up to the current day of month", () => {
    const { kpis } = buildMonthlyAnalysis(makeInput());

    // 700 on the 5th counts, 900 on the 25th does not, and the expense
    // never does.
    expect(kpis.previousMonthIncomeUpToDay).toBe(700);
  });

  it("falls back to zeroes when no summary is available", () => {
    const { kpis } = buildMonthlyAnalysis(makeInput({ summaries: [] }));

    expect(kpis.receita).toBe(0);
    expect(kpis.despesa).toBe(0);
    expect(kpis.resultado).toBe(0);
  });
});

describe("buildMonthlyAnalysis weekComparison", () => {
  it("sums this week from Monday through today", () => {
    const { weekComparison } = buildMonthlyAnalysis(makeInput());

    expect(weekComparison.current).toBe(3000);
  });

  it("sums the whole previous week", () => {
    const { weekComparison } = buildMonthlyAnalysis(makeInput());

    expect(weekComparison.previous).toBe(800);
  });

  // The like-for-like number: comparing a partial week against a full one
  // would always look like a decline.
  it("sums the previous week only up to the same weekday", () => {
    const { weekComparison } = buildMonthlyAnalysis(makeInput());

    expect(weekComparison.previousUpToDay).toBe(500);
  });

  it("labels the elapsed days of this week and stops at today", () => {
    const { weekComparison } = buildMonthlyAnalysis(makeInput());

    expect(weekComparison.labels).toEqual(["Seg", "Ter", "Qua", "Qui"]);
  });

  it("carries the revenue target through", () => {
    const { weekComparison } = buildMonthlyAnalysis(makeInput());

    expect(weekComparison.monthlyTarget).toBe(20000);
  });

  it("projects the rest of the week from last week's daily average", () => {
    const { weekComparison } = buildMonthlyAnalysis(makeInput());

    // 3000 so far + (800 / 7) per day for the 3 days left.
    expect(weekComparison.projectedWeekly).toBeCloseTo(3000 + (800 / 7) * 3, 5);
  });

  it("projects the month from the weekly run rate", () => {
    const { weekComparison } = buildMonthlyAnalysis(makeInput());

    const projectedWeekly = 3000 + (800 / 7) * 3;
    // 3800 booked so far + the daily rate across the 9 remaining days.
    expect(weekComparison.projectedMonthly).toBeCloseTo(
      3800 + (projectedWeekly / 7) * 9,
      5,
    );
  });

  // The Sunday immediately before this Monday belongs to the previous week.
  // Deriving the Monday boundary from a local date but serialising it with
  // toISOString() slides it a day earlier in any UTC+ timezone, which would
  // pull this sale into the current week.
  it("puts the preceding Sunday in the previous week, in any timezone", () => {
    const { weekComparison } = buildMonthlyAnalysis(
      makeInput({
        entries: [sale("2026-02-15", 999), sale("2026-02-17", 1000)],
      }),
    );

    expect(weekComparison.current).toBe(1000);
    expect(weekComparison.previous).toBe(999);
  });

  it("treats a week with no sales as a flat baseline", () => {
    const { weekComparison } = buildMonthlyAnalysis(
      makeInput({ entries: [] }),
    );

    expect(weekComparison.current).toBe(0);
    expect(weekComparison.previous).toBe(0);
    expect(weekComparison.projectedWeekly).toBe(0);
  });
});

describe("buildMonthlyAnalysis composition", () => {
  it("builds the three-month history ending at the requested month", () => {
    const { history } = buildMonthlyAnalysis(makeInput());

    expect(history.map((h) => h.month)).toEqual([
      "2025-12",
      "2026-01",
      "2026-02",
    ]);
  });

  it("measures goal progress against counter sales, not total income", () => {
    const { goals } = buildMonthlyAnalysis(makeInput());

    // 1000 + 2000 + 500 + 300 of venda_balcao, not the 10000 of TotalIncome.
    expect(goals.revenueActual).toBe(3800);
    expect(goals.revenueTarget).toBe(20000);
  });

  it("delegates the per-weekday breakdown", () => {
    const { weekdays } = buildMonthlyAnalysis(makeInput());

    expect(weekdays).toHaveLength(7);
    expect(weekdays.find((d) => d.isToday)?.label).toBe("Qui");
  });

  it("delegates the expense composition", () => {
    const { expenseComposition } = buildMonthlyAnalysis(makeInput());

    expect(expenseComposition).toEqual([
      {
        categoryId: "aluguel",
        categoryName: "Aluguel",
        amount: 400,
        percentage: 100,
      },
    ]);
  });

  it("delegates the cash position", () => {
    const { cashPosition } = buildMonthlyAnalysis(makeInput());

    expect(cashPosition.currentBalance).toBe(5000);
    expect(cashPosition.endOfMonthProjection).toBe(9000);
  });

  it("returns every section of the analysis", () => {
    const analysis = buildMonthlyAnalysis(makeInput());

    expect(Object.keys(analysis).sort()).toEqual([
      "cashOutDays",
      "cashPosition",
      "expenseComposition",
      "goals",
      "health",
      "highlights",
      "history",
      "kpis",
      "recommendations",
      "trends",
      "weekComparison",
      "weekdays",
    ]);
  });

  it("survives an entirely empty month", () => {
    const analysis = buildMonthlyAnalysis(
      makeInput({
        entries: [],
        previousEntries: [],
        summaries: [],
        goals: [],
        cashFlowPoints: [],
      }),
    );

    expect(analysis.kpis.resultado).toBe(0);
    expect(analysis.weekdays).toHaveLength(7);
    expect(analysis.expenseComposition).toEqual([]);
    expect(analysis.history).toHaveLength(3);
  });
});

describe("buildMonthlyAnalysis input ordering", () => {
  // The window runs oldest-first, so the analysed month is the last slot and
  // the comparison month the one before it. Reading from the front instead
  // would report figures from two months ago as the current month.
  it("reads the analysed month from the last slot", () => {
    const { kpis } = buildMonthlyAnalysis(
      makeInput({
        summaries: [
          makeSummary({ Month: "2025-12", TotalIncome: 111 }),
          makeSummary({ Month: "2026-01", TotalIncome: 222 }),
          makeSummary({ Month: "2026-02", TotalIncome: 333 }),
        ],
      }),
    );

    expect(kpis.receita).toBe(333);
  });

  it("reads the comparison month from the slot before it", () => {
    const { trends } = buildMonthlyAnalysis(
      makeInput({
        summaries: [
          makeSummary({ Month: "2025-12", TotalIncome: 50 }),
          makeSummary({ Month: "2026-01", TotalIncome: 100 }),
          makeSummary({ Month: "2026-02", TotalIncome: 200 }),
        ],
      }),
    );

    expect(trends.receita.current).toBe(200);
    expect(trends.receita.previous).toBe(100);
    expect(trends.receita.change).toBe(100);
  });

  it("reads the active goal from the last slot", () => {
    const { goals } = buildMonthlyAnalysis(makeInput());

    expect(goals.revenueTarget).toBe(20000);
  });

  // A month the API has no row for arrives as a hole. Collapsing it would
  // pull an older month into the current slot.
  it("keeps its bearings when an older month has no data", () => {
    const { kpis, history } = buildMonthlyAnalysis(
      makeInput({
        summaries: [
          undefined,
          undefined,
          makeSummary({ Month: "2026-02", TotalIncome: 777, Balance: 777 }),
        ],
      }),
    );

    expect(kpis.receita).toBe(777);
    expect(history.map((h) => h.income)).toEqual([0, 0, 777]);
  });

  it("handles the analysed month itself having no data", () => {
    const { kpis } = buildMonthlyAnalysis(
      makeInput({
        summaries: [
          makeSummary({ Month: "2025-12", TotalIncome: 111 }),
          makeSummary({ Month: "2026-01", TotalIncome: 222 }),
          undefined,
        ],
      }),
    );

    expect(kpis.receita).toBe(0);
  });
});
