import type { Category, Devedor, Entry, FiadoMovimento } from "@/api/types";

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

/** Uma conta do caderninho de fiado: por padrão, alguém devendo há 60 dias. */
export function makeDevedor(overrides: Partial<Devedor> = {}): Devedor {
  return {
    cliente: "joao_silva",
    nome: "João Silva",
    saldo: 34000,
    desde: "2026-06-12",
    dias_em_aberto: 60,
    ...overrides,
  };
}

/**
 * Um movimento do caderninho. O padrão é positivo — uma dívida — porque o
 * sinal é o tipo: um pagamento é este mesmo objeto com `valor` negativo.
 */
export function makeMovimento(
  overrides: Partial<FiadoMovimento> = {},
): FiadoMovimento {
  return {
    id: "01J0000000000000000000000A",
    cliente: "joao_silva",
    nome: "João Silva",
    valor: 4000,
    data: "2026-08-10",
    descricao: "",
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
