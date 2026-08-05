import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import CashFlowChart from "./CashFlowChart";
import type { DayCash } from "@/api/types";

// Recharts measures its container, and jsdom reports every element as 0×0, so
// the chart renders nothing without a width. Fixing the responsive container's
// size is the usual way round it and is what lets the series be asserted on.
vi.mock("recharts", async () => {
  const actual = await vi.importActual<typeof import("recharts")>("recharts");
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <actual.ResponsiveContainer width={800} height={320}>
        {children as React.ReactElement}
      </actual.ResponsiveContainer>
    ),
  };
});

vi.mock("@/lib/entries", async () => {
  const actual = await vi.importActual<typeof import("@/lib/entries")>("@/lib/entries");
  return { ...actual, todayISO: () => "2026-07-27" };
});

/**
 * A month whose booked curve dives after today and whose projected one does
 * not — which is the entire month, every month: the bills are all recorded on
 * the 1st and the sales as they happen.
 *
 * `balance` is the projected figure. The chart used to be handed the booked
 * `RunningBalance` from /summary/cashflow and label its tail "projeção", so the
 * page drew a collapse while the card beside it said the month closes in the
 * black. See ADR-021.
 */
const forecast: DayCash[] = [
  { date: "2026-07-26", balance: 3050000, scheduledIn: 0, scheduledOut: 0, expectedIn: 0 },
  { date: "2026-07-27", balance: 3031155, scheduledIn: 0, scheduledOut: 20000, expectedIn: 0 },
  { date: "2026-07-28", balance: 2810655, scheduledIn: 0, scheduledOut: 330000, expectedIn: 109500 },
];

describe("CashFlowChart", () => {
  it("plots the projected series, day by day", () => {
    const { container } = render(<CashFlowChart data={forecast} />);
    const labels = [...container.querySelectorAll("text")].map((t) => t.textContent);

    // One column per day, read off the series' own fields. The previous shape
    // spelled them Date/RunningBalance, so this is also what catches a payload
    // whose casing drifted — an undefined date would blow up in parseISO.
    for (const day of ["26/07", "27/07", "28/07"]) {
      expect(labels).toContain(day);
    }
    // Two series: the realised half solid, the projected half dashed. Today is
    // in both so the lines meet rather than leaving a gap at the join.
    expect(container.querySelectorAll('path[stroke-dasharray="6 4"]').length).toBeGreaterThan(0);
  });

  it("says what the dashed half is", () => {
    const { container } = render(<CashFlowChart data={forecast} />);
    const text = container.textContent ?? "";

    // It used to read "Hoje / projeção" over the booked curve — the one thing
    // that line was not.
    expect(text).toContain("Projetado");
    expect(text).toContain("recebimento médio");
  });

  it("renders an empty month without crashing", () => {
    const { container } = render(<CashFlowChart data={[]} />);
    expect(container).toBeTruthy();
  });
});
