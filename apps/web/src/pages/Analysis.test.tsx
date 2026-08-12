import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  Analysis as AnalysisData,
  ProjectionExperiment,
} from "@/api/types";
import { currentMonthKey } from "@/lib/entries";
import { normalizeSpaces } from "@/test/factories";

const useMonthlyAnalysis = vi.hoisted(() => vi.fn());
vi.mock("../hooks/useMonthlyAnalysis", () => ({ useMonthlyAnalysis }));

// ADR-030's experiment is a second query, and the page calls it on every
// render. Mocked at the hook, like the analysis above, so these tests stay
// about what the page prints rather than needing a QueryClientProvider.
const useProjectionExperiment = vi.hoisted(() => vi.fn());
vi.mock("../hooks/useProjectionExperiment", () => ({ useProjectionExperiment }));

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
      despesaAgendada: 0,
    },
    health: {
      status: "atencao",
      score: 70,
      messages: [
        {
          type: "goal_behind",
          severity: "warning",
          title: "Precisa acelerar para bater a meta",
          description: "A projeção indica fechamento abaixo da meta.",
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
      pace: { current: 524604, previous: 813240, days: 3, change: -35, direction: 'down' },
      projectedWeekly: 524604,
      monthlyTarget: 3600000,
      days: [],
    },
    highlights: {
      bestIncome: { date: "—", label: "Sem dados", amount: 0 },
      worstIncome: { date: "—", label: "Sem dados", amount: 0 },
      bestBalance: { date: "—", label: "Sem dados", amount: 0 },
      worstBalance: { date: "—", label: "Sem dados", amount: 0 },
    },
    cashOutDays: [],
    expenseComposition: [],
    revenueComposition: [],
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
      // Meeting every ask lands on the goal; trading as usual lands short of it.
      plannedClose: 3600000,
      gap: 229499,
      onTrack: false,
      daysRemaining: 5,
      basis: "janela",
      // 3.370.501 / 3.600.000 — short of the goal, but not far short.
      coverage: 0.9362,
      status: "warning",
      todayTarget: {
        state: "ok",
        basis: "em_curso",
        date: "2026-07-27",
        day: 1,
        realized: 48200,
        historical: 111700,
        target: 121100,
        delta: 9400,
        deltaPercent: 0.084,
        factor: 1.084,
        status: "above",
        source: "plano",
      },
      // The distribution today's ask is one slice of. Every other day of the
      // month is priced from it — see the backend's get_meta_do_dia.
      plan: { state: "ok", factor: 1.084, gapFactor: 1.084 },
    },
    history: [],
    cashPosition: {
      currentBalance: 3031155,
      endOfMonthProjection: 2810655,
      daysUntilNegative: null,
      lowestProjected: 2810655,
      lowestProjectedDate: "2026-07-31",
      expectsReceipts: true,
      forecast: [
        { date: "2026-07-26", balance: 3050000, scheduledIn: 0, scheduledOut: 0, expectedIn: 0 },
        { date: "2026-07-27", balance: 3031155, scheduledIn: 0, scheduledOut: 20000, expectedIn: 0 },
        { date: "2026-07-28", balance: 2810655, scheduledIn: 0, scheduledOut: 330000, expectedIn: 109500 },
      ],
    },
    recommendations: [
      {
        severity: "danger",
        title: "Faturamento caiu e não bate a meta",
        message:
          "Precisa de R$ 1.645,00 nos próximos 5 dias para atingir a meta do mês.",
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

function experimentData(
  overrides: Partial<ProjectionExperiment> = {},
): ProjectionExperiment {
  return {
    current: {
      available: true,
      official: 3370501,
      experimental: 3210000,
      recentFactor: 0.95,
      observations: 21,
    },
    backtest: {
      samples: [
        {
          month: "2026-06",
          cutoffDay: 15,
          actualClose: 3400000,
          official: 3300000,
          experimental: 3350000,
        },
      ],
      officialMae: 100000,
      regimeMae: 50000,
      officialWins: 0,
      regimeWins: 1,
      weekdayErrors: [{ day: 1, mae: 12345, observations: 3 }],
    },
    ...overrides,
  };
}

// Every test renders the whole page, so the experiment query has to answer on
// all of them. Individual tests override it when the experiment is the subject.
beforeEach(() => {
  useProjectionExperiment.mockReturnValue({
    data: experimentData(),
    isPending: false,
    isError: false,
  });
});

function renderWith(data: AnalysisData) {
  useMonthlyAnalysis.mockReturnValue({
    data,
    isError: false,
    refetch: vi.fn(),
  });
  return render(<Analysis />);
}

describe("the experimental projection section", () => {
  it("states the backtest is empty instead of throwing on it", () => {
    // The backend sends no samples until a month is old enough to backtest,
    // which is the ordinary case for a pharmacy in its first months. The
    // section used to read samples.length off it and take the page down.
    useProjectionExperiment.mockReturnValue({
      data: experimentData({
        backtest: {
          samples: [],
          officialMae: 0,
          regimeMae: 0,
          officialWins: 0,
          regimeWins: 0,
          weekdayErrors: [],
        },
      }),
      isPending: false,
      isError: false,
    });

    // The section only renders for the month in progress, which is the only
    // month a forecast has anything to say about.
    renderWith(analysisData({ month: currentMonthKey() }));

    expect(
      screen.getByText(/Ainda não há histórico suficiente para o backtest/),
    ).toBeInTheDocument();
    // The rest of the page is still there — the empty backtest is a state, not
    // a failure of the analysis around it.
    expect(screen.getByText("A semana da farmácia")).toBeInTheDocument();
  });

  it("never presents the candidate as the official projection", () => {
    // The section only renders for the month in progress, which is the only
    // month a forecast has anything to say about.
    renderWith(analysisData({ month: currentMonthKey() }));

    expect(
      screen.getByText(/Não altera a projeção oficial/),
    ).toBeInTheDocument();
  });
});

describe("Analysis page", () => {
  it("quotes the backend's per-day ask instead of deriving a second one", () => {
    const data = analysisData();
    const { container } = renderWith(data);

    // The card used to divide the shortfall left after its own projection
    // (R$459,00 here) while the insight above it quoted the shortfall from
    // real faturamento — two daily targets on one screen.
    expect(screen.getByText(/Meta para hoje/)).toBeInTheDocument();
    expect(screen.getByText("R$ 1.211,00")).toBeInTheDocument();
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
      screen.getByText("Pela média de cada dia da semana nas últimas 8 semanas, com peso maior para as mais recentes."),
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
    renderWith(closed);

    expect(
      screen.queryByText("Pela média de cada dia da semana nas últimas 8 semanas, com peso maior para as mais recentes."),
    ).not.toBeInTheDocument();
  });

  it("shows what the day has taken beside what it is being asked for", () => {
    // The ask is a whole-day figure measured from the morning, so without the
    // day's own takings nobody can tell whether it was met. The fixture has
    // R$482,00 in against an ask of R$1.211,00.
    // The section only renders for the month in progress, which is the only
    // month a forecast has anything to say about.
    renderWith(analysisData({ month: currentMonthKey() }));

    expect(screen.getByText(/R\$ 482,00 vendidos até agora/)).toBeInTheDocument();
    expect(screen.queryByText(/Meta batida/)).not.toBeInTheDocument();
  });

  it("says the day's ask is met once the takings clear it", () => {
    const data = analysisData();
    data.projection = {
      ...data.projection,
      todayTarget: { ...data.projection.todayTarget, realized: 130000 },
    };
    renderWith(data);

    expect(screen.getByText(/Meta batida/)).toBeInTheDocument();
  });

  it("labels the per-day ask by every day left, not business days", () => {
    renderWith(analysisData());

    // When todayTarget.state is "ok", it shows "Meta para hoje" instead of a per-day ask
    expect(screen.getByText("Meta para hoje")).toBeInTheDocument();
    expect(screen.queryByText(/dia útil/)).not.toBeInTheDocument();
  });

  // A month running ahead is asked for its ordinary day, and the card says so
  // as a floor rather than as slack. This used to assert the opposite — "R$
  // 200,00 abaixo do esperado" — which is the page telling a pharmacy it may
  // sell less than an ordinary Monday (ADR-025).
  it("words an ask at the average as the rhythm, never as a lighter day", () => {
    const data = analysisData();
    data.projection = {
      ...data.projection,
      todayTarget: {
        ...data.projection.todayTarget,
        state: "ok",
        target: 111700,
        historical: 111700,
        delta: 0,
        deltaPercent: 0,
        factor: 1,
        status: "on_track",
        source: "media",
      },
    };
    renderWith(data);

    expect(
      screen.getByText("O esperado para o dia — o mês está no ritmo"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/abaixo do esperado/)).not.toBeInTheDocument();
  });

  // Same figures, different news: the goal being met used to arrive as an absent
  // target, indistinguishable from "não há histórico".
  it("says the goal is met when that is why the ask is only the average", () => {
    const data = analysisData();
    data.projection = {
      ...data.projection,
      todayTarget: {
        ...data.projection.todayTarget,
        target: 111700,
        historical: 111700,
        delta: 0,
        deltaPercent: 0,
        factor: 1,
        status: "on_track",
        source: "meta_batida",
      },
      plan: { state: "ok", factor: 1, gapFactor: 0 },
    };
    renderWith(data);

    expect(
      screen.getByText("Meta do mês já batida — hoje é manter o ritmo"),
    ).toBeInTheDocument();
  });

  // domingo and sábado are masculine; the sentence used to hardcode "uma".
  it("agrees with the gender of the weekday it names", () => {
    const data = analysisData();
    data.projection = {
      ...data.projection,
      todayTarget: { ...data.projection.todayTarget, day: 0 },
    };
    renderWith(data);

    expect(screen.getByText(/o esperado para um domingo/)).toBeInTheDocument();
  });

  // The card used to divide and colour by onTrack, so 97% of the goal showed a
  // red "97% da meta" under a green "Ritmo suficiente".
  it("reads the coverage and its verdict off the payload", () => {
    const data = analysisData();
    data.projection = { ...data.projection, coverage: 0.97, status: "success" };
    const { container } = renderWith(data);

    const line = screen.getByText("Equivale a 97% da meta");
    expect(line).toBeInTheDocument();
    expect(line.className).toContain("text-success");
    expect(container.textContent).not.toContain("Equivale a 94%");
  });

  // pace.current is accumulated over the finished days, so "Média diária"
  // printed three days of takings as one day's average.
  it("labels the week-to-date pace as a total, not a daily average", () => {
    const { container } = renderWith(analysisData());

    expect(screen.getByText("Ritmo até ontem (3 dias fechados)")).toBeInTheDocument();
    expect(container.textContent).not.toContain("Média diária");
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
    // A month with no goal has no target for today either: the backend says so
    // with state "sem_meta" rather than a bare absence (analytics/projection.go).
     data.projection = {
       ...data.projection,
       target: 0,
       todayTarget: { ...data.projection.todayTarget, state: "sem_meta" },
     };
    data.recommendations = [];
    data.expenseComposition = [];
    data.revenueComposition = [];
    data.cashOutDays = [];
    data.weekdays = [];

    renderWith(data);

    // Sections used to vanish when they had nothing to show, so the page came
    // back a different shape on every visit and "nothing to say" looked the
    // same as "failed to load".
    for (const title of [
      "Projeção do mês",
      "Insights do mês",
      "Composição de despesas",
      "Composição do faturamento",
      "Dias com maior saída de caixa",
      "A semana da farmácia",
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
      pace: { current: 0, previous: 0, days: 3, change: 0, direction: 'stable' },
    };
    // The backend drops its own month-over-month copy without a baseline
    // (hasBaseline); only the weekly recommendation survives.
    data.recommendations = data.recommendations.slice(0, 1);

    const { container } = renderWith(data);

    expect(
      screen.getByText("Sem vendas na semana passada para comparar"),
    ).toBeInTheDocument();
    // The KPI cards and the projection card read the same empty baseline
    // through the same label: the backend reports a previous of zero as a flat
    // 100% rise, which they used to print as a real month-over-month move. The
    // projection card is the fourth — it had a sentence of its own for the same
    // state until it started going through trendLabel like the rest.
    expect(screen.getAllByText("Sem base no mês passado")).toHaveLength(4);
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

describe("the KPI row", () => {
  // The cards used to read the whole month's booked expenses, so on the 3rd
  // "Despesa" showed every bill of August against two days of sales and
  // "Resultado" reported the month lost. The commitment is still shown — beside
  // the figure, never inside it.
  it("shows what is still to fall due beside the spend, not inside it", () => {
    const data = analysisData();
    data.kpis = { ...data.kpis, despesa: 20000, despesaAgendada: 1600000 };

    renderWith(data);

    expect(screen.getByText("R$ 200,00")).toBeInTheDocument();
    expect(screen.getByText("+ R$ 16.000,00 a vencer")).toBeInTheDocument();
  });

  it("says nothing about pending bills when there are none", () => {
    const { container } = renderWith(analysisData());

    expect(container.textContent).not.toContain("a vencer");
  });
});

describe("the cash position card", () => {
  it("says the runway counts an ordinary day's receipts", () => {
    // The section only renders for the month in progress, which is the only
    // month a forecast has anything to say about.
    renderWith(analysisData({ month: currentMonthKey() }));

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
      pace: { current: 0, previous: 0, days: 0, change: 0, direction: 'stable' },
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
