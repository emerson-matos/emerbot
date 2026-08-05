import { ProjectionBasis } from "@/api/types";
import type { ProjectionStatus } from "@/api/types";

/**
 * What the projection is standing on, in the user's words. The backend decides
 * which one applies (`Projection.basis`) — a card must not infer confidence
 * from the amounts, since only the backend knows how much of the window traded.
 *
 * A closed month has no note: nothing was estimated, the figure *is* the
 * month's faturamento, and captioning it "pela média das últimas 8 semanas"
 * would present the one number that is not a forecast as though it were one.
 *
 * It lives here rather than on the Analysis page because the dashboard quotes
 * the same projection now. Two copies of this map would drift, and the drift
 * would be a page quoting an eight-week estimate as fact.
 */
export const projectionBasisNote: Partial<Record<ProjectionBasis, string>> = {
  [ProjectionBasis.Window]:
    "Pela média de cada dia da semana nas últimas 8 semanas, com peso maior para as mais recentes.",
  [ProjectionBasis.Partial]:
    "Menos de uma semana de vendas registradas — ainda vai mudar bastante.",
  [ProjectionBasis.None]:
    "Sem vendas registradas nas últimas 8 semanas para projetar.",
};

/**
 * The colour of the projection verdict. Read off `Projection.status`, never
 * re-derived from coverage in the browser: a percentage rounded here beside a
 * colour computed there is how one screen showed "97% da meta" in red under a
 * green "Ritmo suficiente".
 */
export const projectionTone: Record<ProjectionStatus, string> = {
  "": "text-muted-foreground",
  success: "text-success",
  warning: "text-warning",
  danger: "text-destructive",
};
