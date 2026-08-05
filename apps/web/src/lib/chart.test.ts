import { describe, expect, it } from "vitest";
import { brlAxisTick } from "./chart";

describe("brlAxisTick", () => {
  it("keeps the fraction when the gridline is not on a whole thousand", () => {
    // Recharts picks ticks off the data: a week whose tallest bar is
    // R$ 8.500,00 gets 0 / 2.500 / 5.000 / 7.500 / 10.000. Rounded to whole
    // thousands that axis reads R$3k and R$8k — two labels naming a number the
    // gridline is not at.
    expect([0, 2500, 5000, 7500, 10000].map(brlAxisTick)).toEqual([
      "R$0k", "R$2,5k", "R$5k", "R$7,5k", "R$10k",
    ]);
  });

  it("drops a trailing zero rather than printing R$5,0k", () => {
    expect(brlAxisTick(36000)).toBe("R$36k");
  });

  it("keeps the sign outside the currency", () => {
    expect(brlAxisTick(-8500)).toBe("-R$8,5k");
  });
});
