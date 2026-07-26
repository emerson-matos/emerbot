import { useQuery } from "@tanstack/react-query";
import { api } from "../api/http";
import { queryKeys } from "../api/queries";
import type { YearMonth } from "@/api/types";

/**
 * The month's analysis, assembled by the backend.
 *
 * This used to fan out to five endpoints and build the analysis in the browser,
 * which meant the WhatsApp digest and the AI bot — neither of which runs a
 * browser — could not say any of it. The logic now lives in Go
 * (packages/finance/analytics) and every consumer reads the same numbers.
 *
 * Returns the query rather than just its data so the page can tell "still
 * loading" from "the request failed" — collapsing the two leaves a failed load
 * showing a skeleton that never resolves.
 */
export function useMonthlyAnalysis(month: YearMonth) {
  return useQuery({
    queryKey: queryKeys.analysis(month),
    queryFn: () => api.analysis.monthly(month),
  });
}
