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
    const monthly = vi
      .spyOn(api.analysis, "monthly")
      .mockResolvedValue(analysis);

    const { result } = renderHook(
      () => useMonthlyAnalysis("2026-02" as YearMonth),
      { wrapper: wrapper() },
    );

    expect(result.current.data).toBeUndefined();
    await waitFor(() => expect(result.current.data).toEqual(analysis));
    expect(monthly).toHaveBeenCalledExactlyOnceWith("2026-02");
  });

  it("reports a failure instead of looking like it is still loading", async () => {
    vi.spyOn(api.analysis, "monthly").mockRejectedValue(new Error("boom"));

    const { result } = renderHook(
      () => useMonthlyAnalysis("2026-02" as YearMonth),
      { wrapper: wrapper() },
    );

    // The page branches on isError; if a failure only ever showed up as
    // `data === undefined` it would render its skeleton forever.
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.isPending).toBe(false);
    expect(result.current.data).toBeUndefined();
  });
});

describe("queryKeys.analysis", () => {
  it("keys by month so switching months refetches", () => {
    expect(queryKeys.analysis("2026-02")).not.toEqual(
      queryKeys.analysis("2026-03"),
    );
  });

  it('sits under "analysis" so entry and goal mutations can invalidate it', () => {
    // The mutations in api/queries.ts drop the whole ["analysis"] subtree,
    // because the analysis is derived from entries, summaries and goals.
    expect(queryKeys.analysis("2026-02")[0]).toBe("analysis");
  });
});
