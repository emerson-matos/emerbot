import type { Category, Entry } from "@/api/types";

/**
 * Builders for the API shapes the lib helpers consume. Each takes partial
 * overrides so a test only spells out the fields it actually asserts on.
 */

export function makeEntry(overrides: Partial<Entry> = {}): Entry {
  return {
    UserID: "user-1",
    EntryID: "entry-1",
    TransactionDate: "2026-02-05",
    Amount: 1000,
    Category: "venda_balcao",
    Type: "income",
    Description: "",
    DueDate: null,
    PaymentStatus: "paid",
    PaymentDate: null,
    Supplier: "",
    Source: "web",
    ...overrides,
  };
}

export function makeCategory(overrides: Partial<Category> = {}): Category {
  return {
    UserID: "user-1",
    Slug: "venda_balcao",
    Label: "Venda Balcão",
    Type: "income",
    Default: false,
    ...overrides,
  };
}

/**
 * pt-BR currency formatting separates the symbol with U+00A0, which is
 * invisible in source. Normalize before asserting so the expectations stay
 * readable and survive ICU tweaks.
 */
export function normalizeSpaces(value: string): string {
  return value.replace(/\u00A0/g, " ");
}
