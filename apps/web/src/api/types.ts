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
}

export interface CreateEntryInput {
  date: string;
  amount: number;
  category: string;
  type: "expense" | "income";
  description: string;
  due_date?: string;
  payment_status: "pending" | "paid";
  supplier?: string;
}

export interface MonthlySummary {
  Month: string;
  TotalIncome: number;
  TotalExpense: number;
  Balance: number;
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
  CashRunway: "cash_runway",
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
  messages: Insight[];
}

export interface MonthTrend {
  current: number;
  previous: number;
  change: number;
  direction: "up" | "down" | "stable";
}

export interface Trends {
  receita: MonthTrend;
  despesa: MonthTrend;
  resultado: MonthTrend;
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
  detail?: string;
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
  income: number;
  incomeTarget: number | null;
  expense: number;
  expenseTarget: number | null;
}

export interface WeekComparison {
  current: number;
  previous: number;
  previousUpToDay: number;
  projectedWeekly: number;
  projectedMonthly: number;
  monthlyTarget: number;
  labels: string[];
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
  month: YearMonth;
  kpis: {
    resultado: number;
    receita: number;
    despesa: number;
    daysRemaining: number;
    previousMonthIncomeUpToDay: number;
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
