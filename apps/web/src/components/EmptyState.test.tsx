import { render, screen } from "@testing-library/react";
import { Inbox } from "lucide-react";
import { describe, expect, it } from "vitest";
import EmptyState from "./EmptyState";

describe("EmptyState", () => {
  it("renders the message", () => {
    render(<EmptyState icon={Inbox} message="Nenhum lançamento" />);

    expect(screen.getByText("Nenhum lançamento")).toBeInTheDocument();
  });

  it("renders the icon", () => {
    const { container } = render(
      <EmptyState icon={Inbox} message="Nenhum lançamento" />,
    );

    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("appends the caller's className to the wrapper", () => {
    const { container } = render(
      <EmptyState icon={Inbox} message="Vazio" className="mt-8" />,
    );

    expect(container.firstElementChild).toHaveClass("mt-8");
  });
});
