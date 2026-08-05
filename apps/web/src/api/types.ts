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
  Supplier: string;
  Source: string;
  /**
   * Where the money came from, for income entries; empty on expenses. Only
   * "venda" counts as faturamento. Undefined on entries written before the
   * field existed — see the shim in lib/notifications.ts.
   */
  Origin?: IncomeOrigin | "";
  /**
   * Set on the occurrences /recorrente generated together, empty on a one-off
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

export interface NotificationPrefs {
  waEnabled: boolean;
  phone: string;
  notifyDueToday: boolean;
  notifyOverdue: boolean;
  notifyGoal: boolean;
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

export interface ExpenseComposition {
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
  Below: "below",
  OnTrack: "on_track",
  Above: "above",
} as const;
export type DayTargetScale =
  (typeof DayTargetScale)[keyof typeof DayTargetScale];

export const DayTargetState = {
  /** A real ask: the amounts below are meaningful. */
  OK: "ok",
  /** The month has ended — there is no "today" inside it to sell on. */
  ClosedMonth: "mes_fechado",
  /** No revenue goal set, so no share of one to ask for today. */
  NoGoal: "sem_meta",
  /** The goal is already reached. The one absence that is good news. */
  GoalMet: "meta_batida",
  /** Nothing in the trailing window to price any remaining day from. */
  NoHistory: "sem_historico",
  /** A weekday that never traded: the pharmacy does not open on it. */
  ClosedWeekday: "dia_sem_movimento",
  /**
   * The day after the analysed month's last one. It belongs to a month with
   * its own goal and its own gap, none of which are knowable from here. Only
   * `nextDayTarget` can carry it.
   */
  MonthOver: "mes_acaba_hoje",
} as const;
export type DayTargetState =
  (typeof DayTargetState)[keyof typeof DayTargetState];

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
   * The calendar day the ask is for, "YYYY-MM-DD". Absent when there is no day
   * to name — a closed month has no today, and the day after the month's last
   * one is not this month's to price.
   */
  date?: string;
  /** time.Weekday / getDay(): 0 = Sunday. Name it with lib/weekdays. */
  day: number;
  /** What this weekday usually brings, over a whole day. */
  historical: number;
  /** What it has to bring to keep the month on its goal. */
  target: number;
  /** `target` minus `historical`. Negative when the day can afford to be lighter. */
  delta: number;
  deltaPercent: number;
  factor: number;
  status: DayTargetScale;
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
   * What today has already sold. `todayTarget` is a whole-day figure measured
   * from the morning, so this is what it gets met against.
   */
  todayRevenue: number;
  /** Today's revenue target derived from historical weekday averages. */
  todayTarget: DayTarget;
  /**
   * Tomorrow's share of the same plan, at the same `factor` — the two are one
   * distribution of the gap over the days left, not two readings of it. It
   * therefore assumes today lands on `todayTarget`, which is exactly what
   * `projected` assumes about today as well.
   */
  nextDayTarget: DayTarget;
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
  expenseComposition: ExpenseComposition[];
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
