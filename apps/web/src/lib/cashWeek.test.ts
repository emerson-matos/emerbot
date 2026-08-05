import { describe, expect, it } from "vitest";
import { cashWeekDays, cashWeekPeaks, firstNegative } from "./cashWeek";
import type { DayCash } from "@/api/types";

const day = (date: string, o: Partial<DayCash> = {}): DayCash => ({
  date,
  balance: 500000,
  scheduledIn: 0,
  scheduledOut: 0,
  expectedIn: 0,
  ...o,
});

/** A month's forecast running either side of the 5th. */
const forecast: DayCash[] = [
  day("2026-08-03"),
  day("2026-08-04"),
  day("2026-08-05", { scheduledIn: 30000, expectedIn: 95000, scheduledOut: 15000 }),
  day("2026-08-06", { expectedIn: 118000 }),
  day("2026-08-07", { expectedIn: 110000, scheduledOut: 62000 }),
  day("2026-08-08", { expectedIn: 148000, scheduledOut: 850000, balance: -40000 }),
  day("2026-08-09", { expectedIn: 60000 }),
  day("2026-08-10", { expectedIn: 122000 }),
  day("2026-08-11", { expectedIn: 119000 }),
  day("2026-08-12", { expectedIn: 121000 }),
];

describe("cashWeekDays", () => {
  it("starts at today and runs a week", () => {
    const days = cashWeekDays(forecast, "2026-08-05");

    // Today is in: it is a day money can still move on, the same line ADR-017
    // draws for everything forward-looking.
    expect(days).toHaveLength(7);
    expect(days[0].date).toBe("2026-08-05");
    expect(days[0].isToday).toBe(true);
    expect(days[6].date).toBe("2026-08-11");
    // Days already closed are out — there is nothing to decide about Monday on
    // Wednesday.
    expect(days.some((d) => d.date < "2026-08-05")).toBe(false);
  });

  it("adds booked and expected into the day's inflow", () => {
    const today = cashWeekDays(forecast, "2026-08-05")[0];

    // One bar, two parts: together they are what lands, and they stay
    // separable because only one of them is a fact.
    expect(today.totalIn).toBe(125000);
  });

  it("has nothing to show for a month that has closed", () => {
    // Its final days are not "os próximos dias", and showing them under that
    // heading would date-shift the whole card.
    expect(cashWeekDays(forecast, "2026-09-01")).toHaveLength(0);
  });

  it("shows what is left when the month ends inside the week", () => {
    expect(cashWeekDays(forecast, "2026-08-10")).toHaveLength(3);
  });
});

describe("cashWeekPeaks", () => {
  it("measures each half against its own tallest bar", () => {
    const { maxIn, maxOut } = cashWeekPeaks(cashWeekDays(forecast, "2026-08-05"));

    expect(maxIn).toBe(148000);
    expect(maxOut).toBe(850000);
  });

  it("is zero on both halves when nothing moves", () => {
    const quiet = cashWeekDays([day("2026-08-05"), day("2026-08-06")], "2026-08-05");

    // The card renders an empty state off this rather than dividing by it.
    expect(cashWeekPeaks(quiet)).toEqual({ maxIn: 0, maxOut: 0 });
  });
});

describe("firstNegative", () => {
  it("names the first day the balance goes under water", () => {
    expect(firstNegative(cashWeekDays(forecast, "2026-08-05"))?.date).toBe("2026-08-08");
  });

  it("is undefined when the week holds", () => {
    expect(firstNegative(cashWeekDays(forecast, "2026-08-09"))).toBeUndefined();
  });
});
