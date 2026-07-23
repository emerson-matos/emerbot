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
