import { describe, expect, it } from "vitest";
import { cashWeekDays, cashWeekPeaks, cashWeekSeries, firstNegative } from "./cashWeek";
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
  it("opens on yesterday and runs five days past today", () => {
    const days = cashWeekDays(forecast, "2026-08-05");

    expect(days.map((d) => d.date)).toEqual([
      "2026-08-04", "2026-08-05", "2026-08-06", "2026-08-07",
      "2026-08-08", "2026-08-09", "2026-08-10",
    ]);
    // Yesterday is the anchor: every other column carries an amber estimate,
    // and one real day is what lets a reader judge whether they look sane.
    expect(days[0].isPast).toBe(true);
    expect(days[1].isToday).toBe(true);
    expect(days.filter((d) => d.isPast)).toHaveLength(1);
  });

  it("opens on today when yesterday belongs to another month", () => {
    // This forecast covers one calendar month, so on the 1st there is no
    // yesterday inside it — the window keeps its shape instead of reaching
    // for a day the series does not have.
    const august = cashWeekDays(forecast, "2026-08-03");

    expect(august[0].date).toBe("2026-08-03");
    expect(august[0].isToday).toBe(true);
  });

  it("adds booked and expected into the day's inflow", () => {
    const today = cashWeekDays(forecast, "2026-08-05")[1];

    // One bar, two parts: together they are what lands, and they stay
    // separable because only one of them is a fact.
    expect(today.totalIn).toBe(125000);
  });

  it("has nothing to show for a month that has closed", () => {
    // Its final days are not "os próximos dias", and showing them under that
    // heading would date-shift the whole card.
    expect(cashWeekDays(forecast, "2026-09-01")).toHaveLength(0);
  });

  it("shows what is left when the month ends inside the window", () => {
    expect(cashWeekDays(forecast, "2026-08-10").map((d) => d.date)).toEqual([
      "2026-08-09", "2026-08-10", "2026-08-11", "2026-08-12",
    ]);
  });
});

describe("cashWeekPeaks", () => {
  it("draws both bars against one ceiling", () => {
    const { maxIn, maxOut, ceiling } = cashWeekPeaks(cashWeekDays(forecast, "2026-08-05"));

    expect(maxIn).toBe(148000);
    expect(maxOut).toBe(850000);
    // Side by side on one baseline, two bars are read as comparable whether or
    // not they are — so the bill and the takings share a scale, and the week's
    // R$ 8.500,00 payroll is what both are measured against.
    expect(ceiling).toBe(850000);
  });

  it("takes the ceiling from the inflow when nothing big falls due", () => {
    const quietBills = cashWeekDays(
      [day("2026-08-05", { expectedIn: 130000 }), day("2026-08-06", { scheduledOut: 4000 })],
      "2026-08-05",
    );

    expect(cashWeekPeaks(quietBills).ceiling).toBe(130000);
  });

  it("is zero throughout when nothing moves", () => {
    const quiet = cashWeekDays([day("2026-08-05"), day("2026-08-06")], "2026-08-05");

    // The card renders an empty state off this rather than dividing by it.
    expect(cashWeekPeaks(quiet)).toEqual({ maxIn: 0, maxOut: 0, ceiling: 0 });
  });
});

describe("firstNegative", () => {
  it("names the first day ahead that goes under water", () => {
    expect(firstNegative(cashWeekDays(forecast, "2026-08-05"))?.date).toBe("2026-08-08");
  });

  it("ignores a dip that already happened", () => {
    // "Fica negativo" is a claim about what is coming. On the 9th the dip of
    // the 8th is in the window as the anchor, and it is not news to act on.
    const days = cashWeekDays(forecast, "2026-08-09");

    expect(days[0].date).toBe("2026-08-08");
    expect(days[0].balance).toBeLessThan(0);
    expect(firstNegative(days)).toBeUndefined();
  });
});

describe("cashWeekSeries", () => {
  it("names the two days a reader locates themselves by", () => {
    const series = cashWeekSeries(cashWeekDays(forecast, "2026-08-05"));

    expect(series.map((d) => d.label)).toEqual([
      "Ontem", "Hoje", "Qui", "Sex", "Sáb", "Dom", "Seg",
    ]);
  });

  it("splits the inflow into booked and expected, in reais", () => {
    const today = cashWeekSeries(cashWeekDays(forecast, "2026-08-05"))[1];

    expect(today["Entrada lançada"]).toBe(300);
    expect(today["Entrada esperada"]).toBe(950);
    expect(today["Saída"]).toBe(150);
  });

  it("leaves a closed day with no expected inflow at all", () => {
    // The backend credits an ordinary day's receipts only from today on, so a
    // past column is green and red and nothing else — which is what makes it
    // readable as the anchor.
    expect(cashWeekSeries(cashWeekDays(forecast, "2026-08-05"))[0]["Entrada esperada"]).toBe(0);
  });
});
