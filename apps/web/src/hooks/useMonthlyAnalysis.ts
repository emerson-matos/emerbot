import { useQuery } from "@tanstack/react-query";
import { api } from "../api/http";
import { queryKeys } from "../api/queries";
import type { Analysis, YearMonth } from "@/api/types";

/**
 * The month's analysis, assembled by the backend.
 *
 * This used to fan out to five endpoints and build the analysis in the browser,
 * which meant the WhatsApp digest and the AI bot — neither of which runs a
 * browser — could not say any of it. The logic now lives in Go
 * (packages/finance/analytics) and every consumer reads the same numbers.
 *
 * Returns undefined while loading, which is what the page renders its skeleton
 * from.
 */
export function useMonthlyAnalysis(month: YearMonth): Analysis | undefined {
  const { data } = useQuery({
    queryKey: queryKeys.analysis(month),
    queryFn: () => api.analysis.monthly(month),
  });
  return data;
}
