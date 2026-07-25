import { describe, expect, it } from "vitest";
import { makeSummary } from "@/test/factories";
import { getTrends } from "./trends";

describe("getTrends", () => {
  it("computes percentage change per metric against the previous month", () => {
    const trends = getTrends(
      makeSummary({ TotalIncome: 15000, TotalExpense: 5000, Balance: 10000 }),
      makeSummary({ TotalIncome: 10000, TotalExpense: 4000, Balance: 6000 }),
    );

    expect(trends.receita).toEqual({
      current: 15000,
      previous: 10000,
      change: 50,
      direction: "up",
    });
    expect(trends.despesa.change).toBe(25);
    expect(trends.resultado.change).toBe(67);
  });

  it("treats a missing previous month as zero", () => {
    const trends = getTrends(
      makeSummary({ TotalIncome: 15000, TotalExpense: 0, Balance: 15000 }),
      null,
    );

    expect(trends.receita).toEqual({
      current: 15000,
      previous: 0,
      change: 100,
      direction: "up",
    });
    // Nothing this month and nothing last month is flat, not a 100% jump.
    expect(trends.despesa).toEqual({
      current: 0,
      previous: 0,
      change: 0,
      direction: "stable",
    });
  });

  it("reports changes within ±2% as stable", () => {
    const trends = getTrends(
      makeSummary({ TotalIncome: 10100 }),
      makeSummary({ TotalIncome: 10000 }),
    );

    expect(trends.receita.change).toBe(1);
    expect(trends.receita.direction).toBe("stable");
  });

  it("reports a drop beyond the stable band as down", () => {
    const trends = getTrends(
      makeSummary({ TotalIncome: 8000 }),
      makeSummary({ TotalIncome: 10000 }),
    );

    expect(trends.receita.change).toBe(-20);
    expect(trends.receita.direction).toBe("down");
  });

  // Balance can go negative, so the denominator is |previous| — otherwise
  // recovering from -100 to +50 would read as a decline.
  it("uses the absolute previous value so recoveries read as growth", () => {
    const trends = getTrends(
      makeSummary({ Balance: 5000 }),
      makeSummary({ Balance: -10000 }),
    );

    expect(trends.resultado.change).toBe(150);
    expect(trends.resultado.direction).toBe("up");
  });
});
