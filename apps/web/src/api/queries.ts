import { useMutation, useQueries, useQueryClient, useQuery } from '@tanstack/react-query'
import { api } from './client'
import type { Entry } from './client'
import { useToast } from '@/lib/toast'

export const queryKeys = {
  summaryMonthly: (month: string) => ['summary', 'monthly', month] as const,
  summaryCategories: (from?: string, to?: string) => ['summary', 'categories', from, to] as const,
  cashflow: (days: number) => ['summary', 'cashflow', days] as const,
  entries: (from: string, to: string) => ['entries', from, to] as const,
  goal: (month: string) => ['goal', month] as const,
}

export function useMonthlySummary(month: string) {
  return useQuery({
    queryKey: queryKeys.summaryMonthly(month),
    queryFn: () => api.summary.monthly(month),
  })
}

export function useMonthlyTrend(months: string[]) {
  return useQueries({
    queries: months.map(month => ({
      queryKey: queryKeys.summaryMonthly(month),
      queryFn: () => api.summary.monthly(month),
    })),
  })
}

export function useCategorySummary(from?: string, to?: string) {
  return useQuery({
    queryKey: queryKeys.summaryCategories(from, to),
    queryFn: () => api.summary.categories(from, to),
  })
}

export function useCashFlow(days = 30) {
  return useQuery({
    queryKey: queryKeys.cashflow(days),
    queryFn: () => api.summary.cashflow(days),
  })
}

export function useEntries(from: string, to: string) {
  return useQuery({
    queryKey: queryKeys.entries(from, to),
    queryFn: () => api.entries.list({ from, to }),
  })
}

export function useGoal(month: string) {
  return useQuery({
    queryKey: queryKeys.goal(month),
    queryFn: () => api.goals.get(month),
  })
}

type EntriesPage = { entries: Entry[]; count: number }

// Optimistically flips an entry to "paid" in the cached list, rolling back
// on failure; on settle, revalidates entries + every summary so KPIs/charts
// catch up.
export function useMarkPaidMutation(from: string, to: string) {
  const queryClient = useQueryClient()
  const notify = useToast()
  const key = queryKeys.entries(from, to)

  return useMutation({
    mutationFn: (entryID: string) => api.entries.update(entryID, { payment_status: 'paid' }),
    onMutate: async (entryID: string) => {
      await queryClient.cancelQueries({ queryKey: key })
      const previous = queryClient.getQueryData<EntriesPage>(key)
      queryClient.setQueryData<EntriesPage | undefined>(key, old =>
        old
          ? { ...old, entries: old.entries.map(e => (e.EntryID === entryID ? { ...e, PaymentStatus: 'paid' as const } : e)) }
          : old,
      )
      return { previous }
    },
    onError: (_err, _entryID, context) => {
      if (context?.previous) queryClient.setQueryData(key, context.previous)
      notify('Não foi possível marcar como pago.', 'error')
    },
    onSuccess: () => {
      notify('Transação marcada como paga.', 'success')
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['summary'] })
      queryClient.invalidateQueries({ queryKey: key })
    },
  })
}
