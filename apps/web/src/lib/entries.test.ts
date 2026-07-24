import { describe, expect, it } from "vitest";
import { makeEntry } from "@/test/factories";
import {
  bucketByUrgency,
  effectiveDate,
  formatEffectiveDate,
  formatPaidAt,
  netAmount,
} from "./entries";

describe("effectiveDate", () => {
  it("prefers the due date", () => {
    const entry = makeEntry({
      TransactionDate: "2026-02-01",
      DueDate: "2026-02-10",
    });

    expect(effectiveDate(entry)).toBe("2026-02-10");
  });

  it("falls back to the transaction date when nothing is due", () => {
    expect(effectiveDate(makeEntry({ TransactionDate: "2026-02-01" }))).toBe(
      "2026-02-01",
    );
  });

  it("returns null when neither date is present", () => {
    expect(
      effectiveDate(makeEntry({ TransactionDate: "", DueDate: null })),
    ).toBeNull();
  });
});

describe("formatEffectiveDate", () => {
  it("renders the effective date as dd/MM/yy", () => {
    expect(
      formatEffectiveDate(makeEntry({ TransactionDate: "2026-02-05" })),
    ).toBe("05/02/26");
  });

  it("renders an em dash when there is no date", () => {
    expect(
      formatEffectiveDate(makeEntry({ TransactionDate: "", DueDate: null })),
    ).toBe("—");
  });

  it("renders an em dash for an unparseable date", () => {
    expect(
      formatEffectiveDate(makeEntry({ TransactionDate: "not-a-date" })),
    ).toBe("—");
  });
});

describe("formatPaidAt", () => {
  it("renders the payment day when settled", () => {
    expect(formatPaidAt(makeEntry({ PaymentDate: "2026-02-05" }))).toBe(
      "em 05/02",
    );
  });

  it("renders nothing when unpaid", () => {
    expect(formatPaidAt(makeEntry({ PaymentDate: null }))).toBe("");
  });

  it("renders nothing for an unparseable payment date", () => {
    expect(formatPaidAt(makeEntry({ PaymentDate: "nope" }))).toBe("");
  });
});

describe("netAmount", () => {
  it("adds income and subtracts expenses", () => {
    const total = netAmount([
      makeEntry({ Type: "income", Amount: 5000 }),
      makeEntry({ Type: "expense", Amount: 2000 }),
      makeEntry({ Type: "expense", Amount: 500 }),
    ]);

    expect(total).toBe(2500);
  });

  it("can go negative", () => {
    expect(netAmount([makeEntry({ Type: "expense", Amount: 900 })])).toBe(-900);
  });

  it("is zero for no entries", () => {
    expect(netAmount([])).toBe(0);
  });
});

describe("bucketByUrgency", () => {
  const today = "2026-02-05";

  it("splits entries by their effective date relative to today", () => {
    const overdue = makeEntry({
      EntryID: "overdue",
      DueDate: "2026-02-01",
      PaymentStatus: "pending",
    });
    const dueToday = makeEntry({ EntryID: "today", DueDate: today });
    const upcoming = makeEntry({ EntryID: "later", DueDate: "2026-02-20" });
    const history = makeEntry({
      EntryID: "settled",
      DueDate: "2026-01-20",
      PaymentStatus: "paid",
    });

    const buckets = bucketByUrgency(
      [overdue, dueToday, upcoming, history],
      today,
    );

    expect(buckets.overdue).toEqual([overdue]);
    expect(buckets.dueToday).toEqual([dueToday]);
    expect(buckets.upcoming).toEqual([upcoming]);
    expect(buckets.history).toEqual([history]);
  });

  // A past-dated entry is only "overdue" if it was never settled; paid ones
  // are history so the caller can collapse them.
  it("keeps settled past entries out of overdue", () => {
    const buckets = bucketByUrgency(
      [
        makeEntry({ DueDate: "2026-01-10", PaymentStatus: "paid" }),
        makeEntry({ DueDate: "2026-01-11", PaymentStatus: "pending" }),
      ],
      today,
    );

    expect(buckets.overdue).toHaveLength(1);
    expect(buckets.history).toHaveLength(1);
  });

  it("buckets anything due today regardless of payment status", () => {
    const buckets = bucketByUrgency(
      [
        makeEntry({ DueDate: today, PaymentStatus: "paid" }),
        makeEntry({ DueDate: today, PaymentStatus: "pending" }),
      ],
      today,
    );

    expect(buckets.dueToday).toHaveLength(2);
    expect(buckets.overdue).toEqual([]);
    expect(buckets.history).toEqual([]);
  });

  // The API hands back newest-first, so upcoming is reversed to put the
  // nearest due date at the top of the table.
  it("reverses upcoming so the nearest due date comes first", () => {
    const far = makeEntry({ EntryID: "far", DueDate: "2026-03-30" });
    const near = makeEntry({ EntryID: "near", DueDate: "2026-02-10" });

    const buckets = bucketByUrgency([far, near], today);

    expect(buckets.upcoming.map((e) => e.EntryID)).toEqual(["near", "far"]);
  });

  it("treats an entry with no date as overdue when still pending", () => {
    const buckets = bucketByUrgency(
      [
        makeEntry({
          TransactionDate: "",
          DueDate: null,
          PaymentStatus: "pending",
        }),
      ],
      today,
    );

    expect(buckets.overdue).toHaveLength(1);
  });

  it("returns four empty buckets for no entries", () => {
    expect(bucketByUrgency([], today)).toEqual({
      overdue: [],
      dueToday: [],
      upcoming: [],
      history: [],
    });
  });
});
