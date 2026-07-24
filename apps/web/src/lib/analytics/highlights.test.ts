import { describe, expect, it } from "vitest";
import { makeEntry } from "@/test/factories";
import { getCashOutDays, getHighlights } from "./highlights";

const sale = (date: string, amount: number) =>
  makeEntry({ TransactionDate: date, Amount: amount });

const cost = (date: string, amount: number, category = "fornecedor") =>
  makeEntry({
    TransactionDate: date,
    Amount: amount,
    Type: "expense",
    Category: category,
  });

describe("getHighlights", () => {
  it("returns placeholders when there are no entries", () => {
    const highlights = getHighlights([]);
    const empty = { date: "—", label: "Sem dados", amount: 0 };

    expect(highlights.bestIncome).toEqual(empty);
    expect(highlights.worstIncome).toEqual(empty);
    expect(highlights.bestBalance).toEqual(empty);
    expect(highlights.worstBalance).toEqual(empty);
  });

  it("picks the best and worst revenue days", () => {
    const highlights = getHighlights([
      sale("2026-02-05", 1000),
      sale("2026-02-06", 9000),
      sale("2026-02-07", 300),
    ]);

    expect(highlights.bestIncome.date).toBe("2026-02-06");
    expect(highlights.bestIncome.amount).toBe(9000);
    expect(highlights.worstIncome.date).toBe("2026-02-07");
    expect(highlights.worstIncome.amount).toBe(300);
  });

  it("sums multiple entries on the same day before comparing", () => {
    const highlights = getHighlights([
      sale("2026-02-05", 1000),
      sale("2026-02-05", 1000),
      sale("2026-02-06", 1500),
    ]);

    expect(highlights.bestIncome.date).toBe("2026-02-05");
    expect(highlights.bestIncome.amount).toBe(2000);
  });

  it("nets expenses against revenue for the balance highlights", () => {
    const highlights = getHighlights([
      sale("2026-02-05", 5000),
      cost("2026-02-05", 1000),
      sale("2026-02-06", 2000),
      cost("2026-02-06", 8000),
    ]);

    expect(highlights.bestBalance.date).toBe("2026-02-05");
    expect(highlights.bestBalance.amount).toBe(4000);
    expect(highlights.worstBalance.date).toBe("2026-02-06");
    expect(highlights.worstBalance.amount).toBe(-6000);
  });

  it("labels the day in pt-BR", () => {
    const highlights = getHighlights([sale("2026-02-05", 1000)]);

    expect(highlights.bestIncome.label).toMatch(/05/);
    expect(highlights.bestIncome.label).toMatch(/fev/i);
  });
});

describe("getCashOutDays", () => {
  it("returns an empty list when nothing was spent", () => {
    expect(getCashOutDays([])).toEqual([]);
    expect(getCashOutDays([sale("2026-02-05", 1000)])).toEqual([]);
  });

  it("ranks days by total spend, breaking each into categories", () => {
    const days = getCashOutDays([
      cost("2026-02-05", 1000, "aluguel"),
      cost("2026-02-06", 7000, "fornecedor"),
      cost("2026-02-06", 500, "aluguel"),
    ]);

    expect(days).toHaveLength(2);
    expect(days[0]).toEqual({
      date: "2026-02-06",
      total: 7500,
      items: [
        { category: "fornecedor", amount: 7000, count: 1 },
        { category: "aluguel", amount: 500, count: 1 },
      ],
    });
    expect(days[1].date).toBe("2026-02-05");
  });

  it("merges repeat categories within a day and counts them", () => {
    const [day] = getCashOutDays([
      cost("2026-02-05", 1000, "fornecedor"),
      cost("2026-02-05", 2000, "fornecedor"),
    ]);

    expect(day.items).toEqual([
      { category: "fornecedor", amount: 3000, count: 2 },
    ]);
  });

  it("keeps only the five biggest days", () => {
    const entries = Array.from({ length: 8 }, (_, i) =>
      cost(`2026-02-0${i + 1}`, (i + 1) * 100),
    );

    const days = getCashOutDays(entries);

    expect(days).toHaveLength(5);
    expect(days[0].total).toBe(800);
    expect(days[4].total).toBe(400);
  });
});
