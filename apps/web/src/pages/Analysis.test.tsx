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
    period: {
      throughDay: 25,
      // Whole weeks: 25 closed days is three of them.
      comparableThroughDay: 21,
      daysRemaining: 6,
      daysTotal: 31,
      inProgress: true,
    },
    kpis: {
      resultado: 900000,
      faturamento: 3600000,
      despesa: 2700000,
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
      faturamento: { current: 0, previous: 0, change: 0, direction: "stable" },
      despesa: { current: 0, previous: 0, change: 0, direction: "stable" },
      resultado: { current: 0, previous: 0, change: 0, direction: "stable" },
    },
    weekdays: [],
    weekComparison: {
      current: 524604,
      previous: 813240,
      pace: { current: 524604, previous: 813240, days: 3 },
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
      revenueTarget: 3600000,
      revenueActual: 2777500,
      revenuePct: 77,
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
      basis: "janela",
    },
    history: [],
    cashPosition: {
      currentBalance: 3031155,
      endOfMonthProjection: 2810655,
      daysUntilNegative: null,
      lowestProjected: 2810655,
      lowestProjectedDate: "2026-07-31",
      expectsReceipts: true,
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

  it("says what the projection is built from", () => {
    const { rerender } = renderWith(analysisData());

    // The projection used to be a bare figure. It is an estimate off a
    // trailing window, and the card has to say so — the same number means
    // something different when it stands on two days than on eight weeks.
    expect(
      screen.getByText("Pela média de cada dia da semana nas últimas 8 semanas."),
    ).toBeInTheDocument();

    const thin = analysisData();
    thin.projection = { ...thin.projection, basis: "parcial" };
    useMonthlyAnalysis.mockReturnValue({ data: thin, isError: false, refetch: vi.fn() });
    rerender(<Analysis />);

    expect(
      screen.getByText(/Menos de uma semana de vendas registradas/),
    ).toBeInTheDocument();
  });

  it("does not caption a closed month as a forecast", () => {
    // A month that has ended projected nothing: `projected` is its own
    // faturamento. Captioning it "pela média das últimas 8 semanas" put an
    // estimate label on the one figure on this card that is not an estimate.
    const closed = analysisData();
    closed.projection = { ...closed.projection, basis: "fechado" };
    const { container } = renderWith(closed);

    expect(normalizeSpaces(container.textContent ?? "")).not.toContain(
      "últimas 8 semanas",
    );
  });

  it("labels the per-day ask by every day left, not business days", () => {
    renderWith(analysisData());

    expect(screen.getByText("Necessário por dia (5 dias, hoje incluído)")).toBeInTheDocument();
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
    data.weekComparison = {
      ...data.weekComparison,
      pace: { current: 0, previous: 0, days: 3 },
    };
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

describe("Analysis in the opening week of a month", () => {
  // Days have closed, but not a whole week of them, so the backend has no
  // like-for-like window: the 1st and 2nd of August are a Saturday and a
  // Sunday, the 1st and 2nd of July a Wednesday and a Thursday. It zeroes both
  // sides and reports comparableThroughDay 0. The page used to render
  // "↓ 22% vs mês passado até o dia 2" for a pharmacy trading exactly as it had
  // the month before.
  function thirdOfMonth() {
    const data = analysisData();
    data.period = {
      throughDay: 2,
      comparableThroughDay: 0,
      daysRemaining: 29,
      daysTotal: 31,
      inProgress: true,
    };
    data.trends = {
      faturamento: { current: 0, previous: 0, change: 0, direction: "stable" },
      despesa: { current: 0, previous: 0, change: 0, direction: "stable" },
      resultado: { current: 0, previous: 0, change: 0, direction: "stable" },
    };
    data.recommendations = data.recommendations.slice(0, 1);
    return data;
  }

  it("says the week has not closed instead of drawing a fall", () => {
    const { container } = renderWith(thirdOfMonth());

    expect(screen.getAllByText("Primeira semana — sem base para comparar")).toHaveLength(3);
    expect(
      screen.getByText(
        "A primeira semana do mês ainda não fechou — a comparação com o mês passado começa no dia 8.",
      ),
    ).toBeInTheDocument();
    // And not the first-day copy: two days really have closed.
    expect(container.textContent).not.toContain("ainda não há dia fechado");
    expect(container.textContent).not.toContain("% vs mês passado");
  });
});

describe("the cash position card", () => {
  it("says the runway counts an ordinary day's receipts", () => {
    renderWith(analysisData());

    expect(
      screen.getByText("Contando o recebimento médio de cada dia da semana nos dias que faltam."),
    ).toBeInTheDocument();
  });

  // Without trading history the days ahead are priced at nothing, so the curve
  // is the month's bills against none of its sales. Announcing a balance about
  // to go negative off that is the alarm this guard exists to stop.
  it("makes no runway claim without trading history", () => {
    const data = analysisData();
    data.cashPosition = { ...data.cashPosition, expectsReceipts: false, daysUntilNegative: 1 };

    const { container } = renderWith(data);

    expect(container.textContent).not.toContain("Saldo fica negativo");
    expect(
      screen.getByText(/Sem histórico de recebimento para projetar/),
    ).toBeInTheDocument();
  });
});

describe("Analysis on the first day of a month", () => {
  // The month has not had a finished day, so the backend zeroes every
  // retrospective figure and reports throughDay 0. The page must say the month
  // is starting rather than draw a fall to zero — this used to render "↓ 100%
  // vs mês passado até o dia 1" on every 1st.
  function firstOfMonth() {
    const data = analysisData();
    data.period = { throughDay: 0, comparableThroughDay: 0, daysRemaining: 31, daysTotal: 31, inProgress: true };
    data.trends = {
      faturamento: { current: 0, previous: 0, change: 0, direction: "stable" },
      despesa: { current: 0, previous: 0, change: 0, direction: "stable" },
      resultado: { current: 0, previous: 0, change: 0, direction: "stable" },
    };
    data.weekComparison = {
      ...data.weekComparison,
      pace: { current: 0, previous: 0, days: 0 },
    };
    // Without a finished day the backend has no baseline, so its own
    // month-over-month recommendations never fire; only the weekly pace one,
    // which is about what is still ahead, survives.
    data.recommendations = data.recommendations.slice(0, 1);
    return data;
  }

  it("says the month is starting instead of reporting a collapse", () => {
    const { container } = renderWith(firstOfMonth());

    expect(screen.getAllByText("Mês começando — sem dia fechado")).toHaveLength(3);
    expect(
      screen.getByText("O mês está começando — ainda não há dia fechado para comparar."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("A semana está começando — nenhum dia fechado ainda"),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("% vs mês passado");
    expect(container.textContent).not.toContain("% vs semana anterior");
  });
});
