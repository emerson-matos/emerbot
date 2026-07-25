import { describe, expect, it } from "vitest";
import { makeSummary } from "@/test/factories";
import { getGoalProgress, getHistory } from "./goals";

// Local construction: getGoalProgress reads getDate()/getMonth() in local time.
const now = new Date(2026, 1, 5, 12, 0, 0); // 5 Feb 2026, month has 28 days

describe("getGoalProgress", () => {
  it("derives days remaining from the calendar month", () => {
    const progress = getGoalProgress(makeSummary(), null, now, 0);

    expect(progress.daysTotal).toBe(28);
    expect(progress.daysRemaining).toBe(23);
  });

  it("handles a leap February", () => {
    const progress = getGoalProgress(
      makeSummary(),
      null,
      new Date(2028, 1, 5, 12, 0, 0),
      0,
    );

    expect(progress.daysTotal).toBe(29);
  });

  it("zeroes the targets when no goal is set but keeps the actuals", () => {
    const progress = getGoalProgress(
      makeSummary({ TotalExpense: 4000 }),
      null,
      now,
      7000,
    );

    expect(progress).toMatchObject({
      revenueTarget: 0,
      revenueActual: 7000,
      revenuePct: 0,
      expenseTarget: 0,
      expenseActual: 4000,
      expensePct: 0,
    });
  });

  it("computes progress percentages against the targets", () => {
    const progress = getGoalProgress(
      makeSummary({ TotalExpense: 2500 }),
      { revenueTarget: 20000, expenseTarget: 10000 },
      now,
      5000,
    );

    expect(progress.revenuePct).toBe(25);
    expect(progress.expensePct).toBe(25);
  });

  // The bar should cap at full, but the raw actual is still reported so the
  // UI can show "above target".
  it("caps percentages at 100 while keeping the real actual", () => {
    const progress = getGoalProgress(
      makeSummary({ TotalExpense: 30000 }),
      { revenueTarget: 10000, expenseTarget: 10000 },
      now,
      45000,
    );

    expect(progress.revenuePct).toBe(100);
    expect(progress.expensePct).toBe(100);
    expect(progress.revenueActual).toBe(45000);
    expect(progress.expenseActual).toBe(30000);
  });

  it("avoids dividing by a zero target", () => {
    const progress = getGoalProgress(
      makeSummary({ TotalExpense: 500 }),
      { revenueTarget: 0, expenseTarget: 0 },
      now,
      500,
    );

    expect(progress.revenuePct).toBe(0);
    expect(progress.expensePct).toBe(0);
  });
});

describe("getHistory", () => {
  it("zips summaries and goals onto the requested month range", () => {
    const history = getHistory(
      [
        makeSummary({ Month: "2026-01", TotalIncome: 1000, TotalExpense: 400 }),
        makeSummary({ Month: "2026-02", TotalIncome: 2000, TotalExpense: 900 }),
      ],
      [
        { revenueTarget: 1500, expenseTarget: 500 },
        { revenueTarget: 2500, expenseTarget: 800 },
      ],
      ["2026-01", "2026-02"],
    );

    expect(history).toHaveLength(2);
    expect(history[0]).toMatchObject({
      month: "2026-01",
      income: 1000,
      incomeTarget: 1500,
      expense: 400,
      expenseTarget: 500,
    });
    expect(history[1].income).toBe(2000);
  });

  // Months with no data still need a slot so the chart keeps a continuous axis.
  it("fills gaps with zeroed amounts and null targets", () => {
    const history = getHistory([], [], ["2026-01", "2026-02"]);

    expect(history).toHaveLength(2);
    expect(history[0]).toMatchObject({
      income: 0,
      expense: 0,
      incomeTarget: null,
      expenseTarget: null,
    });
  });

  it("labels each month in pt-BR", () => {
    const [january] = getHistory([], [], ["2026-01"]);

    expect(january.label).toMatch(/jan/i);
    expect(january.label).toContain("2026");
  });
});
