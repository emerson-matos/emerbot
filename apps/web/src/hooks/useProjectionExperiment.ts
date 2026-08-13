import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/http";
import { queryKeys } from "@/api/queries";

// ADR-030: loaded only on the analysis page, never as part of the operational
// analysis request.
export function useProjectionExperiment(enabled = true) {
  return useQuery({
    queryKey: queryKeys.projectionExperiment(),
    queryFn: () => api.analysis.projectionExperiment(),
    enabled,
  });
}
