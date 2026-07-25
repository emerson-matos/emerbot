import { describe, expect, it } from "vitest";
import { makeEntry } from "@/test/factories";
import { getWeekdayStats } from "./weekday";

// 2026-02-05 is a Thursday. Dates are parsed at local noon by the helper, so
// building `now` locally keeps the weekday stable in any timezone.
const now = new Date(2026, 1, 5, 12, 0, 0);

describe("getWeekdayStats", () => {
  it("returns all seven weekdays even with no entries", () => {
    const stats = getWeekdayStats([], now);

    expect(stats).toHaveLength(7);
    expect(stats.map((s) => s.label)).toEqual([
      "Dom",
      "Seg",
      "Ter",
      "Qua",
      "Qui",
      "Sex",
      "Sáb",
    ]);
    expect(stats.every((s) => s.total === 0 && s.avg === 0)).toBe(true);
  });

  it("flags the current weekday", () => {
    const stats = getWeekdayStats([], now);

    expect(stats.filter((s) => s.isToday)).toHaveLength(1);
    expect(stats.find((s) => s.isToday)?.label).toBe("Qui");
  });

  it("totals counter sales per weekday", () => {
    // 2026-02-05 and 2026-02-12 are both Thursdays.
    const stats = getWeekdayStats(
      [
        makeEntry({ TransactionDate: "2026-02-05", Amount: 1000 }),
        makeEntry({ TransactionDate: "2026-02-12", Amount: 3000 }),
      ],
      now,
    );
    const thursday = stats[4];

    expect(thursday.total).toBe(4000);
    expect(thursday.count).toBe(2);
    expect(thursday.avg).toBe(2000);
  });

  // The average is per *day*, not per entry — two sales on one Thursday is
  // still a single Thursday's worth of revenue.
  it("averages over distinct dates, not entry count", () => {
    const stats = getWeekdayStats(
      [
        makeEntry({ TransactionDate: "2026-02-05", Amount: 1000 }),
        makeEntry({ TransactionDate: "2026-02-05", Amount: 3000 }),
      ],
      now,
    );
    const thursday = stats[4];

    expect(thursday.total).toBe(4000);
    expect(thursday.count).toBe(1);
    expect(thursday.avg).toBe(4000);
  });

  it("counts only venda_balcao income", () => {
    const stats = getWeekdayStats(
      [
        makeEntry({ TransactionDate: "2026-02-05", Amount: 1000 }),
        makeEntry({
          TransactionDate: "2026-02-05",
          Amount: 9999,
          Type: "expense",
          Category: "aluguel",
        }),
        makeEntry({
          TransactionDate: "2026-02-05",
          Amount: 5555,
          Type: "income",
          Category: "outros",
        }),
      ],
      now,
    );

    expect(stats[4].total).toBe(1000);
  });

  it("accepts timestamps by using only the date part", () => {
    const stats = getWeekdayStats(
      [makeEntry({ TransactionDate: "2026-02-05T23:45:00Z", Amount: 2500 })],
      now,
    );

    expect(stats[4].total).toBe(2500);
  });
});
