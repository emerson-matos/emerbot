import { describe, expect, it } from "vitest";
import { cn } from "./utils";

describe("cn", () => {
  it("joins class names", () => {
    expect(cn("flex", "items-center")).toBe("flex items-center");
  });

  it("drops falsy values", () => {
    expect(cn("flex", false, undefined, null, "gap-2")).toBe("flex gap-2");
  });

  it("supports conditional objects and arrays", () => {
    expect(cn(["flex", { hidden: false, "gap-2": true }])).toBe("flex gap-2");
  });

  // The whole reason for tailwind-merge: the later utility in the same group
  // must win instead of both landing in the class list.
  it("lets a later Tailwind utility override an earlier one in the same group", () => {
    expect(cn("p-2", "p-4")).toBe("p-4");
    expect(cn("text-sm text-muted-foreground", "text-destructive")).toBe(
      "text-sm text-destructive",
    );
  });

  it("keeps utilities from different groups", () => {
    expect(cn("px-2", "py-4")).toBe("px-2 py-4");
  });

  it("returns an empty string for no input", () => {
    expect(cn()).toBe("");
  });
});
