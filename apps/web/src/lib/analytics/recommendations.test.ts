import { describe, expect, it } from "vitest";
import { getRecommendations } from "./recommendations";
import {
  RecommendationSeverity,
  type CashPosition,
  type GoalProgress,
  type MonthTrend,
  type Trends,
  type WeekComparison,
} from "./types";

const flat: MonthTrend = {
  current: 0,
  previous: 0,
  change: 0,
  direction: "stable",
};

function makeInput(overrides: {
  week?: Partial<WeekComparison>;
  goals?: Partial<GoalProgress>;
  trends?: Partial<Trends>;
  cash?: Partial<CashPosition>;
} = {}) {
  const weekComparison: WeekComparison = {
    current: 0,
    previous: 0,
    previousUpToDay: 0,
    projectedWeekly: 0,
    projectedMonthly: 0,
    monthlyTarget: 0,
    labels: [],
    ...overrides.week,
  };

  const goals: GoalProgress = {
    revenueTarget: 0,
    revenueActual: 0,
    revenuePct: 0,
    expenseTarget: 0,
    expenseActual: 0,
    expensePct: 0,
    daysRemaining: 0,
    daysTotal: 0,
    ...overrides.goals,
  };

  const trends: Trends = {
    receita: flat,
    despesa: flat,
    resultado: flat,
    ...overrides.trends,
  };

  const cashPosition: CashPosition = {
    currentBalance: 0,
    endOfMonthProjection: 0,
    daysUntilNegative: null,
    lowestProjected: 0,
    lowestProjectedDate: "2026-02-05",
    ...overrides.cash,
  };

  return { weekComparison, goals, trends, cashPosition };
}

// Half the month gone with the target already met — comfortably on track.
const onTrackGoals = {
  revenueTarget: 10000,
  revenueActual: 9000,
  daysTotal: 28,
  daysRemaining: 14,
};

// Barely started against a large target — well behind pace.
const behindGoals = {
  revenueTarget: 100000,
  revenueActual: 1000,
  daysTotal: 28,
  daysRemaining: 14,
};

describe("getRecommendations goal pacing", () => {
  it("celebrates a rising week that still fits the projection", () => {
    const [rec] = getRecommendations(
      makeInput({
        goals: onTrackGoals,
        week: { current: 2000, previousUpToDay: 1000 },
      }),
    );

    expect(rec.severity).toBe(RecommendationSeverity.Success);
    expect(rec.title).toBe("Ritmo subiu e fecha a meta");
  });

  it("warns when the week rose but the target is still out of reach", () => {
    const [rec] = getRecommendations(
      makeInput({
        goals: behindGoals,
        week: { current: 2000, previousUpToDay: 1000 },
      }),
    );

    expect(rec.severity).toBe(RecommendationSeverity.Warning);
    expect(rec.title).toBe("Ritmo subiu mas ainda falta");
    expect(rec.message).toContain("14 dias");
  });

  it("warns when the week dipped but the projection still holds", () => {
    const [rec] = getRecommendations(
      makeInput({
        goals: onTrackGoals,
        week: { current: 500, previousUpToDay: 1000 },
      }),
    );

    expect(rec.severity).toBe(RecommendationSeverity.Warning);
    expect(rec.title).toBe("Caiu mas a projeção fecha");
  });

  it("escalates to danger when the week dipped and the target is missed", () => {
    const [rec] = getRecommendations(
      makeInput({
        goals: behindGoals,
        week: { current: 500, previousUpToDay: 1000 },
      }),
    );

    expect(rec.severity).toBe(RecommendationSeverity.Danger);
    expect(rec.title).toBe("Faturamento caiu e não bate a meta");
  });

  it("treats a week within ±5% as stable", () => {
    const [rec] = getRecommendations(
      makeInput({
        goals: onTrackGoals,
        week: { current: 1020, previousUpToDay: 1000 },
      }),
    );

    expect(rec.title).toBe("Ritmo estável e dentro da projeção");
  });

  it("nudges a stable but insufficient pace", () => {
    const [rec] = getRecommendations(
      makeInput({
        goals: behindGoals,
        week: { current: 1020, previousUpToDay: 1000 },
      }),
    );

    expect(rec.severity).toBe(RecommendationSeverity.Warning);
    expect(rec.title).toBe("Ritmo estável mas não é suficiente");
  });

  it("skips pacing advice without a revenue target", () => {
    expect(getRecommendations(makeInput())).toEqual([]);
  });

  it("skips pacing advice on the last day of the month", () => {
    const recs = getRecommendations(
      makeInput({ goals: { ...onTrackGoals, daysRemaining: 0 } }),
    );

    expect(recs).toEqual([]);
  });
});

describe("getRecommendations trend and cash alerts", () => {
  it("flags expenses growing more than 15%", () => {
    const recs = getRecommendations(
      makeInput({
        trends: {
          despesa: { current: 0, previous: 0, change: 30, direction: "up" },
        },
      }),
    );

    expect(recs).toHaveLength(1);
    expect(recs[0].title).toBe("Despesas acima do normal");
    expect(recs[0].message).toContain("30%");
  });

  it("ignores expense growth inside the tolerance", () => {
    const recs = getRecommendations(
      makeInput({
        trends: {
          despesa: { current: 0, previous: 0, change: 10, direction: "up" },
        },
      }),
    );

    expect(recs).toEqual([]);
  });

  it("flags revenue dropping more than 10%", () => {
    const recs = getRecommendations(
      makeInput({
        trends: {
          receita: { current: 0, previous: 0, change: -25, direction: "down" },
        },
      }),
    );

    expect(recs[0].severity).toBe(RecommendationSeverity.Danger);
    expect(recs[0].title).toBe("Receita caiu");
    expect(recs[0].message).toContain("25%");
  });

  it("warns when the balance goes negative within a week", () => {
    const recs = getRecommendations(makeInput({ cash: { daysUntilNegative: 3 } }));

    expect(recs[0].severity).toBe(RecommendationSeverity.Danger);
    expect(recs[0].message).toContain("3 dias");
  });

  it("keeps the day noun singular", () => {
    const recs = getRecommendations(makeInput({ cash: { daysUntilNegative: 1 } }));

    expect(recs[0].message).toContain("1 dia.");
  });

  it("stays quiet when the balance turns negative further out", () => {
    const recs = getRecommendations(
      makeInput({ cash: { daysUntilNegative: 20 } }),
    );

    expect(recs).toEqual([]);
  });

  it("accumulates independent alerts", () => {
    const recs = getRecommendations(
      makeInput({
        trends: {
          despesa: { current: 0, previous: 0, change: 40, direction: "up" },
          receita: { current: 0, previous: 0, change: -30, direction: "down" },
        },
        cash: { daysUntilNegative: 2 },
      }),
    );

    expect(recs.map((r) => r.title)).toEqual([
      "Despesas acima do normal",
      "Receita caiu",
      "Saldo fica negativo em breve",
    ]);
  });
});
