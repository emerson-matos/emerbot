import { describe, expect, it } from "vitest";
import { makeEntry } from "@/test/factories";
import { getExpenseComposition } from "./composition";

describe("getExpenseComposition", () => {
  it("groups expenses by category, sorted by amount descending", () => {
    const composition = getExpenseComposition([
      makeEntry({ Type: "expense", Category: "aluguel", Amount: 2000 }),
      makeEntry({ Type: "expense", Category: "fornecedor", Amount: 6000 }),
      makeEntry({ Type: "expense", Category: "aluguel", Amount: 2000 }),
    ]);

    expect(composition).toEqual([
      {
        categoryId: "fornecedor",
        categoryName: "Fornecedor",
        amount: 6000,
        percentage: 60,
      },
      {
        categoryId: "aluguel",
        categoryName: "Aluguel",
        amount: 4000,
        percentage: 40,
      },
    ]);
  });

  it("ignores income entries", () => {
    const composition = getExpenseComposition([
      makeEntry({ Type: "income", Category: "venda_balcao", Amount: 9000 }),
      makeEntry({ Type: "expense", Category: "aluguel", Amount: 1000 }),
    ]);

    expect(composition).toHaveLength(1);
    expect(composition[0]).toMatchObject({
      categoryId: "aluguel",
      percentage: 100,
    });
  });

  it("returns an empty list when there are no expenses", () => {
    expect(getExpenseComposition([])).toEqual([]);
    expect(
      getExpenseComposition([makeEntry({ Type: "income", Amount: 500 })]),
    ).toEqual([]);
  });

  it("titlecases the slug for display", () => {
    const [first] = getExpenseComposition([
      makeEntry({ Type: "expense", Category: "material_de_limpeza" }),
    ]);

    expect(first.categoryName).toBe("Material De Limpeza");
  });
});
