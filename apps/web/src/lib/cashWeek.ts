import type { DayCash } from "@/api/types";

/** How many days ahead the card shows. A week is the unit people plan in. */
export const CASH_WEEK_DAYS = 7;

export interface CashWeekDay extends DayCash {
  /** Everything landing that day: what is booked plus what the weekday brings. */
  totalIn: number;
  isToday: boolean;
}

/**
 * The days still ahead, today included, capped at a week.
 *
 * Today is in because it is a day money can still move on — the same line
 * ADR-017 draws for everything forward-looking. Days already closed are out:
 * this card is for deciding what to do, and there is nothing to decide about
 * Monday on Wednesday.
 *
 * A closed month, or the last day of one, yields an empty list rather than the
 * month's tail. "Os próximos dias" of a month that has ended are not in this
 * forecast, and showing its final days under that heading would date-shift the
 * whole card.
 */
export function cashWeekDays(
  forecast: DayCash[],
  today: string,
  count = CASH_WEEK_DAYS,
): CashWeekDay[] {
  return forecast
    .filter((day) => day.date >= today)
    .slice(0, count)
    .map((day) => ({
      ...day,
      // Booked and expected are one bar because together they are the day's
      // inflow; they stay separable inside it because only one of them is a
      // fact. See the amber convention in lib/chart.
      totalIn: day.scheduledIn + day.expectedIn,
      isToday: day.date === today,
    }));
}

/**
 * The tallest bar each half of the card has to draw. They are returned apart
 * because the card sizes each half by its own peak — which is what keeps a
 * single scale across both halves rather than breaking it; see axisSplit.
 *
 * Both zero when nothing moves all week: the caller renders an empty state
 * rather than dividing by it.
 */
export function cashWeekPeaks(days: CashWeekDay[]): { maxIn: number; maxOut: number } {
  return {
    maxIn: Math.max(0, ...days.map((d) => d.totalIn)),
    maxOut: Math.max(0, ...days.map((d) => d.scheduledOut)),
  };
}

/**
 * The first day of the window whose projected balance is under water, if any.
 * A selection over figures the backend already published — never a recomputed
 * runway. `CashPosition.daysUntilNegative` answers the same question for the
 * whole month; this one is about the week on screen.
 */
export function firstNegative(days: CashWeekDay[]): CashWeekDay | undefined {
  return days.find((d) => d.balance < 0);
}
