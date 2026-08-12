import { CognitoAuthError } from "./cognito";
import type { CognitoAuthResult } from "./cognito";

export { CognitoAuthError };
export type { CognitoAuthResult };

// Display profile derived from the Cognito ID token.
export interface UserProfile {
  name?: string;
  email?: string;
  phone?: string;
}

export interface Entry {
  UserID: string;
  EntryID: string;
  TransactionDate: string;
  Amount: number;
  Category: string;
  Type: "expense" | "income";
  Description: string;
  DueDate: string | null;
  PaymentStatus: "pending" | "paid";
  PaymentDate: string | null;
  /**
   * How it was paid or received, in the user's own words ("pix", "dinheiro") —
   * free text, and empty far more often than not (ADR-026). It is a fact about
   * the settlement, so the API clears it whenever an entry goes back to
   * pending. Nothing groups or totals by it.
   */
  PaymentMethod?: string;
  Supplier: string;
  Source: string;
  /**
   * Where the money came from, for income entries; empty on expenses. Only
   * "venda" counts as faturamento. Undefined on entries written before the
   * field existed — see the shim in lib/notifications.ts.
   */
  Origin?: IncomeOrigin | "";
  /**
   * Set on occurrences created together as one series, empty on a one-off
   * entry. Editing one instalment of a series edits the whole agreement — see
   * the scope option on api.entries.update.
   */
  RecurrenceID?: string;
  /** 1-based position within the series. */
  RecurrenceIndex?: number;
  RecurrenceTotal?: number;
}

/** Only IncomeOrigin.Venda counts toward faturamento; see domain.IsRevenue. */
export const IncomeOrigin = {
  Venda: "venda",
  RecebimentoCliente: "recebimento_cliente",
  Emprestimo: "emprestimo",
  AporteSocio: "aporte_socio",
  ReceitaFinanceira: "receita_financeira",
  Restituicao: "restituicao",
  Outros: "outros",
} as const;
export type IncomeOrigin = (typeof IncomeOrigin)[keyof typeof IncomeOrigin];

/** pt-BR labels, mirroring domain.OriginLabels. */
export const incomeOriginLabels: Record<IncomeOrigin, string> = {
  venda: "Venda",
  recebimento_cliente: "Recebimento de cliente",
  emprestimo: "Empréstimo",
  aporte_socio: "Aporte de sócio",
  receita_financeira: "Receita financeira",
  restituicao: "Restituição",
  outros: "Outros",
};

export interface CreateEntryInput {
  date: string;
  amount: number;
  category: string;
  type: "expense" | "income";
  description: string;
  supplier?: string;
  /** Defaults to "venda" server-side when omitted on an income entry. */
  origin?: IncomeOrigin;
}

/**
 * A patch, not a whole entry: an omitted key keeps what is stored, and an
 * explicitly empty description, supplier or due_date clears it. Send only what
 * actually changed — a key you did not mean to touch is an edit.
 */
export type UpdateEntryInput = Partial<CreateEntryInput> & {
  payment_status?: "pending" | "paid";
  /** "" clears the due date. */
  due_date?: string;
  supplier?: string;
  /** Free text, "" clears it; sent beside payment_status when quitting. */
  payment_method?: string;
};

/**
 * The three inflow figures answer three different questions and are bucketed by
 * three different dates — see packages/finance.MonthlySummary and ADR-016.
 * Collapsing any two of them back together is how a loan ended up counted as
 * business growth.
 */
export interface MonthlySummary {
  Month: string;
  /** FATURAMENTO: what was sold, by the day of each sale, paid or not. */
  TotalRevenue: number;
  /** ENTRADAS DE CAIXA: money that actually arrived, whatever its origin. */
  TotalCashIn: number;
  /** Money that actually left. Pairs with TotalCashIn for real cash movement. */
  TotalCashOut: number;
  /** Every inflow by effective date, pending receivables included. */
  TotalExpectedIn: number;
  TotalExpense: number;
  /** TotalExpectedIn - TotalExpense: a forecast of the month, not a cash position. */
  ExpectedBalance: number;
}

export interface CategorySummary {
  Category: string;
  Type: "expense" | "income";
  Total: number;
  Count: number;
}

export interface CashFlowPoint {
  Date: string;
  ProjectedIncome: number;
  ProjectedExpense: number;
  RunningBalance: number;
}

export interface Goal {
  UserID: string;
  Month: string;
  /** A faturamento target: measured against sales only, never cash in. */
  RevenueTarget: number;
  ExpenseTarget: number;
}

// Where the daily WhatsApp messages go. There is nothing to configure — they
// always go out — so this is the delivery address and not a set of preferences.
export interface NotificationPrefs {
  phone: string;
}

export interface Category {
  UserID: string;
  Slug: string;
  Label: string;
  Type: "expense" | "income";
  Default: boolean;
}

// --- Monthly analysis (GET /analysis/monthly) ---
//
// The whole analysis is computed in Go (packages/finance/analytics) so the
// dashboard, the WhatsApp digest and the AI bot all describe a month the same
// way. These types mirror that package's JSON — amounts are centavos.

export type YearMonth = `${number}-${number}`;

export const FinancialHealthStatus = {
  Boa: "boa",
  Atencao: "atencao",
  Critico: "critico",
} as const;
export type FinancialHealthStatus =
  (typeof FinancialHealthStatus)[keyof typeof FinancialHealthStatus];

export const InsightType = {
  ExpenseGrowth: "expense_growth",
  RevenueDrop: "revenue_drop",
  LowCashFlow: "low_cash_flow",
  GoalBehind: "goal_behind",
  GoodPerformance: "good_performance",
  WeeklyImprovement: "weekly_improvement",
  WeeklyDecline: "weekly_decline",
  GoalOnTrack: "goal_on_track",
} as const;
export type InsightType = (typeof InsightType)[keyof typeof InsightType];

export const InsightSeverity = {
  Info: "info",
  Warning: "warning",
  Critical: "critical",
} as const;
export type InsightSeverity =
  (typeof InsightSeverity)[keyof typeof InsightSeverity];

export interface Insight {
  type: InsightType;
  severity: InsightSeverity;
  title: string;
  description: string;
  value?: number;
}

export interface FinancialHealth {
  status: FinancialHealthStatus;
  /**
   * The 0–100 number shown next to the traffic light. Computed in Go from the
   * insight severities so it can never disagree with the status beside it —
   * this page used to derive it here as the share of insights that were
   * informational, which moved the score whenever a rule fired or stopped
   * firing, with nothing financial having changed.
   */
  score: number;
  messages: Insight[];
}

export type TrendDirection = "up" | "down" | "stable";

export interface MonthTrend {
  current: number;
  previous: number;
  change: number;
  direction: TrendDirection;
}

/**
 * Each metric against the previous month, both sides measured over the same
 * *finished* days. The window is `Analysis['period']` — label every percentage
 * with it rather than presenting it as a whole month.
 */
export interface Trends {
  faturamento: MonthTrend;
  despesa: MonthTrend;
  resultado: MonthTrend;
}

/**
 * How far into the analysed month this payload stands. Nothing retrospective
 * in the analysis counts today (it is still being traded), and nothing
 * forward-looking writes today off (it can still be sold on).
 */
export interface Period {
  /**
   * Last day of the month with complete data — yesterday while the month runs,
   * its last day once closed. 0 on the first day of a month: nothing has
   * finished, every retrospective figure is empty, and the UI must say the
   * month is starting rather than draw a fall to zero.
   */
  throughDay: number;
  /**
   * Last day the *month-over-month* figures were measured through, on both
   * sides. It closes on whole weeks, so it trails `throughDay` by up to six
   * days and is 0 for the whole opening week: the first N days of two months
   * hold different weekdays unless N is a multiple of seven, and a percentage
   * over mismatched weekdays measures the calendar rather than the pharmacy.
   *
   * Every "vs mês passado" label belongs to this window and no other. 0 means
   * there is no comparison to draw — say the week has not closed, never render
   * a fall to zero. Figures about this month alone keep using `throughDay`.
   */
  comparableThroughDay: number;
  /** Days still to trade, today included. 0 for a closed month. */
  daysRemaining: number;
  daysTotal: number;
  /** True when the analysed month is the one we are in. */
  inProgress: boolean;
}

export interface WeekdayStat {
  /**
   * time.Weekday / getDay(): 0 = Sunday. The only identity this carries — the
   * Portuguese label it used to ship beside it was fed back into a lookup
   * table here, so the backend's spelling decided what the browser printed.
   * Name it with lib/weekdays.
   */
  day: number;
  avg: number;
  count: number;
  isToday: boolean;
  basis: ProjectionBasis;
}

export interface DayHighlight {
  date: string;
  label: string;
  amount: number;
}

export interface CashOutDay {
  date: string;
  total: number;
  items: { category: string; amount: number; count: number }[];
}

/**
 * One category's share of a total: of the month's expenses in
 * `expenseComposition`, of its faturamento in `revenueComposition`. Same shape
 * and same rendering — what differs is which entries went into the fold, which
 * is the backend's business.
 */
export interface CategoryComposition {
  categoryId: string;
  categoryName: string;
  amount: number;
  percentage: number;
}

/**
 * revenueActual is faturamento (sales only), which is what the target is set
 * against — a month must not read as "goal reached" because a loan came in.
 */
export interface GoalProgress {
  revenueTarget: number;
  revenueActual: number;
  revenuePct: number;
  expenseTarget: number;
  expenseActual: number;
  expensePct: number;
  daysRemaining: number;
  daysTotal: number;
}

export interface MonthlySnapshot {
  month: YearMonth;
  label: string;
  // Faturamento, the same basis as revenueTarget, so a bar and its target line
  // measure the same thing. They used to disagree: this read the broad income
  // total against a sales target.
  revenue: number;
  revenueTarget: number | null;
  expense: number;
  expenseTarget: number | null;
}

export interface WeekComparison {
  /** Monday through today, and the whole of last week — totals for the chart. */
  current: number;
  previous: number;
  pace: WeekPace;
  projectedWeekly: number;
  monthlyTarget: number;
  /**
   * The week so far, one entry per day that has happened, as getDay() numbers
   * (0 = Sunday). Name them with lib/weekdays — they used to arrive as
   * abbreviated Portuguese from Go, which made the axis depend on the
   * backend's spelling.
   */
  days: number[];
}

/**
 * This week against last week over the same *finished* days — Monday through
 * yesterday on both sides. The only week-over-week reading to render as a
 * percentage: comparing a morning still being traded against a full day
 * reported "ritmo caiu" every morning, and 100% down on a Monday.
 */
export interface WeekPace {
  current: number;
  previous: number;
  /** Finished days of this week both sides cover; 0 on a Monday. */
  days: number;
  /**
   * The backend's own verdict on the pace, in the same shape as MonthTrend, and
   * the only one to render. This card used to divide `current` by `previous`
   * itself, without the dead band the health insight applies, so it printed
   * "↑ 3% vs semana anterior" for a week the insights beside it called flat.
   *
   * A `previous` of 0 still reads as a flat 100% up: check `previous` and say
   * "sem base" rather than quoting it.
   */
  change: number;
  direction: TrendDirection;
}

/**
 * Where the month lands and what it would take to close the gap to the
 * income goal, in faturamento (money earned by selling something — see
 * isFaturamento in packages/finance/analytics).
 *
 * All of this used to be worked out in the browser from the weekday averages,
 * while the backend told the WhatsApp bot a different projection and priced
 * the daily ask off real income rather than off the projection. The page
 * therefore printed two different "necessário por dia" figures within one
 * screen. It is computed once in Go now; render these fields, don't re-derive
 * them.
 */
/**
 * How much trading the projection stands on. The days still to come are priced
 * from a trailing eight-week window, and a window with barely anything in it
 * must not read as confidently as one built on two months of sales.
 */
export const ProjectionBasis = {
  /** A week or more of trading days in the window — the ordinary case. */
  Window: "janela",
  /** Fewer than seven days traded in eight weeks; the figure will still move. */
  Partial: "parcial",
  /** Nothing traded in the window at all. */
  None: "sem_base",
  /**
   * The month has ended: nothing was estimated and `projected` is its own
   * faturamento. Carries no caption — the one figure here that is not a
   * forecast must not be captioned as one.
   */
  Closed: "fechado",
} as const;
export type ProjectionBasis =
  (typeof ProjectionBasis)[keyof typeof ProjectionBasis];

export const DayTargetScale = {
  /**
   * Below the weekday average. A *target* can no longer be — the plan is floored
   * at the weekday's own rhythm — so this now only grades what a closed day
   * sold.
   */
  Below: "below",
  OnTrack: "on_track",
  Above: "above",
} as const;
export type DayTargetScale =
  (typeof DayTargetScale)[keyof typeof DayTargetScale];

/**
 * What a day's ask is made of. The floor at the weekday average makes different
 * situations produce the same amount: a Wednesday asked for exactly its average
 * is a month in step when the gap works out at the rhythm, and a month already
 * past its goal when the gap works out at nothing. One is "mantenha o ritmo",
 * the other is "você já chegou lá", and the numbers no longer tell them apart.
 */
export const DayTargetSource = {
  /** Above the average: the month is behind and this day carries its share. */
  Gap: "plano",
  /** The floor, with the month still chasing its goal. `target === historical`. */
  Average: "media",
  /** The floor, with the goal already reached. Same figures, different news. */
  GoalMet: "meta_batida",
} as const;
export type DayTargetSource =
  (typeof DayTargetSource)[keyof typeof DayTargetSource];

export const DayTargetState = {
  /** A real ask: the amounts below are meaningful. */
  OK: "ok",
  /** The month has ended — there is no "today" inside it to sell on. */
  ClosedMonth: "mes_fechado",
  /** No revenue goal set, so no share of one to ask for today. */
  NoGoal: "sem_meta",
  /**
   * The goal is already reached. No longer produced: a month past its goal is
   * asked for its ordinary rhythm rather than for nothing, so the day comes
   * back `ok` with a `meta_batida` *source*. Kept because stored snapshots
   * still carry it.
   */
  GoalMet: "meta_batida",
  /** Nothing in the trailing window to price any remaining day from. */
  NoHistory: "sem_historico",
  /** A weekday that never traded: the pharmacy does not open on it. */
  ClosedWeekday: "dia_sem_movimento",
  /**
   * A day past the analysed month's last one. It belongs to a month with its
   * own goal and its own gap, none of which are knowable from here.
   */
  MonthOver: "mes_acaba_hoje",
  /**
   * A date in a month that has not started. Told apart from a closed one
   * because the backend's clock cannot: both report `inProgress: false`.
   */
  FutureMonth: "mes_futuro",
  /**
   * A day that has finished. Not a gap in the data and not bad news — the day
   * has a result instead of a target, and `realized` carries it.
   */
  ClosedDay: "dia_fechado",
} as const;
export type DayTargetState =
  (typeof DayTargetState)[keyof typeof DayTargetState];

/**
 * What kind of figure a day carries: something that happened, something
 * happening, or something being asked for. Render it — a target and a result
 * read identically without it.
 */
export const DayBasis = {
  /** A day that closed. `realized` is fact; there is no `target`. */
  Realized: "realizado",
  /** Today: an ask that still stands, beside what the till has taken. */
  InProgress: "em_curso",
  /** A day ahead. Its `target` assumes every day before it lands on its own. */
  Projected: "projetado",
} as const;
export type DayBasis = (typeof DayBasis)[keyof typeof DayBasis];

export interface DayTarget {
  /**
   * Whether there is an ask today and, when there is not, why. Only `ok`
   * leaves the amounts below meaningful — every other state zeroes them.
   *
   * This replaced a bare `valid: boolean`, which could not tell "a meta já foi
   * batida" from "não há histórico": rendering both as the same blank made the
   * good news indistinguishable from the gap in the data.
   */
  state: DayTargetState;
  /**
   * Whether these figures are a result, a day in progress, or a bet. Consumers
   * must render it; `realizado` and `projetado` are not the same claim.
   */
  basis: DayBasis;
  /**
   * The calendar day the ask is for, "YYYY-MM-DD". Absent when there is no day
   * to name — a closed month has no today, and a date outside the analysed
   * month is not this month's to price.
   */
  date?: string;
  /**
   * What the day actually sold. The whole point of a closed day and the
   * missing half of an open one: `target` is a whole-day figure measured from
   * the morning, so without this there is no saying whether it was met.
   */
  realized: number;
  /** time.Weekday / getDay(): 0 = Sunday. Name it with lib/weekdays. */
  day: number;
  /** What this weekday usually brings, over a whole day. */
  historical: number;
  /**
   * What it has to bring to keep the month on its goal — never less than
   * `historical`. The ask is the greater of the gap's share and the weekday's
   * own rhythm: a target is a floor under the day, not a ceiling over it.
   */
  target: number;
  /**
   * `target` minus `historical`, so never negative on a day still ahead. On a
   * closed day it is `realized` against `historical` and may be either way —
   * that is a result, not an ask.
   */
  delta: number;
  deltaPercent: number;
  /** Never below 1: 1.08 asks for 8% above the usual rhythm, 1 asks for it exactly. */
  factor: number;
  status: DayTargetScale;
  /** Which of the three the ask is. Absent on a day with no ask at all. */
  source?: DayTargetSource;
}

export const ProjectionStatus = {
  /** No goal to cover, so no verdict — not the same as falling short of one. */
  NoTarget: "",
  Success: "success",
  Warning: "warning",
  Danger: "danger",
} as const;
export type ProjectionStatus =
  (typeof ProjectionStatus)[keyof typeof ProjectionStatus];

export interface Projection {
  actual: number;
  remaining: number;
  projected: number;
  target: number;
  /**
   * Where the month lands if every day still to come sells what `plan` asks of
   * it, as against `projected` — where it lands if they merely trade as usual.
   * Never below `projected`: with the ask floored at the weekday average, the
   * plan cannot close the month worse than doing nothing differently would.
   */
  plannedClose: number;
  /** What the projection still misses the target by; 0 once it clears it. */
  gap: number;
  onTrack: boolean;
  daysRemaining: number;
  /**
   * Render this as a qualifier; do not re-derive one from the amounts. The
   * backend is the only thing that knows how wide the window was and how much
   * of it traded.
   */
  basis: ProjectionBasis;
  /**
   * `projected / target`: 1.10 overshoots by 10%, 0.80 lands 20% short. 0 when
   * there is no target. Render these; do not divide or re-derive a verdict here
   * — a percentage rounded in the browser beside a colour read off `onTrack` is
   * how one screen showed "97% da meta" in red under a green "Ritmo suficiente".
   */
  coverage: number;
  status: ProjectionStatus;
  /**
   * Today's revenue target derived from historical weekday averages, with what
   * the day has sold so far beside it. It is the only day named here: every
   * other one is a parameter of the backend's day tool, because a field per
   * day is not an answer to "e no sábado?".
   */
  todayTarget: DayTarget;
  /**
   * How the gap is spread over the days the month has left: each of them asked
   * for its own weekday average times `factor`, and never for less than that
   * average. Deliberately not derived from `todayTarget` — on a Sunday the
   * pharmacy does not open, that has no factor at all while the plan behind it
   * still prices every other day.
   */
  plan: Plan;
}

/** ADR-030's observational forecast candidate; it never drives the official projection. */
export interface ProjectionExperiment {
  current: {
    available: boolean;
    official: number;
    experimental: number;
    recentFactor: number;
    observations: number;
  };
  backtest: {
    samples: Array<{
      month: string;
      cutoffDay: number;
      actualClose: number;
      official: number;
      experimental: number;
    }>;
    officialMae: number;
    regimeMae: number;
    officialWins: number;
    regimeWins: number;
    weekdayErrors: Array<{
      day: number;
      mae: number;
      observations: number;
    }>;
  };
}

export interface Plan {
  state: DayTargetState;
  /** Never below 1. Meaningless unless `state` is `ok`. */
  factor: number;
  /**
   * What the gap alone would have asked for, before the floor: above 1 the
   * month is behind, at or below 1 it is in step or ahead, 0 means the goal is
   * already met. It is the only thing that separates the months a floored ask
   * cannot tell apart — don't infer "adiantado" from `target === historical`,
   * which is also what a month in perfect step looks like.
   */
  gapFactor: number;
}

export const RecommendationSeverity = {
  Success: "success",
  Warning: "warning",
  Danger: "danger",
} as const;
export type RecommendationSeverity =
  (typeof RecommendationSeverity)[keyof typeof RecommendationSeverity];

export interface Recommendation {
  severity: RecommendationSeverity;
  title: string;
  message: string;
}

/**
 * `currentBalance` is booked fact — money in the account today. Every other
 * figure is a projection: the days from today on are credited with what an
 * ordinary day of that weekday receives, while expenses stand exactly as
 * scheduled. Without that the curve was the month's whole bill list against
 * none of its sales, and it announced a negative balance within days every time
 * a month opened.
 */
export interface CashPosition {
  currentBalance: number;
  endOfMonthProjection: number;
  daysUntilNegative: number | null;
  lowestProjected: number;
  lowestProjectedDate: string;
  /**
   * Whether there was any trading history to credit the days ahead with. False
   * means the forward figures are bills against nothing — do not present them
   * as a balance heading for zero.
   */
  expectsReceipts: boolean;
  /**
   * Tomorrow's own line of the runway. Absent when the forecast does not reach
   * it — a closed month, or the month's last day. Everything else here
   * describes the month; this is the one figure about a day someone is about
   * to open the doors on.
   */
  nextDay?: DayCash;
  /**
   * The whole curve, one entry per day of the month — what the chart draws.
   * `endOfMonthProjection` is its last `balance` and `lowestProjected` its
   * smallest, so nothing here needs re-deriving in the browser.
   *
   * Days before today add nothing to their booked balance, so the same series
   * carries the realised half and the projected one: split it at today rather
   * than fetching a second curve for the left of the chart. The chart used to
   * plot the *booked* running balance from `/summary/cashflow` and caption its
   * tail "projeção" — the curve that dives every month because all the bills
   * are booked on the 1st and none of the sales have happened yet.
   */
  forecast: DayCash[];
}

/**
 * One day of the runway, split into what is booked and what is expected —
 * they are answerable in different ways. `scheduledOut` is a bill someone can
 * call about; `expectedIn` is a rhythm nobody controls.
 */
export interface DayCash {
  date: string;
  /**
   * The projected balance at the end of the day: everything booked up to it,
   * plus the receipts the days from today on are expected to bring on top of
   * what they have booked. Same basis as `endOfMonthProjection`.
   */
  balance: number;
  scheduledIn: number;
  scheduledOut: number;
  /**
   * What an ordinary day of that weekday still brings beyond what it has
   * booked; 0 when the day has already booked more than its weekday usually
   * receives, and 0 when there is no trading history at all —
   * `expectsReceipts` is what tells those two apart.
   */
  expectedIn: number;
}

export interface Analysis {
  /**
   * The shape of this payload. The daily snapshot is stored as JSON and diffed
   * against yesterday's, so a renamed field would read as zero on the old side
   * and report the whole month as movement. The API refuses to serve or compare
   * a snapshot whose version does not match.
   */
  schemaVersion: number;
  month: YearMonth;
  period: Period;
  /**
   * The state of the month *now*, today included — not a measurement of it
   * (that is `trends`, over its own labelled window) and not the whole month.
   * Every figure covers the days that have arrived and no others.
   */
  kpis: {
    /** What came in minus what went out, over the days so far. */
    resultado: number;
    /** What the pharmacy sold this month. Every performance reading uses this. */
    faturamento: number;
    /** Every centavo that actually arrived — loans and aportes included. */
    entradasCaixa: number;
    /** What has actually left the account so far. */
    despesa: number;
    /**
     * Booked for the days still to come. A commitment, not a spend: render it
     * beside `despesa`, never added into it — the whole month's bills are
     * booked on the 1st, and counting them as money gone reported a month as
     * lost on its 3rd day.
     */
    despesaAgendada: number;
    // No "days remaining" or "last month up to today" here: they are
    // `period.daysRemaining` and `trends.faturamento` (whose current/previous
    // are both months over the same finished days). They used to be derived a
    // second time here and disagreed with the figures rendered beside them.
  };
  health: FinancialHealth;
  trends: Trends;
  weekdays: WeekdayStat[];
  weekComparison: WeekComparison;
  highlights: {
    bestIncome: DayHighlight;
    worstIncome: DayHighlight;
    bestBalance: DayHighlight;
    worstBalance: DayHighlight;
  };
  cashOutDays: CashOutDay[];
  expenseComposition: CategoryComposition[];
  /**
   * The month's faturamento split by category — which kinds of sale it was
   * made of (atacado, balcão, convênio). Origin decides what is faturamento
   * (see ADR-016); the category only splits it. Empty when nothing was sold.
   */
  revenueComposition: CategoryComposition[];
  goals: GoalProgress;
  projection: Projection;
  history: MonthlySnapshot[];
  cashPosition: CashPosition;
  recommendations: Recommendation[];
}

// --- Imported payment-processor data (PagBank; Stone later) ---

export type PaymentMethod = "credito" | "debito" | "pix" | "boleto" | "outros";

export interface Sale {
  id: string;
  provider: string;
  external_id: string;
  sale_date: string;
  gross_amount: number;
  net_amount: number;
  fee_amount: number;
  method: PaymentMethod;
  brand: string;
  installments: number;
}

export interface ExpectedReceivable {
  provider: string;
  sale_id: string;
  expected_date: string;
  amount: number;
  installment_number: number;
  installment_total: number;
}

export interface SalesResponse {
  sales: Sale[];
  totals: { gross: number; net: number; fee: number };
  by_method: Record<string, number> | null;
  from: string;
  to: string;
}

export interface ReceivablesResponse {
  receivables: ExpectedReceivable[];
  total: number;
  from: string;
  to: string;
}

export interface PaymentForecastPoint {
  date: string;
  projected_income: number;
  projected_receivable: number;
  projected_expense: number;
  running_balance: number;
}

export interface ForecastResponse {
  points: PaymentForecastPoint[];
  month: string;
}

// --- Caderninho de fiado (GET /fiado) ---
//
// Controle interno, à parte do razão: nada aqui é faturamento, caixa nem
// previsto, e nenhuma métrica do painel enxerga estes valores (ADR-027).
// Amounts are centavos and dates are "YYYY-MM-DD", como no resto da API.

/** A foto de uma conta do caderninho: quanto a pessoa deve *agora*. */
export interface Devedor {
  /** Slug normalizado. É a identidade da pessoa — e o endereço dela na URL. */
  cliente: string;
  /** O nome como o usuário digitou; é o que se mostra na tela. */
  nome: string;
  /**
   * Positivo é o que a pessoa deve. Zero é conta quitada e negativo é crédito
   * dela (pagou mais do que devia) — os dois acontecem e nenhum é erro, então
   * a tela nomeia os três casos em vez de assumir dívida.
   */
  saldo: number;
  /**
   * O dia em que o saldo saiu de zero — "está devendo sem parar desde X", não
   * "a compra mais antiga em aberto". Vem null quando não há dívida, porque o
   * backend limpa isso quando a conta zera.
   */
  desde: string | null;
  /**
   * Há quantos dias a conta está aberta, contado pelo backend. Fiado não
   * vence, envelhece: a única leitura permitida deste número é "em aberto há N
   * dias" — nunca "vencido", "atrasado" ou "inadimplente", porque nada foi
   * prometido. Não recalcule no browser: quem sabe que dia é hoje no fuso da
   * farmácia é o servidor, e uma dívida de hoje não tem idade medível
   * (ADR-017), então N começa em 1.
   */
  dias_em_aberto: number | null;
}

export interface CaderninhoResponse {
  /** Ordenado por saldo desc: quem deve mais primeiro. */
  devedores: Devedor[];
  total_em_aberto: number;
  count: number;
}

/**
 * Um movimento: o que aconteceu, no dia em que aconteceu.
 *
 * **O sinal é o tipo.** Positivo é dívida (a pessoa levou), negativo é
 * pagamento (a pessoa pagou) — não há campo `tipo` a consultar, e por isso a
 * UI tem de deixar o sinal visível em vez de mostrar só o módulo.
 *
 * Não há saldo por linha, de propósito: o saldo é o do devedor e aparece uma
 * vez, no topo. Somar movimento a movimento aqui reintroduz a coluna que foi
 * descartada — e numa lista paginada ela começaria pelo meio da história.
 */
export interface FiadoMovimento {
  id: string;
  cliente: string;
  nome: string;
  valor: number;
  data: string;
  descricao: string;
}

/**
 * Uma página de movimentos, do mais recente para o mais antigo.
 *
 * `next_cursor` vem ausente quando acabou — é o que encerra a paginação.
 * `truncated` com `warning` é a ADR-015: uma lista cortada nunca sai calada, e
 * o aviso é para renderizar, não para engolir.
 */
export interface FiadoMovimentosResponse {
  movimentos: FiadoMovimento[];
  count: number;
  next_cursor?: string;
  truncated: boolean;
  warning?: string;
}
