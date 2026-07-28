import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Analysis as AnalysisData } from "@/api/types";
import { normalizeSpaces } from "@/test/factories";

const useMonthlyAnalysis = vi.hoisted(() => vi.fn());
vi.mock("../hooks/useMonthlyAnalysis", () => ({ useMonthlyAnalysis }));

import Analysis from "./Analysis";

/**
 * A month behind its goal: R$27.775,00 of a R$36.000,00 target with five days
 * left, projected to land at R$33.705,01. Those are the numbers from the
 * report that started this — the page printed two different daily targets for
 * them at once.
 */
function analysisData(overrides: Partial<AnalysisData> = {}): AnalysisData {
  return {
    month: "2026-07",
    kpis: {
      resultado: 900000,
      receita: 3600000,
      despesa: 2700000,
      daysRemaining: 5,
      previousMonthIncomeUpToDay: 0,
    },
    health: {
      status: "atencao",
      score: 70,
      messages: [
        {
          type: "goal_behind",
          severity: "warning",
          title: "Precisa acelerar para bater a meta",
          description: "Necessário R$ 1.645,00/dia nos próximos 5 dias",
        },
      ],
    },
    trends: {
      receita: { current: 0, previous: 0, change: 0, direction: "stable" },
      despesa: { current: 0, previous: 0, change: 0, direction: "stable" },
      resultado: { current: 0, previous: 0, change: 0, direction: "stable" },
      comparedThroughDay: 26,
    },
    weekdays: [],
    weekComparison: {
      current: 524604,
      previous: 813240,
      previousUpToDay: 813240,
      projectedWeekly: 524604,
      monthlyTarget: 3600000,
      labels: [],
    },
    highlights: {
      bestIncome: { date: "—", label: "Sem dados", amount: 0 },
      worstIncome: { date: "—", label: "Sem dados", amount: 0 },
      bestBalance: { date: "—", label: "Sem dados", amount: 0 },
      worstBalance: { date: "—", label: "Sem dados", amount: 0 },
    },
    cashOutDays: [],
    expenseComposition: [],
    goals: {
      incomeTarget: 3600000,
      incomeActual: 2777500,
      incomePct: 77,
      expenseTarget: 0,
      expenseActual: 2700000,
      expensePct: 0,
      daysRemaining: 5,
      daysTotal: 31,
    },
    projection: {
      actual: 2777500,
      remaining: 593001,
      projected: 3370501,
      target: 3600000,
      gap: 229499,
      onTrack: false,
      daysRemaining: 5,
      neededPerDay: 164500,
    },
    history: [],
    cashPosition: {
      currentBalance: 3031155,
      endOfMonthProjection: 2810655,
      daysUntilNegative: null,
      lowestProjected: 2810655,
      lowestProjectedDate: "2026-07-31",
    },
    recommendations: [
      {
        severity: "danger",
        title: "Faturamento caiu e não bate a meta",
        message:
          "Precisa de R$ 1.645,00/dia nos próximos 5 dias para atingir a meta do mês.",
      },
      {
        severity: "warning",
        title: "Despesas acima do normal",
        message: "Cresceram 40% vs mês passado. Revise gastos.",
      },
    ],
    ...overrides,
  } as AnalysisData;
}

function renderWith(data: AnalysisData) {
  useMonthlyAnalysis.mockReturnValue({
    data,
    isError: false,
    refetch: vi.fn(),
  });
  return render(<Analysis />);
}

describe("Analysis page", () => {
  it("quotes the backend's per-day ask instead of deriving a second one", () => {
    const data = analysisData();
    const { container } = renderWith(data);

    // The card used to divide the shortfall left after its own projection
    // (R$459,00 here) while the insight above it quoted the shortfall from
    // real faturamento — two daily targets on one screen.
    expect(screen.getByText(/Necessário por dia/)).toBeInTheDocument();
    expect(screen.getByText("R$ 1.645,00")).toBeInTheDocument();
    expect(normalizeSpaces(container.textContent ?? "")).not.toContain(
      "R$ 459,00",
    );
  });

  it("labels the per-day ask by every day left, not business days", () => {
    renderWith(analysisData());

    expect(screen.getByText("Necessário por dia (5 dias)")).toBeInTheDocument();
    expect(screen.queryByText(/dia útil/)).not.toBeInTheDocument();
  });

  it("prints the weekly recommendation once", () => {
    renderWith(analysisData());

    // It captions the week-comparison card; the recommendation list carries
    // the rest. Rendering both put the same sentence on screen twice.
    expect(
      screen.getAllByText("Faturamento caiu e não bate a meta"),
    ).toHaveLength(1);
    expect(screen.getByText("Despesas acima do normal")).toBeInTheDocument();
  });

  it("keeps the page heading through loading and failure", () => {
    useMonthlyAnalysis.mockReturnValue({
      data: undefined,
      isError: false,
      refetch: vi.fn(),
    });
    const { rerender } = render(<Analysis />);
    expect(screen.getByRole("heading", { name: "Análise" })).toBeInTheDocument();

    useMonthlyAnalysis.mockReturnValue({
      data: undefined,
      isError: true,
      refetch: vi.fn(),
    });
    rerender(<Analysis />);
    expect(screen.getByRole("heading", { name: "Análise" })).toBeInTheDocument();
    expect(
      screen.getByText("Não foi possível carregar a análise"),
    ).toBeInTheDocument();
  });

  it("shows the health score the backend computed", () => {
    renderWith(analysisData());

    // Not the share of insights that were informational: that one message is
    // a warning, which the old frontend formula would have scored 0.
    expect(screen.getByText("70")).toBeInTheDocument();
  });

  it("keeps every section on the page and states its empty case", () => {
    const data = analysisData();
    data.projection = { ...data.projection, target: 0, neededPerDay: 0 };
    data.recommendations = [];
    data.expenseComposition = [];
    data.cashOutDays = [];
    data.weekdays = [];

    renderWith(data);

    // Sections used to vanish when they had nothing to show, so the page came
    // back a different shape on every visit and "nothing to say" looked the
    // same as "failed to load".
    for (const title of [
      "Projeção do Mês",
      "Recomendações",
      "Composição de Despesas",
      "Dias com Maior Saída de Caixa",
      "Média por Dia da Semana",
    ]) {
      expect(screen.getByText(title)).toBeInTheDocument();
    }
    expect(screen.getByText(/Defina uma meta de faturamento/)).toBeInTheDocument();
    expect(screen.getByText(/Nada a ajustar por enquanto/)).toBeInTheDocument();
  });

  it("does not invent a percentage against a month that never traded", () => {
    const data = analysisData();
    data.kpis = { ...data.kpis, previousMonthIncomeUpToDay: 0 };
    data.weekComparison = { ...data.weekComparison, previousUpToDay: 0 };
    // The backend drops its own month-over-month copy without a baseline
    // (hasBaseline); only the weekly recommendation survives.
    data.recommendations = data.recommendations.slice(0, 1);

    const { container } = renderWith(data);

    expect(
      screen.getByText("Sem faturamento no mês passado para comparar"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Sem vendas na semana passada para comparar"),
    ).toBeInTheDocument();
    // The KPI cards read the same empty baseline: the backend reports a
    // previous of zero as a flat 100% rise, which they used to print as a real
    // month-over-month move.
    expect(screen.getAllByText("Sem base no mês passado")).toHaveLength(3);
    expect(container.textContent).not.toContain("% vs mês passado");
  });
});
