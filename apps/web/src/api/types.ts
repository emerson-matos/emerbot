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

export type UpdateEntryInput = Partial<CreateEntryInput> & {
  payment_status?: "pending" | "paid";
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

/**
 * `Indefinido` is a month with no finished day to judge — its first day, or one
 * that has not begun. It is the *absence* of a grade, not a fourth grade:
 * render it neutrally and without a score. Calling such a month "boa, 100
 * pontos" is the same unfounded claim as the "crítico" it replaced.
 */
export const FinancialHealthStatus = {
  Boa: "boa",
  Atencao: "atencao",
  Critico: "critico",
  Indefinido: "indefinido",
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
  MonthStart: "month_start",
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
   *
   * Null when `status` is `indefinido`: a month with no finished day has no
   * score, and 0 would render as the worst month on record.
   */
  score: number | null;
  messages: Insight[];
}

export interface MonthTrend {
  current: number;
  previous: number;
  change: number;
  direction: "up" | "down" | "stable";
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
  /** Days still to trade, today included. 0 for a closed month. */
  daysRemaining: number;
  daysTotal: number;
  /** True when the analysed month is the one we are in. */
  inProgress: boolean;
}

export interface WeekdayStat {
  day: number;
  label: string;
  avg: number;
  total: number;
  count: number;
  isToday: boolean;
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
  labels: string[];
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
export interface Projection {
  actual: number;
  remaining: number;
  projected: number;
  target: number;
  /** What the projection still misses the target by; 0 once it clears it. */
  gap: number;
  onTrack: boolean;
  daysRemaining: number;
  /** What each day left must bring, measured from `actual`, to reach `target`. */
  neededPerDay: number;
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

export interface CashPosition {
  currentBalance: number;
  endOfMonthProjection: number;
  daysUntilNegative: number | null;
  lowestProjected: number;
  lowestProjectedDate: string;
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
  kpis: {
    resultado: number;
    /** What the pharmacy sold this month. Every performance reading uses this. */
    faturamento: number;
    /** Every centavo that actually arrived — loans and aportes included. */
    entradasCaixa: number;
    despesa: number;
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
