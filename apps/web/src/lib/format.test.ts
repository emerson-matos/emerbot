import { describe, expect, it } from "vitest";
import { formatBRL, formatSignedBRL } from "./format";

describe("formatBRL", () => {
  it("formats positive centavos as BRL currency", () => {
    expect(formatBRL(12345)).toBe("R$ 123,45");
  });

  it("formats zero", () => {
    expect(formatBRL(0)).toBe("R$ 0,00");
  });

  it("formats negative centavos", () => {
    expect(formatBRL(-500)).toBe("-R$ 5,00");
  });
});

describe("formatSignedBRL", () => {
  it("prefixes positive values with a plus sign", () => {
    expect(formatSignedBRL(1000)).toBe("+ R$ 10,00");
  });

  it("prefixes negative values with a minus sign and formats the absolute value", () => {
    expect(formatSignedBRL(-1000)).toBe("− R$ 10,00");
  });
});
