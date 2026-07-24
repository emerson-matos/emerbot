import { cleanup, render, screen } from "@testing-library/react";
import { Wallet } from "lucide-react";
import { afterEach, describe, expect, it } from "vitest";
import KpiCard, { KpiCardActions, KpiCardContent, toneVar } from "./KpiCard";

afterEach(cleanup);

const accent = (container: HTMLElement) =>
  container.querySelector<HTMLElement>("[aria-hidden]");

describe("KpiCard", () => {
  it("renders its children in the resolved state", () => {
    render(
      <KpiCard>
        <KpiCardContent icon={Wallet} tone="positive">
          <p>Saldo</p>
        </KpiCardContent>
      </KpiCard>,
    );

    expect(screen.getByText("Saldo")).toBeTruthy();
  });

  it("paints the accent bar with the tone color", () => {
    const { container } = render(<KpiCard tone="positive">conteúdo</KpiCard>);

    expect(accent(container)?.style.background).toBe(toneVar.positive);
  });

  it("shows a skeleton instead of children while loading", () => {
    const { container } = render(
      <KpiCard isLoading>
        <p>Saldo</p>
      </KpiCard>,
    );

    expect(container.querySelector("[data-slot=skeleton]")).not.toBeNull();
    expect(screen.queryByText("Saldo")).toBeNull();
  });

  // Loading is a neutral state — the tone shouldn't leak through and imply
  // a result before there is one.
  it("uses the neutral accent while loading regardless of tone", () => {
    const { container } = render(<KpiCard isLoading tone="negative" />);

    expect(accent(container)?.style.background).toBe(toneVar.neutral);
  });

  it("shows the default error message", () => {
    render(<KpiCard isError />);

    expect(screen.getByText("Erro ao carregar")).toBeTruthy();
  });

  it("shows a custom error message", () => {
    render(<KpiCard isError errorMessage="Falha na rede" />);

    expect(screen.getByText("Falha na rede")).toBeTruthy();
    expect(screen.queryByText("Erro ao carregar")).toBeNull();
  });

  it("prefers the loading state over the error state", () => {
    const { container } = render(<KpiCard isLoading isError />);

    expect(container.querySelector("[data-slot=skeleton]")).not.toBeNull();
    expect(screen.queryByText("Erro ao carregar")).toBeNull();
  });

  // The actions row is reserved in the placeholder states so cards in a grid
  // keep the same height between loading and loaded.
  it("reserves the actions row when the resolved card has actions", () => {
    const { container } = render(
      <KpiCard isLoading>
        <KpiCardContent icon={Wallet} tone="neutral">
          <p>Saldo</p>
        </KpiCardContent>
        <KpiCardActions>
          <button type="button">Ver</button>
        </KpiCardActions>
      </KpiCard>,
    );

    // Placeholder keeps the row but drops its contents.
    expect(container.querySelector("[data-slot=skeleton]")).not.toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(
      container.querySelectorAll("[data-slot=card] > div").length,
    ).toBeGreaterThan(1);
  });

  it("does not reserve an actions row when there are none", () => {
    const { container } = render(
      <KpiCard isError>
        <KpiCardContent icon={Wallet} tone="neutral">
          <p>Saldo</p>
        </KpiCardContent>
      </KpiCard>,
    );

    expect(container.querySelectorAll("[data-slot=card] > div")).toHaveLength(1);
  });
});
