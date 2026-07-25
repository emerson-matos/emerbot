import { useMemo, useRef } from "react";
import { format } from "date-fns";
import {
  useMonthlyTrend,
  useGoal,
  useEntries,
  useCashFlow,
} from "../api/queries";
import { buildMonthlyAnalysis } from "@/lib/analytics/build";
import type { YearMonth, Analysis } from "@/lib/analytics/types";

function getMonthOffset(month: string, offset: number): string {
  const [y, m] = month.split("-").map(Number);
  const date = new Date(y, m - 1 + offset, 1);
  return format(date, "yyyy-MM");
}

function useMonthlyEntries(month: string) {
  const [y, m] = month.split("-").map(Number);
  const from = format(new Date(y, m - 1, 1), "yyyy-MM-dd");
  const to = format(new Date(y, m, 0), "yyyy-MM-dd");
  return useEntries(from, to);
}

export function useMonthlyAnalysis(month: YearMonth): Analysis | undefined {
  const nowRef = useRef(new Date());
  const now = nowRef.current;

  const entriesQuery = useMonthlyEntries(month);
  const prevMonth = getMonthOffset(month, -1);
  const previousEntriesQuery = useMonthlyEntries(prevMonth);

  const months3 = [getMonthOffset(month, -2), getMonthOffset(month, -1), month];
  const summariesQueries = useMonthlyTrend(months3);
  const goal0Query = useGoal(months3[0]);
  const goal1Query = useGoal(months3[1]);
  const goal2Query = useGoal(months3[2]);
  const cashFlowQuery = useCashFlow(month);

  const isLoading =
    entriesQuery.isLoading ||
    previousEntriesQuery.isLoading ||
    summariesQueries.some((q) => q.isLoading) ||
    goal0Query.isLoading ||
    goal1Query.isLoading ||
    goal2Query.isLoading ||
    cashFlowQuery.isLoading;

  const entries = useMemo(
    () => entriesQuery.data?.entries ?? [],
    [entriesQuery.data?.entries],
  );

  const previousEntries = useMemo(
    () => previousEntriesQuery.data?.entries ?? [],
    [previousEntriesQuery.data?.entries],
  );

  const summaries = useMemo(
    () =>
      summariesQueries
        .map((q) => q.data)
        .filter(Boolean) as import("../api/types").MonthlySummary[],
    [summariesQueries],
  );

  const goals = useMemo(() => {
    return [goal0Query.data?.goal, goal1Query.data?.goal, goal2Query.data?.goal]
      .filter((g): g is NonNullable<typeof g> => g != null)
      .map((g) => ({
        revenueTarget: g.RevenueTarget,
        expenseTarget: g.ExpenseTarget,
      }));
  }, [goal0Query.data?.goal, goal1Query.data?.goal, goal2Query.data?.goal]);

  const cashFlowPoints = useMemo(
    () => cashFlowQuery.data?.points ?? [],
    [cashFlowQuery.data?.points],
  );

  const analysis = useMemo(() => {
    if (isLoading) return undefined;
    return buildMonthlyAnalysis({ month, entries, previousEntries, summaries, goals, cashFlowPoints, now });
  }, [isLoading, month, entries, previousEntries, summaries, goals, cashFlowPoints, now]);

  return analysis;
}
