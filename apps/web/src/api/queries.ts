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
import type { CreateEntryInput, Entry, NotificationPrefs } from "./types";
import { api } from "./http";
import { useAuth } from "@/lib/auth";

export const queryKeys = {
  summaryMonthly: (month: string) => ["summary", "monthly", month] as const,
  summaryCategories: (from?: string, to?: string) =>
    ["summary", "categories", from, to] as const,
  cashflow: (month: string) => ["summary", "cashflow", month] as const,
  entries: (from: string, to: string) => ["entries", from, to] as const,
  entriesByMonth: () => ["entries", "byMonth"] as const,
  goal: (month: string) => ["goal", month] as const,
  notificationPrefs: () => ["notifications", "preferences"] as const,
  categories: () => ["categories"] as const,
  paymentsSales: (from: string, to: string) =>
    ["payments", "sales", from, to] as const,
  paymentsReceivables: (from: string, to: string) =>
    ["payments", "receivables", from, to] as const,
  paymentsForecast: (month: string) =>
    ["payments", "forecast", month] as const,
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
  const currentMonth = format(new Date(), "yyyy-MM");

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
    },
  });
}

export function useNotificationPrefs() {
  return useQuery({
    queryKey: queryKeys.notificationPrefs(),
    queryFn: () => api.notifications.getPreferences(),
    select: (data) => data.preferences,
  });
}

export function useSaveNotificationPrefsMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: Partial<NotificationPrefs>) =>
      api.notifications.savePreferences(data),
    onError: () => {
      toast.error("Não foi possível salvar as preferências.");
    },
    onSuccess: (result) => {
      queryClient.setQueryData(queryKeys.notificationPrefs(), result);
      toast.success("Preferências salvas.");
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

export function useMarkPaidMutation() {
  const queryClient = useQueryClient();
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
      toast.error("Não foi possível marcar como pago.");
    },
    onSuccess: () => {
      toast.success("Transação marcada como paga.");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["summary"] });
      queryClient.invalidateQueries(entriesKey);
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
    },
  });
}

export function useDeleteEntryMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => api.entries.delete(id),
    onSuccess: () => {
      toast.success("Transação excluída.");
    },
    onError: () => {
      toast.error("Não foi possível excluir a transação.");
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
