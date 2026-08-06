import type { DayCash } from "@/api/types";

/**
 * The window: ontem, hoje, amanhã. Three columns, the same three days the rest
 * of the page is built around — the anchor that has closed, the day being
 * traded, and the one you can still do something about.
 *
 * Yesterday earns its column by being the anchor. The other two carry an amber
 * estimate, and a reader has no way to judge whether those estimates look sane
 * without one real day beside them. It is deliberately one day back and not
 * three: this card is for deciding what to do next, and the past is context,
 * not the subject.
 *
 * It ran five days ahead, and seven pairs of bars in the width of a phone are
 * about eight points each, under labels that have to alternate to fit, behind a
 * tooltip that covers the whole card at a touch. The window is not information
 * if it cannot be read. It was briefly seven on a desktop and three on a phone,
 * which cost more than it bought: the footer quotes the balance on the window's
 * last day, so the same card announced "Saldo em 06/08" on a phone and "Saldo em
 * 10/08" on a laptop — one card, one number, is worth more than two columns.
 *
 * Nothing is hidden by this. Whether the *month* runs dry is a different
 * question and "Posição de caixa", directly below, answers it for the whole
 * month.
 */
export const CASH_WEEK_BACK = 1;
export const CASH_WEEK_AHEAD = 1;

export interface CashWeekDay extends DayCash {
  /** Everything landing that day: what is booked plus what the weekday brings. */
  totalIn: number;
  isToday: boolean;
  /**
   * A day that has already closed. Its figures carry no estimate at all — the
   * backend credits an ordinary day's receipts only from today on — so what is
   * drawn is what was *booked* to move that day.
   *
   * Booked, not banked: the curve buckets every entry by its due date whatever
   * its payment status (finance.EffectiveDate), so a receivable that came due
   * yesterday and was never paid still sits on yesterday. The column is an
   * anchor, not a receipt.
   */
  isPast: boolean;
}

/**
 * Slices the month's forecast around today.
 *
 * Anchored on today rather than filtered by a date range, so the window keeps
 * its shape at the edges of the month: on the 1st there is no yesterday inside
 * this forecast — it belongs to the previous month, which this series does not
 * cover — and the card simply opens on today.
 *
 * A month with no day left yields nothing rather than its own tail. "Os
 * próximos dias" of a month that has ended are not in this forecast, and
 * showing its final days under that heading would date-shift the whole card.
 */
export function cashWeekDays(
  forecast: DayCash[],
  today: string,
  back = CASH_WEEK_BACK,
  ahead = CASH_WEEK_AHEAD,
): CashWeekDay[] {
  const start = forecast.findIndex((day) => day.date >= today);
  if (start === -1) return [];

  return forecast
    .slice(Math.max(0, start - back), start + 1 + ahead)
    .map((day) => ({
      ...day,
      // Booked and expected are one bar because together they are the day's
      // inflow; they stay separable inside it because only one of them is a
      // fact. See the amber convention in lib/chart.
      totalIn: day.scheduledIn + day.expectedIn,
      isToday: day.date === today,
      isPast: day.date < today,
    }));
}

/**
 * The tallest inflow and the tallest outflow of the window, and the larger of
 * the two.
 *
 * Recharts sizes the axis itself, so this is not the drawing scale — it is how
 * the card tells a week where nothing moves from one where something does, and
 * an all-zero week renders an empty state instead of seven flat columns under
 * an axis running to R$ 0.
 */
export function cashWeekPeaks(
  days: CashWeekDay[],
): { maxIn: number; maxOut: number; ceiling: number } {
  const maxIn = Math.max(0, ...days.map((d) => d.totalIn));
  const maxOut = Math.max(0, ...days.map((d) => d.scheduledOut));
  return { maxIn, maxOut, ceiling: Math.max(maxIn, maxOut) };
}

/**
 * The first day still ahead whose projected balance is under water, if any.
 * A selection over figures the backend already published — never a recomputed
 * runway. `CashPosition.daysUntilNegative` answers the same question for the
 * whole month; this one is about the week on screen.
 *
 * Days already closed are skipped: "fica negativo" is a claim about what is
 * coming, and a balance that dipped yesterday is not news the card can act on.
 */
export function firstNegative(days: CashWeekDay[]): CashWeekDay | undefined {
  return days.find((d) => !d.isPast && d.balance < 0);
}

/** One column of the chart. Amounts in reais — Recharts reads these by name. */
export interface CashWeekBar {
  date: string;
  /** The axis label: "Ontem", "Hoje", or the weekday. */
  label: string;
  /** The tooltip heading, where there is room for the full date. */
  full: string;
  isToday: boolean;
  isPast: boolean;
  "Entrada lançada": number;
  "Entrada esperada": number;
  "Saída": number;
  /**
   * The balance at the end of that day — the question the bars alone cannot
   * answer. "Entra R$ 1.200,00 e saem R$ 8.500,00 no sábado" is only alarming
   * if there is nothing in the account to absorb it, and this is the line that
   * says so.
   *
   * Projected end to end, which is why it is named that way: the days ahead
   * are credited with an ordinary day's receipts, and even the anchor's figure
   * counts a receivable that came due and may not have been paid.
   */
  "Saldo projetado": number;
}

const WEEKDAY_SHORT = ["Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"];
const WEEKDAY_FULL = [
  "domingo", "segunda-feira", "terça-feira", "quarta-feira",
  "quinta-feira", "sexta-feira", "sábado",
];

/**
 * Turns the window into chart rows. The series names are the legend, so they
 * are the words a reader sees: a key of "in" would have to be translated
 * somewhere else and then kept in step.
 *
 * The three days a reader locates themselves by are named rather than numbered —
 * "Ontem", "Hoje" and "Amanhã" beside the remaining weekdays is a week you can
 * read without counting. Tomorrow earns its name twice over on a phone, where
 * the window is exactly those three days (CASH_WEEK_AHEAD_NARROW): a card whose
 * whole subject is ontem/hoje/amanhã must not label a third of itself "Qui".
 */
export function cashWeekSeries(days: CashWeekDay[]): CashWeekBar[] {
  const todayIndex = days.findIndex((d) => d.isToday);
  return days.map((day, i) => {
    const weekday = new Date(`${day.date}T12:00:00`).getDay();
    const isTomorrow = todayIndex !== -1 && i === todayIndex + 1;
    return {
      date: day.date,
      label: day.isToday
        ? "Hoje"
        : day.isPast
          ? "Ontem"
          : isTomorrow
            ? "Amanhã"
            : WEEKDAY_SHORT[weekday],
      full: `${WEEKDAY_FULL[weekday]}, ${day.date.slice(8)}/${day.date.slice(5, 7)}`,
      isToday: day.isToday,
      isPast: day.isPast,
      "Entrada lançada": day.scheduledIn / 100,
      "Entrada esperada": day.expectedIn / 100,
      "Saída": day.scheduledOut / 100,
      "Saldo projetado": day.balance / 100,
    };
  });
}
