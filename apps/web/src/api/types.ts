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

// --- Imported payment-processor data (PagBank; Stone later) ---

export type PaymentMethod = "credito" | "debito" | "pix" | "boleto" | "outros";

export interface Sale {
  ID: string;
  Provider: string;
  ExternalID: string;
  SaleDate: string;
  GrossAmount: number;
  NetAmount: number;
  FeeAmount: number;
  Method: PaymentMethod;
  Brand: string;
  Installments: number;
}

export interface ExpectedReceivable {
  Provider: string;
  SaleID: string;
  ExpectedDate: string;
  Amount: number;
  InstallmentNumber: number;
  InstallmentTotal: number;
}

export interface SalesResponse {
  sales: Sale[] | null;
  totals: { gross: number; net: number; fee: number };
  by_method: Record<string, number> | null;
  from: string;
  to: string;
}

export interface ReceivablesResponse {
  receivables: ExpectedReceivable[] | null;
  total: number;
  from: string;
  to: string;
}

export interface PaymentForecastPoint {
  Date: string;
  ProjectedIncome: number;
  ProjectedReceivable: number;
  ProjectedExpense: number;
  RunningBalance: number;
}

export interface ForecastResponse {
  points: PaymentForecastPoint[] | null;
  month: string;
}
