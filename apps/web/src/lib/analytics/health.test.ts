import { describe, expect, it } from "vitest";
import { makeEntry, makeSummary } from "@/test/factories";
import { getHealth } from "./health";
import {
  FinancialHealthStatus,
  InsightSeverity,
  InsightType,
  type GoalProgress,
  type WeekComparison,
} from "./types";

function makeWeek(overrides: Partial<WeekComparison> = {}): WeekComparison {
  return {
    current: 0,
    previous: 0,
    previousUpToDay: 0,
    projectedWeekly: 0,
    projectedMonthly: 0,
    monthlyTarget: 0,
    labels: [],
    ...overrides,
  };
}

function makeGoals(overrides: Partial<GoalProgress> = {}): GoalProgress {
  return {
    revenueTarget: 0,
    revenueActual: 0,
    revenuePct: 0,
    expenseTarget: 0,
    expenseActual: 0,
    expensePct: 0,
    daysRemaining: 0,
    daysTotal: 0,
    ...overrides,
  };
}

const types = (health: ReturnType<typeof getHealth>) =>
  health.messages.map((m) => m.type);

describe("getHealth status", () => {
  it("is boa when the month is positive and nothing is flagged", () => {
    const health = getHealth(
      [],
      makeSummary({ TotalIncome: 10000, TotalExpense: 4000, Balance: 6000 }),
      null,
      makeWeek(),
      makeGoals(),
    );

    expect(health.status).toBe(FinancialHealthStatus.Boa);
  });

  it("is critico when the balance is negative", () => {
    const health = getHealth(
      [],
      makeSummary({ TotalIncome: 1000, TotalExpense: 4000, Balance: -3000 }),
      null,
      makeWeek(),
      makeGoals(),
    );

    expect(health.status).toBe(FinancialHealthStatus.Critico);
    expect(types(health)).toContain(InsightType.LowCashFlow);
  });

  it("is atencao when a warning is raised on an otherwise positive month", () => {
    const health = getHealth(
      [],
      makeSummary({ TotalIncome: 9000, TotalExpense: 8000, Balance: 1000 }),
      makeSummary({ TotalIncome: 20000, TotalExpense: 4000 }),
      makeWeek(),
      makeGoals(),
    );

    expect(health.status).toBe(FinancialHealthStatus.Atencao);
    expect(types(health)).toContain(InsightType.RevenueDrop);
  });
});

describe("getHealth insights", () => {
  it("reports how many days closed in the black", () => {
    const health = getHealth(
      [
        makeEntry({ TransactionDate: "2026-02-05", Amount: 5000 }),
        makeEntry({
          TransactionDate: "2026-02-06",
          Amount: 5000,
          Type: "expense",
          Category: "aluguel",
        }),
      ],
      makeSummary({ TotalIncome: 5000, TotalExpense: 5000, Balance: 0 }),
      null,
      makeWeek(),
      makeGoals(),
    );

    expect(health.messages.some((m) => m.title === "1 dos 2 dias")).toBe(true);
  });

  it("expresses expenses as a share of revenue", () => {
    const health = getHealth(
      [],
      makeSummary({ TotalIncome: 10000, TotalExpense: 3000, Balance: 7000 }),
      null,
      makeWeek(),
      makeGoals(),
    );

    expect(
      health.messages.some((m) => m.title === "Despesas representam 30%"),
    ).toBe(true);
  });

  // Only flagged when spend outpaces revenue — growing costs on the back of
  // faster-growing sales is not a problem.
  it("flags expense growth only when it outpaces revenue growth", () => {
    const outpaced = getHealth(
      [],
      makeSummary({ TotalIncome: 11000, TotalExpense: 8000, Balance: 3000 }),
      makeSummary({ TotalIncome: 10000, TotalExpense: 4000 }),
      makeWeek(),
      makeGoals(),
    );
    expect(types(outpaced)).toContain(InsightType.ExpenseGrowth);

    const keepingUp = getHealth(
      [],
      makeSummary({ TotalIncome: 40000, TotalExpense: 8000, Balance: 32000 }),
      makeSummary({ TotalIncome: 10000, TotalExpense: 4000 }),
      makeWeek(),
      makeGoals(),
    );
    expect(types(keepingUp)).not.toContain(InsightType.ExpenseGrowth);
  });

  it("compares the week only when the previous week had revenue", () => {
    const improved = getHealth(
      [],
      makeSummary({ TotalIncome: 100, Balance: 100 }),
      null,
      makeWeek({ current: 2000, previousUpToDay: 1000 }),
      makeGoals(),
    );
    expect(types(improved)).toContain(InsightType.WeeklyImprovement);

    const declined = getHealth(
      [],
      makeSummary({ TotalIncome: 100, Balance: 100 }),
      null,
      makeWeek({ current: 500, previousUpToDay: 1000 }),
      makeGoals(),
    );
    expect(types(declined)).toContain(InsightType.WeeklyDecline);

    const noBaseline = getHealth(
      [],
      makeSummary({ TotalIncome: 100, Balance: 100 }),
      null,
      makeWeek({ current: 2000, previousUpToDay: 0 }),
      makeGoals(),
    );
    expect(types(noBaseline)).not.toContain(InsightType.WeeklyImprovement);
  });

  it("says the goal is on track when the daily pace covers what is left", () => {
    const health = getHealth(
      [],
      makeSummary({ TotalIncome: 8000, Balance: 8000 }),
      null,
      makeWeek(),
      makeGoals({
        revenueTarget: 10000,
        revenueActual: 8000,
        daysTotal: 28,
        daysRemaining: 14,
      }),
    );

    expect(types(health)).toContain(InsightType.GoalOnTrack);
  });

  it("warns when the goal needs a faster pace", () => {
    const health = getHealth(
      [],
      makeSummary({ TotalIncome: 1000, Balance: 1000 }),
      null,
      makeWeek(),
      makeGoals({
        revenueTarget: 100000,
        revenueActual: 1000,
        daysTotal: 28,
        daysRemaining: 14,
      }),
    );

    const behind = health.messages.find(
      (m) => m.type === InsightType.GoalBehind,
    );
    expect(behind?.severity).toBe(InsightSeverity.Warning);
    expect(behind?.description).toContain("14 dias");
    expect(health.status).toBe(FinancialHealthStatus.Atencao);
  });

  it("skips goal insights when no revenue target is set", () => {
    const health = getHealth(
      [],
      makeSummary({ TotalIncome: 1000, Balance: 1000 }),
      null,
      makeWeek(),
      makeGoals({ daysTotal: 28, daysRemaining: 14 }),
    );

    expect(types(health)).not.toContain(InsightType.GoalBehind);
    expect(types(health)).not.toContain(InsightType.GoalOnTrack);
  });
});
