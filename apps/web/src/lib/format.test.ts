import { describe, expect, it } from "vitest";
import { normalizeSpaces } from "@/test/factories";
import { formatBRL, formatSignedBRL, parseAmountToCents } from "./format";

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

describe("parseAmountToCents", () => {
  it("reads a whole number of reais", () => {
    expect(parseAmountToCents("1234")).toBe(123400);
  });

  it("reads the pt-BR comma as the decimal separator", () => {
    expect(parseAmountToCents("1234,56")).toBe(123456);
  });

  it("reads the numeric-keypad dot as the decimal separator", () => {
    expect(parseAmountToCents("1234.56")).toBe(123456);
  });

  it("pads a single decimal digit", () => {
    expect(parseAmountToCents("10,5")).toBe(1050);
  });

  // "1.234" is a thousands group in pt-BR, not R$ 1,23 — the page is pt-BR, so
  // a dot before three digits groups instead of separating decimals.
  it("treats a dotted group of three as thousands", () => {
    expect(parseAmountToCents("1.234")).toBe(123400);
    expect(parseAmountToCents("1.234,56")).toBe(123456);
    expect(parseAmountToCents("1.234.567,89")).toBe(123456789);
  });

  it("accepts comma grouping when a dotted decimal part disambiguates it", () => {
    expect(parseAmountToCents("1,234.56")).toBe(123456);
  });

  it("ignores the currency symbol and surrounding spaces", () => {
    expect(parseAmountToCents(" R$ 99,90 ")).toBe(9990);
  });

  it("returns null for an empty field", () => {
    expect(parseAmountToCents("")).toBeNull();
    expect(parseAmountToCents("   ")).toBeNull();
  });

  // A malformed filter must not read as zero: the caller shows the field as
  // invalid rather than silently filtering everything out.
  it("returns null for values it cannot parse", () => {
    expect(parseAmountToCents("abc")).toBeNull();
    expect(parseAmountToCents("12,")).toBeNull();
    expect(parseAmountToCents("1,2,3")).toBeNull();
    expect(parseAmountToCents("-50")).toBeNull();
  });

  // Ambiguous between R$ 10,555 (three decimals — malformed) and the en
  // reading of R$ 10.555,00. Rejecting beats guessing wrong by 1000x.
  it("returns null for comma grouping with no decimal part", () => {
    expect(parseAmountToCents("10,555")).toBeNull();
    expect(parseAmountToCents("1,234")).toBeNull();
  });
});
