import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQueryClient,
  useQuery,
} from "@tanstack/react-query";
import { api, CognitoAuthError } from "./client";
import type { CreateEntryInput, Entry } from "./client";
import { useToast } from "@/lib/toast";

export const queryKeys = {
  summaryMonthly: (month: string) => ["summary", "monthly", month] as const,
  summaryCategories: (from?: string, to?: string) =>
    ["summary", "categories", from, to] as const,
  cashflow: (month: string) => ["summary", "cashflow", month] as const,
  entries: (from: string, to: string) => ["entries", from, to] as const,
  entriesPaged: (pageSize: number) => ["entries", "paged", pageSize] as const,
  goal: (month: string) => ["goal", month] as const,
};

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

// Cursor-paginated entry history for the Transações page, which browses/
// searches across many months rather than one. Each page asks the server for
// at most `pageSize` entries (server-side `limit`, capped independently —
// see entries.go) older than the previous page's oldest entry, instead of
// pulling the whole table into the browser in one shot.
export function useEntriesInfinite(pageSize = 50) {
  return useInfiniteQuery({
    queryKey: queryKeys.entriesPaged(pageSize),
    queryFn: ({ pageParam }: { pageParam?: string }) =>
      api.entries.list({
        limit: String(pageSize),
        ...(pageParam ? { to: pageParam } : {}),
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => {
      if (lastPage.entries.length < pageSize) return undefined;
      const oldest = lastPage.entries[lastPage.entries.length - 1];
      const cursor = (oldest.DueDate || oldest.Date).slice(0, 10);
      const d = new Date(cursor + "T00:00:00");
      d.setDate(d.getDate() - 1);
      return d.toISOString().slice(0, 10);
    },
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
  const notify = useToast();

  return useMutation({
    mutationFn: (data: { revenue_target?: number; expense_target?: number }) =>
      api.goals.save(month, data),
    onError: () => {
      notify("Não foi possível salvar a meta.", "error");
    },
    onSuccess: () => {
      notify("Meta salva.", "success");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.goal(month) });
    },
  });
}

type EntriesPage = { entries: Entry[]; count: number };
type InfiniteEntriesData = { pages: EntriesPage[]; pageParams: unknown[] };

function markEntryPaid<T extends EntriesPage | InfiniteEntriesData>(
  old: T,
  entryID: string,
): T {
  const flip = (e: Entry) =>
    e.EntryID === entryID ? { ...e, PaymentStatus: "paid" as const } : e;

  if ("pages" in old) {
    return {
      ...old,
      pages: old.pages.map((p) => ({ ...p, entries: p.entries.map(flip) })),
    };
  }
  return { ...old, entries: old.entries.map(flip) };
}

// Optimistically flips an entry to "paid" across every cached entries query
// (both the ranged `useEntries` shape and the cursor-paginated
// `useEntriesInfinite` shape used by the Transações page), rolling back on
// failure; on settle, revalidates entries + every summary so KPIs/charts
// catch up.
export function useMarkPaidMutation() {
  const queryClient = useQueryClient();
  const notify = useToast();
  const entriesKey = { queryKey: ["entries"] };

  return useMutation({
    mutationFn: (entryID: string) =>
      api.entries.update(entryID, { payment_status: "paid" }),
    onMutate: async (entryID: string) => {
      await queryClient.cancelQueries(entriesKey);
      const previous = queryClient.getQueriesData<
        EntriesPage | InfiniteEntriesData
      >(entriesKey);
      queryClient.setQueriesData<EntriesPage | InfiniteEntriesData | undefined>(
        entriesKey,
        (old) => (old ? markEntryPaid(old, entryID) : old),
      );
      return { previous };
    },
    onError: (_err, _entryID, context) => {
      context?.previous?.forEach(([key, data]) => {
        queryClient.setQueryData(key, data);
      });
      notify("Não foi possível marcar como pago.", "error");
    },
    onSuccess: () => {
      notify("Transação marcada como paga.", "success");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["summary"] });
      queryClient.invalidateQueries(entriesKey);
    },
  });
}

// Creates a new manual entry (the "Nova Transação" form), then revalidates
// entries + every summary so KPIs/charts/tables catch up.
export function useCreateEntryMutation() {
  const queryClient = useQueryClient();
  const notify = useToast();

  return useMutation({
    mutationFn: (data: CreateEntryInput) => api.entries.create(data),
    onError: () => {
      notify("Não foi possível registrar a transação.", "error");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["entries"] });
      queryClient.invalidateQueries({ queryKey: ["summary"] });
    },
  });
}

export class InvalidCredentialsError extends Error {}
type LoginRequest = {
  email: string;
  password: string;
};

export function useLoginMutation() {
  return useMutation({
    mutationFn: async ({ email, password }: LoginRequest) => {
      try {
        await api.auth.login(email, password);
      } catch (err) {
        if (
          err instanceof CognitoAuthError &&
          (err.type === "NotAuthorizedException" ||
            err.type === "UserNotFoundException")
        ) {
          throw new InvalidCredentialsError();
        }

        throw err;
      }
    },
  });
}
