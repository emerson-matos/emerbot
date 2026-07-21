import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQueryClient,
  useQuery,
} from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
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
  entriesPaged: (pageSize: number) => ["entries", "paged", pageSize] as const,
  goal: (month: string) => ["goal", month] as const,
  notificationPrefs: () => ["notifications", "preferences"] as const,
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

