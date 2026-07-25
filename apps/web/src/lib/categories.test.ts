import { describe, expect, it } from "vitest";
import { makeCategory } from "@/test/factories";
import { categoriesByType, categoryLabelMap } from "./categories";

describe("categoryLabelMap", () => {
  it("indexes labels by slug", () => {
    const map = categoryLabelMap([
      makeCategory({ Slug: "venda_balcao", Label: "Venda Balcão" }),
      makeCategory({ Slug: "aluguel", Label: "Aluguel", Type: "expense" }),
    ]);

    expect(map).toEqual({
      venda_balcao: "Venda Balcão",
      aluguel: "Aluguel",
    });
  });

  it("is empty for no categories", () => {
    expect(categoryLabelMap([])).toEqual({});
  });

  it("keeps the last label when a slug repeats", () => {
    const map = categoryLabelMap([
      makeCategory({ Slug: "aluguel", Label: "Antigo" }),
      makeCategory({ Slug: "aluguel", Label: "Novo" }),
    ]);

    expect(map.aluguel).toBe("Novo");
  });
});

describe("categoriesByType", () => {
  const categories = [
    makeCategory({ Slug: "venda_balcao", Type: "income" }),
    makeCategory({ Slug: "aluguel", Type: "expense" }),
    makeCategory({ Slug: "fornecedor", Type: "expense" }),
  ];

  it("keeps only income categories", () => {
    expect(categoriesByType(categories, "income").map((c) => c.Slug)).toEqual([
      "venda_balcao",
    ]);
  });

  it("keeps only expense categories", () => {
    expect(categoriesByType(categories, "expense").map((c) => c.Slug)).toEqual([
      "aluguel",
      "fornecedor",
    ]);
  });

  it("is empty when nothing matches", () => {
    expect(categoriesByType([], "income")).toEqual([]);
  });
});
