import { describe, expect, it } from "vitest";
import { normalizeSpaces } from "@/test/factories";
import { formatBRL, formatSignedBRL } from "./format";

// pt-BR puts U+00A0 between "R$" and the digits; normalize so the expected
// strings below are plain ASCII and stay legible in a diff.
const brl = (centavos: number) => normalizeSpaces(formatBRL(centavos));
const signed = (centavos: number) => normalizeSpaces(formatSignedBRL(centavos));

describe("formatBRL", () => {
  it("converts centavos to reais with two decimals", () => {
    expect(brl(12345)).toBe("R$ 123,45");
  });

  it("formats zero", () => {
    expect(brl(0)).toBe("R$ 0,00");
  });

  it("keeps the minus sign for negative values", () => {
    expect(brl(-500)).toBe("-R$ 5,00");
  });

  it("groups thousands with a dot", () => {
    expect(brl(123456789)).toBe("R$ 1.234.567,89");
  });

  it("rounds sub-centavo fractions", () => {
    expect(brl(1050.4)).toBe("R$ 10,50");
  });
});

describe("formatSignedBRL", () => {
  it("prefixes positive values with a plus sign", () => {
    expect(signed(1000)).toBe("+ R$ 10,00");
  });

  it("treats zero as positive", () => {
    expect(signed(0)).toBe("+ R$ 0,00");
  });

  // The sign is rendered by the helper, so the amount itself is formatted
  // from its absolute value — no double negative.
  it("uses a minus sign and the absolute value for negatives", () => {
    expect(signed(-1000)).toBe("− R$ 10,00");
  });
});
