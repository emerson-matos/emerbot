import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { makeEntry } from "@/test/factories";
import {
  bucketByUrgency,
  currentMonthKey,
  editEntryPath,
  effectiveDate,
  formatEffectiveDate,
  formatPaidAt,
  netAmount,
  todayISO,
} from "./entries";

describe("todayISO/currentMonthKey", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  /**
   * The instant below is 2026-08-03T02:30:00Z — already the 3rd in UTC, but
   * still 2026-08-02 23:30 in America/Sao_Paulo (UTC-3). A naive
   * `new Date()` + browser-local formatting run in a UTC (or UTC-ahead)
   * browser would report "today" as the 3rd; the pharmacy's actual calendar
   * day at that instant is still the 2nd.
   */
  it("resolves to the pharmacy's calendar day, not UTC's", () => {
    vi.setSystemTime(new Date("2026-08-03T02:30:00Z"));
    expect(todayISO()).toBe("2026-08-02");
    expect(currentMonthKey()).toBe("2026-08");
  });

  it("still returns the same day once São Paulo has also rolled over", () => {
    vi.setSystemTime(new Date("2026-08-03T04:00:00Z"));
    expect(todayISO()).toBe("2026-08-03");
  });
});

describe("editEntryPath", () => {
  /**
   * The transaction date is in the path because it is half the entry's address
   * in the API — an id alone does not identify a row. A link built without it
   * points at a route that does not exist.
   */
  it("addresses an entry by its transaction date and id", () => {
    const entry = makeEntry({ EntryID: "01J", TransactionDate: "2026-07-10" });
    expect(editEntryPath(entry)).toBe("/transacoes/2026-07-10/01J/editar");
  });
});

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

  it("puts the nearest due date first in upcoming", () => {
    const far = makeEntry({ EntryID: "far", DueDate: "2026-03-30" });
    const near = makeEntry({ EntryID: "near", DueDate: "2026-02-10" });

    const buckets = bucketByUrgency([far, near], today);

    expect(buckets.upcoming.map((e) => e.EntryID)).toEqual(["near", "far"]);
  });

  /**
   * The bucket used to inherit the API's newest-first order, which put the
   * entry that had been unpaid the shortest time at the top and buried the
   * oldest debt at the bottom — backwards for a list titled "Em atraso".
   */
  it("puts the oldest debt first in overdue", () => {
    const recent = makeEntry({ EntryID: "recent", DueDate: "2026-02-04", PaymentStatus: "pending" });
    const ancient = makeEntry({ EntryID: "ancient", DueDate: "2025-11-02", PaymentStatus: "pending" });

    const buckets = bucketByUrgency([recent, ancient], today);

    expect(buckets.overdue.map((e) => e.EntryID)).toEqual(["ancient", "recent"]);
  });

  it("puts the most recent first in history", () => {
    const older = makeEntry({ EntryID: "older", DueDate: "2026-01-02", PaymentStatus: "paid" });
    const newer = makeEntry({ EntryID: "newer", DueDate: "2026-02-02", PaymentStatus: "paid" });

    const buckets = bucketByUrgency([older, newer], today);

    expect(buckets.history.map((e) => e.EntryID)).toEqual(["newer", "older"]);
  });

  it("breaks a shared date on the larger amount", () => {
    const small = makeEntry({ EntryID: "small", DueDate: today, Amount: 1000 });
    const large = makeEntry({ EntryID: "large", DueDate: today, Amount: 90000 });

    const buckets = bucketByUrgency([small, large], today);

    expect(buckets.dueToday.map((e) => e.EntryID)).toEqual(["large", "small"]);
  });

  /**
   * The order has to be total, not merely sorted: dates repeat constantly, and
   * an order that leaves ties free to move reshuffles the list on every
   * refetch. Same entries in, same order out, whatever order they arrive in.
   */
  it("does not depend on the order the entries arrive in", () => {
    const entries = [
      makeEntry({ EntryID: "a", DueDate: today, Amount: 500 }),
      makeEntry({ EntryID: "b", DueDate: today, Amount: 500 }),
      makeEntry({ EntryID: "c", DueDate: today, Amount: 500 }),
      makeEntry({ EntryID: "d", DueDate: "2026-02-20" }),
      makeEntry({ EntryID: "e", DueDate: "2026-01-02", PaymentStatus: "pending" }),
    ];
    const ids = (list: ReturnType<typeof bucketByUrgency>) => [
      ...list.overdue,
      ...list.dueToday,
      ...list.upcoming,
      ...list.history,
    ].map((e) => e.EntryID);

    const forwards = ids(bucketByUrgency(entries, today));
    const backwards = ids(bucketByUrgency([...entries].reverse(), today));

    expect(backwards).toEqual(forwards);
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
