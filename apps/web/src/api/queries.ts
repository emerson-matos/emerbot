import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQueryClient,
  useQuery,
} from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { addMonths, endOfMonth, format, startOfMonth } from "date-fns";
import { CognitoAuthError } from "./types";
import type {
  CreateEntryInput,
  Entry,
  UpdateEntryInput,
} from "./types";
import { api } from "./http";
import { useAuth } from "@/lib/auth";
import { currentMonthKey } from "@/lib/entries";

export const queryKeys = {
  summaryMonthly: (month: string) => ["summary", "monthly", month] as const,
  summaryCategories: (from?: string, to?: string) =>
    ["summary", "categories", from, to] as const,
  cashflow: (month: string) => ["summary", "cashflow", month] as const,
  analysis: (month: string) => ["analysis", "monthly", month] as const,
  projectionExperiment: () => ["analysis", "projection-experiment"] as const,
  entries: (from: string, to: string) => ["entries", from, to] as const,
  entriesByMonth: () => ["entries", "byMonth"] as const,
  entry: (date: string, id: string) => ["entries", "byId", date, id] as const,
  goal: (month: string) => ["goal", month] as const,
  notificationPrefs: () => ["notifications", "preferences"] as const,
  categories: () => ["categories"] as const,
  paymentsSales: (from: string, to: string) =>
    ["payments", "sales", from, to] as const,
  paymentsReceivables: (from: string, to: string) =>
    ["payments", "receivables", from, to] as const,
  paymentsForecast: (month: string) =>
    ["payments", "forecast", month] as const,
  // Tudo do caderninho pendurado em ["fiado"]: é um sistema à parte do razão
  // (ADR-027), então nada que invalide lançamentos deve alcançá-lo, e nada
  // daqui deve alcançar as métricas.
  fiadoCaderninho: () => ["fiado", "caderninho"] as const,
  fiadoDevedor: (cliente: string) => ["fiado", "devedor", cliente] as const,
  fiadoMovimentosCliente: (cliente: string) =>
    ["fiado", "movimentos", "cliente", cliente] as const,
  fiadoMovimentosDia: (date: string) =>
    ["fiado", "movimentos", "dia", date] as const,
};

export function useCategories() {
  return useQuery({
    queryKey: queryKeys.categories(),
    queryFn: () => api.categories.list(),
    select: (data) => data.categories,
  });
}

export function useMonthlySummary(month: string) {
  return useQuery({
    queryKey: queryKeys.summaryMonthly(month),
    queryFn: () => api.summary.monthly(month),
  });
}

export function useMonthlyTrend(months: string[]) {
  return useQueries({
    queries: months.map((month) => ({
      queryKey: queryKeys.summaryMonthly(month),
      queryFn: () => api.summary.monthly(month),
    })),
  });
}

export function useCategorySummary(from?: string, to?: string) {
  return useQuery({
    queryKey: queryKeys.summaryCategories(from, to),
    queryFn: () => api.summary.categories(from, to),
  });
}

export function useCashFlow(month: string) {
  return useQuery({
    queryKey: queryKeys.cashflow(month),
    queryFn: () => api.summary.cashflow(month),
  });
}

export function useEntries(from: string, to: string) {
  return useQuery({
    queryKey: queryKeys.entries(from, to),
    queryFn: () => api.entries.list({ from, to }),
  });
}

/**
 * Entries for an arbitrary window, fetched only while one is set — the query
 * the Transações page runs once the user picks a period.
 *
 * It exists because useEntriesByMonth can only reach the months the user has
 * paged to, so filtering its cache by date silently reports "no results" for
 * any period outside them. GET /entries?from&to has no such horizon: the API
 * drops its row limit for date-bounded requests and ranges over the same
 * effective date (DueDate, else TransactionDate) the client filters on.
 *
 * An open-ended range sends only `from`, which the API reads as "onwards".
 * The key stays under ["entries"], so both entry mutations already invalidate
 * it and mark-as-paid's optimistic update already patches it.
 */
export function useEntriesInRange(range: { from: string; to?: string } | null) {
  return useQuery({
    queryKey: queryKeys.entries(range?.from ?? "", range?.to ?? ""),
    queryFn: () =>
      api.entries.list(
        range?.to ? { from: range.from, to: range.to } : { from: range!.from },
      ),
    enabled: range !== null,
    // Deliberately no keepPreviousData: the page re-applies the period to
    // whatever rows it holds, so serving the old window's rows while the new
    // one loads would filter them all out and flash "nothing in this period"
    // — the very claim this query exists to stop the page from making.
  });
}

const MAX_MONTHS_FORWARD = 12;
const MAX_MONTHS_BACK = 18;

function monthKeyOffset(key: string, offset: number): string {
  const [y, m] = key.split("-").map(Number);
  return format(addMonths(new Date(y, m - 1, 1), offset), "yyyy-MM");
}

function monthDiff(fromKey: string, toKey: string): number {
  const [fy, fm] = fromKey.split("-").map(Number);
  const [ty, tm] = toKey.split("-").map(Number);
  return (ty - fy) * 12 + (tm - fm);
}

async function fetchEntriesForMonth(monthKey: string) {
  const [y, m] = monthKey.split("-").map(Number);
  const monthStart = new Date(y, m - 1, 1);
  const { entries } = await api.entries.list({
    from: format(startOfMonth(monthStart), "yyyy-MM-dd"),
    to: format(endOfMonth(monthStart), "yyyy-MM-dd"),
  });
  return { month: monthKey, entries };
}

// One page per calendar month, expandable in both directions from the
// current month via fetchNextPage (future) / fetchPreviousPage (past).
export function useEntriesByMonth() {
  const currentMonth = currentMonthKey();

  return useInfiniteQuery({
    queryKey: queryKeys.entriesByMonth(),
    queryFn: ({ pageParam }: { pageParam: string }) => fetchEntriesForMonth(pageParam),
    initialPageParam: currentMonth,
    getNextPageParam: (lastPage) => {
      if (monthDiff(currentMonth, lastPage.month) >= MAX_MONTHS_FORWARD) return undefined;
      return monthKeyOffset(lastPage.month, 1);
    },
    getPreviousPageParam: (firstPage) => {
      if (monthDiff(currentMonth, firstPage.month) <= -MAX_MONTHS_BACK) return undefined;
      return monthKeyOffset(firstPage.month, -1);
    },
  });
}

export function usePaymentsSales(from: string, to: string) {
  return useQuery({
    queryKey: queryKeys.paymentsSales(from, to),
    queryFn: () => api.payments.sales(from, to),
  });
}

export function usePaymentsReceivables(from: string, to: string) {
  return useQuery({
    queryKey: queryKeys.paymentsReceivables(from, to),
    queryFn: () => api.payments.receivables(from, to),
  });
}

export function usePaymentsForecast(month: string) {
  return useQuery({
    queryKey: queryKeys.paymentsForecast(month),
    queryFn: () => api.payments.forecast(month),
  });
}

/** O caderninho inteiro: quem deve, quanto, e o total em aberto. */
export function useCaderninho() {
  return useQuery({
    queryKey: queryKeys.fiadoCaderninho(),
    queryFn: () => api.fiado.list(),
  });
}

/**
 * Um devedor, para a página aberta por URL — a lista não é cache suficiente
 * para um link salvo ou um F5 em /fiado/joao_silva.
 */
export function useDevedor(cliente: string | undefined) {
  return useQuery({
    queryKey: queryKeys.fiadoDevedor(cliente ?? ""),
    queryFn: () => api.fiado.devedor(cliente!),
    enabled: Boolean(cliente),
    // 404 aqui quer dizer que essa pessoa não está no caderninho; repetir a
    // requisição não muda isso, e a página tem estado próprio para dizer.
    retry: false,
  });
}

/**
 * O extrato de uma pessoa, paginado pelo cursor que a própria API devolve.
 *
 * A paginação é do DynamoDB, não uma janela calculada aqui: `next_cursor` é
 * opaco e ausente quando acabou, então é ele — e não uma contagem de páginas —
 * que decide se ainda há o que carregar.
 */
export function useFiadoMovimentos(cliente: string | undefined) {
  return useInfiniteQuery({
    queryKey: queryKeys.fiadoMovimentosCliente(cliente ?? ""),
    queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
      api.fiado.movimentos(cliente!, { cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: Boolean(cliente),
  });
}

/**
 * O caderninho de um dia. Sem cursor de propósito: o endpoint do dia responde
 * uma página só, então quando ela vem cortada o que sobra é o `warning` —
 * renderizá-lo é a única forma de a lista não mentir por omissão (ADR-015).
 */
export function useFiadoMovimentosDoDia(date: string) {
  return useQuery({
    queryKey: queryKeys.fiadoMovimentosDia(date),
    queryFn: () => api.fiado.movimentosDoDia(date),
    enabled: date !== "",
  });
}

export function useGoal(month: string) {
  return useQuery({
    queryKey: queryKeys.goal(month),
    queryFn: () => api.goals.get(month),
  });
}

export function useSaveGoalMutation(month: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: { revenue_target?: number; expense_target?: number }) =>
      api.goals.save(month, data),
    onError: () => {
      toast.error("Não foi possível salvar a meta.");
    },
    onSuccess: () => {
      toast.success("Meta salva.");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.goal(month) });
      queryClient.invalidateQueries({ queryKey: ["analysis"] });
    },
  });
}

// Also the registration call: the API writes the caller into the recipient
// table when it answers, so rendering the Notificações page is what makes an
// account reachable. Nothing else in the app hits this endpoint.
export function useNotificationPrefs() {
  return useQuery({
    queryKey: queryKeys.notificationPrefs(),
    queryFn: () => api.notifications.getPreferences(),
    select: (data) => data.preferences,
  });
}

type EntriesPage = { entries: Entry[]; count: number };
type InfiniteEntriesData = { pages: EntriesPage[]; pageParams: unknown[] };

function markEntryPaid<T extends EntriesPage | InfiniteEntriesData>(
  old: T,
  entryID: string,
  method: string,
): T {
  const flip = (e: Entry) =>
    e.EntryID === entryID
      ? { ...e, PaymentStatus: "paid" as const, PaymentMethod: method }
      : e;

  if ("pages" in old) {
    return {
      ...old,
      pages: old.pages.map((p) => ({ ...p, entries: p.entries.map(flip) })),
    };
  }
  return { ...old, entries: old.entries.map(flip) };
}

/**
 * Quits a lançamento, optionally recording how it was settled.
 *
 * Takes the whole entry, not an id: an entry is addressed by its transaction
 * date together with its id (see api.entries), and the caller always has the
 * row it is acting on. The method is free text and "" means the user left it
 * blank — which is the ordinary case, and is sent as-is rather than omitted so
 * quitting an entry never inherits a form of payment from an earlier attempt.
 */
export interface MarkPaidInput {
  entry: Entry;
  method: string;
}

export function useMarkPaidMutation() {
  const queryClient = useQueryClient();
  const entriesKey = { queryKey: ["entries"] };

  return useMutation({
    mutationFn: ({ entry, method }: MarkPaidInput) =>
      api.entries.update(entry.TransactionDate, entry.EntryID, {
        payment_status: "paid",
        payment_method: method,
      }),
    onMutate: async ({ entry, method }: MarkPaidInput) => {
      await queryClient.cancelQueries(entriesKey);
      const previous = queryClient.getQueriesData<
        EntriesPage | InfiniteEntriesData
      >(entriesKey);
      queryClient.setQueriesData<EntriesPage | InfiniteEntriesData | undefined>(
        entriesKey,
        (old) => (old ? markEntryPaid(old, entry.EntryID, method) : old),
      );
      return { previous };
    },
    onError: (_err, _input, context) => {
      context?.previous?.forEach(([key, data]) => {
        queryClient.setQueryData(key, data);
      });
      toast.error("Não foi possível marcar como pago.");
    },
    onSuccess: () => {
      toast.success("Transação marcada como paga.");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["summary"] });
      queryClient.invalidateQueries({ queryKey: ["analysis"] });
      queryClient.invalidateQueries(entriesKey);
    },
  });
}

// A single entry, for the edit page opened by URL: the list is paged by month
// and a bookmarked or reloaded /transacoes/:id/editar has no page to read from.
export function useEntry(date: string | undefined, id: string | undefined) {
  return useQuery({
    queryKey: queryKeys.entry(date ?? "", id ?? ""),
    queryFn: () => api.entries.get(date!, id!),
    enabled: Boolean(date && id),
    // A 404 here means the address does not exist; retrying cannot change that,
    // and the page has a "not found" state to show for it.
    retry: false,
  });
}

export function useUpdateEntryMutation(date: string, id: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: UpdateEntryInput) => api.entries.update(date, id, data),
    onError: () => {
      toast.error("Não foi possível salvar as alterações.");
    },
    onSuccess: (entry) => {
      // Under its *new* address: an edit that moves the transaction date moves
      // the row, so caching it under the old one would leave a stale entry
      // behind at a key nothing can invalidate.
      queryClient.setQueryData(
        queryKeys.entry(entry.TransactionDate, entry.EntryID),
        entry,
      );
      toast.success("Transação atualizada.");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["entries"] });
      queryClient.invalidateQueries({ queryKey: ["summary"] });
      queryClient.invalidateQueries({ queryKey: ["analysis"] });
    },
  });
}

export function useCreateEntryMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateEntryInput) => api.entries.create(data),
    onError: () => {
      toast.error("Não foi possível registrar a transação.");
    },
    onSuccess: () => {
      toast.success("Transação registrada.");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["entries"] });
      queryClient.invalidateQueries({ queryKey: ["summary"] });
      queryClient.invalidateQueries({ queryKey: ["analysis"] });
    },
  });
}

export function useDeleteEntryMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (entry: Entry) =>
      api.entries.delete(entry.TransactionDate, entry.EntryID),
    onSuccess: () => {
      toast.success("Transação excluída.");
    },
    onError: () => {
      toast.error("Não foi possível excluir a transação.");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["entries"] });
      queryClient.invalidateQueries({ queryKey: ["summary"] });
      queryClient.invalidateQueries({ queryKey: ["analysis"] });
    },
  });
}

export class InvalidCredentialsError extends Error {}
type LoginRequest = {
  email: string;
  password: string;
};

export function useLoginMutation() {
  const auth = useAuth();
  const navigate = useNavigate();

  return useMutation({
    mutationFn: async ({ email, password }: LoginRequest) => {
      try {
        const result = await api.auth.login(email, password);
        return result;
      } catch (err) {
        if (
          err instanceof CognitoAuthError &&
          (err.type === "NotAuthorizedException" ||
            err.type === "InvalidPasswordException" ||
            err.type === "UserNotFoundException")
        ) {
          throw new InvalidCredentialsError();
        }
        throw err;
      }
    },
    onSuccess: (result) => {
      // AuthService derives the display profile from the ID token, so just
      // hand it the tokens.
      auth.login({
        accessToken: result.AccessToken,
        refreshToken: result.RefreshToken,
        idToken: result.IdToken,
      });
      navigate("/");
    },
  });
}
