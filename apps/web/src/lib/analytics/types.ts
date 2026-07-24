import type { Entry, MonthlySummary, CashFlowPoint } from '../../api/types'

export type YearMonth = `${number}-${number}`

export const FinancialHealthStatus = {
  Boa: 'boa',
  Atencao: 'atencao',
  Critico: 'critico',
} as const
export type FinancialHealthStatus = typeof FinancialHealthStatus[keyof typeof FinancialHealthStatus]

export const InsightType = {
  ExpenseGrowth: 'expense_growth',
  RevenueDrop: 'revenue_drop',
  LowCashFlow: 'low_cash_flow',
  GoalBehind: 'goal_behind',
  GoodPerformance: 'good_performance',
  WeeklyImprovement: 'weekly_improvement',
  WeeklyDecline: 'weekly_decline',
  GoalOnTrack: 'goal_on_track',
  CashRunway: 'cash_runway',
} as const
export type InsightType = typeof InsightType[keyof typeof InsightType]

export const InsightSeverity = {
  Info: 'info',
  Warning: 'warning',
  Critical: 'critical',
} as const
export type InsightSeverity = typeof InsightSeverity[keyof typeof InsightSeverity]

export type Insight = {
  type: InsightType
  severity: InsightSeverity
  title: string
  description: string
  value?: number
}

export type FinancialHealth = {
  status: FinancialHealthStatus
  messages: Insight[]
}

export type MonthTrend = {
  current: number
  previous: number
  change: number
  direction: 'up' | 'down' | 'stable'
}

export type Trends = {
  receita: MonthTrend
  despesa: MonthTrend
  resultado: MonthTrend
}

export type WeekdayStat = {
  day: number
  label: string
  avg: number
  total: number
  count: number
  isToday: boolean
}

export type DayHighlight = {
  date: string
  label: string
  amount: number
  detail?: string
}

export type CashOutDay = {
  date: string
  total: number
  items: { category: string; amount: number; count: number }[]
}

export type ExpenseComposition = {
  categoryId: string
  categoryName: string
  amount: number
  percentage: number
}

export type GoalProgress = {
  revenueTarget: number
  revenueActual: number
  revenuePct: number
  expenseTarget: number
  expenseActual: number
  expensePct: number
  daysRemaining: number
  daysTotal: number
}

export type MonthlySnapshot = {
  month: YearMonth
  label: string
  income: number
  incomeTarget: number | null
  expense: number
  expenseTarget: number | null
}

export type WeekComparison = {
  current: number
  previous: number
  previousUpToDay: number
  projectedWeekly: number
  projectedMonthly: number
  monthlyTarget: number
  avg8Weeks?: number
  labels: string[]
}

export const RecommendationSeverity = {
  Success: 'success',
  Warning: 'warning',
  Danger: 'danger',
} as const
export type RecommendationSeverity = typeof RecommendationSeverity[keyof typeof RecommendationSeverity]

export type Recommendation = {
  severity: RecommendationSeverity
  title: string
  message: string
}

export type CashPosition = {
  currentBalance: number
  endOfMonthProjection: number
  daysUntilNegative: number | null
  lowestProjected: number
  lowestProjectedDate: string
}

export type Analysis = {
  kpis: {
    resultado: number
    receita: number
    despesa: number
    daysRemaining: number
    previousMonthIncomeUpToDay: number
  }
  health: FinancialHealth
  trends: Trends
  weekdays: WeekdayStat[]
  weekComparison: WeekComparison
  highlights: {
    bestIncome: DayHighlight
    worstIncome: DayHighlight
    bestBalance: DayHighlight
    worstBalance: DayHighlight
  }
  cashOutDays: CashOutDay[]
  expenseComposition: ExpenseComposition[]
  goals: GoalProgress
  history: MonthlySnapshot[]
  cashPosition: CashPosition
  recommendations: Recommendation[]
}

export type GoalInput = {
  revenueTarget: number
  expenseTarget: number
}

export type AnalysisInput = {
  month: YearMonth
  entries: Entry[]
  previousEntries: Entry[]
  summaries: MonthlySummary[]
  goals: GoalInput[]
  cashFlowPoints: CashFlowPoint[]
  now: Date
}
