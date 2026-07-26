import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@/api/http";
import { queryKeys } from "@/api/queries";
import type { Analysis, YearMonth } from "@/api/types";
import { useMonthlyAnalysis } from "./useMonthlyAnalysis";

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

// Only the fields the assertions touch — the endpoint returns the whole
// Analysis, which the page consumes as-is.
const analysis = {
  month: "2026-02",
  health: { status: "atencao", messages: [] },
  recommendations: [],
} as unknown as Analysis;

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useMonthlyAnalysis", () => {
  it("fetches the month's analysis from the backend in one call", async () => {
    const monthly = vi.spyOn(api.analysis, "monthly").mockResolvedValue(analysis);

    const { result } = renderHook(
      () => useMonthlyAnalysis("2026-02" as YearMonth),
      { wrapper: wrapper() },
    );

    // Undefined while in flight — that is what the page renders its skeleton
    // from.
    expect(result.current).toBeUndefined();
    await waitFor(() => expect(result.current).toEqual(analysis));
    expect(monthly).toHaveBeenCalledExactlyOnceWith("2026-02");
  });

  it("stays undefined when the request fails", async () => {
    vi.spyOn(api.analysis, "monthly").mockRejectedValue(new Error("boom"));

    const { result } = renderHook(
      () => useMonthlyAnalysis("2026-02" as YearMonth),
      { wrapper: wrapper() },
    );

    await waitFor(() => expect(result.current).toBeUndefined());
  });
});

describe("queryKeys.analysis", () => {
  it("keys by month so switching months refetches", () => {
    expect(queryKeys.analysis("2026-02")).not.toEqual(
      queryKeys.analysis("2026-03"),
    );
  });
});
