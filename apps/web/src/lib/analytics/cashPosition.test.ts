import { describe, expect, it } from "vitest";
import { makeCashFlowPoint } from "@/test/factories";
import { getCashPosition } from "./cashPosition";

// "Today" is the user's local calendar day, so `now` is built from local
// fields — 5 Feb 2026, midday.
const now = new Date(2026, 1, 5, 12, 0, 0);

describe("getCashPosition", () => {
  it("returns a zeroed position when there are no projections", () => {
    expect(getCashPosition([], now)).toEqual({
      currentBalance: 0,
      endOfMonthProjection: 0,
      daysUntilNegative: null,
      lowestProjected: 0,
      lowestProjectedDate: "2026-02-05",
    });
  });

  it("reads today's balance and projects the last point as end of month", () => {
    const position = getCashPosition(
      [
        makeCashFlowPoint({ Date: "2026-02-05", RunningBalance: 5000 }),
        makeCashFlowPoint({ Date: "2026-02-06", RunningBalance: 4000 }),
        makeCashFlowPoint({ Date: "2026-02-28", RunningBalance: 7000 }),
      ],
      now,
    );

    expect(position.currentBalance).toBe(5000);
    expect(position.endOfMonthProjection).toBe(7000);
    expect(position.daysUntilNegative).toBeNull();
  });

  it("tracks the lowest projected balance and the day it happens", () => {
    const position = getCashPosition(
      [
        makeCashFlowPoint({ Date: "2026-02-05", RunningBalance: 5000 }),
        makeCashFlowPoint({ Date: "2026-02-10", RunningBalance: 800 }),
        makeCashFlowPoint({ Date: "2026-02-28", RunningBalance: 3000 }),
      ],
      now,
    );

    expect(position.lowestProjected).toBe(800);
    expect(position.lowestProjectedDate).toBe("2026-02-10");
  });

  it("counts the days until the balance first goes negative", () => {
    const position = getCashPosition(
      [
        makeCashFlowPoint({ Date: "2026-02-05", RunningBalance: 5000 }),
        makeCashFlowPoint({ Date: "2026-02-08", RunningBalance: -200 }),
        makeCashFlowPoint({ Date: "2026-02-12", RunningBalance: -900 }),
      ],
      now,
    );

    // First negative day only — a later, deeper dip must not overwrite it.
    expect(position.daysUntilNegative).toBe(3);
    expect(position.lowestProjected).toBe(-900);
    expect(position.lowestProjectedDate).toBe("2026-02-12");
  });

  it("ignores negative balances that are already in the past", () => {
    const position = getCashPosition(
      [
        makeCashFlowPoint({ Date: "2026-02-01", RunningBalance: -500 }),
        makeCashFlowPoint({ Date: "2026-02-05", RunningBalance: 5000 }),
      ],
      now,
    );

    expect(position.daysUntilNegative).toBeNull();
  });

  // Late evening in a UTC- timezone is already tomorrow in UTC, so keying
  // "today" off toISOString() used to drop the current balance to zero for
  // anyone checking the dashboard after dinner.
  it("still finds today's balance late in the evening", () => {
    const position = getCashPosition(
      [
        makeCashFlowPoint({ Date: "2026-02-05", RunningBalance: 5000 }),
        makeCashFlowPoint({ Date: "2026-02-06", RunningBalance: 4000 }),
      ],
      new Date(2026, 1, 5, 23, 30, 0),
    );

    expect(position.currentBalance).toBe(5000);
  });

  it("counts calendar days regardless of the time of day", () => {
    const points = [
      makeCashFlowPoint({ Date: "2026-02-05", RunningBalance: 5000 }),
      makeCashFlowPoint({ Date: "2026-02-08", RunningBalance: -200 }),
    ];

    const morning = getCashPosition(points, new Date(2026, 1, 5, 8, 0, 0));
    const evening = getCashPosition(points, new Date(2026, 1, 5, 22, 0, 0));

    expect(morning.daysUntilNegative).toBe(3);
    expect(evening.daysUntilNegative).toBe(3);
  });

  it("falls back to zero when no point matches today", () => {
    const position = getCashPosition(
      [makeCashFlowPoint({ Date: "2026-02-20", RunningBalance: 900 })],
      now,
    );

    expect(position.currentBalance).toBe(0);
    expect(position.endOfMonthProjection).toBe(900);
  });
});
