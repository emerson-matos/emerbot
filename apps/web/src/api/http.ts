import { authService } from "./auth-service";
import {
  ApiError,
  NetworkError,
  UnauthorizedError,
  ForbiddenError,
} from "./api-error";
import { cognitoInitiateAuth } from "./cognito";
import type {
  Analysis,
  Entry,
  CreateEntryInput,
  UpdateEntryInput,
  MonthlySummary,
  CategorySummary,
  CashFlowPoint,
  Goal,
  NotificationPrefs,
  Category,
  SalesResponse,
  ReceivablesResponse,
  ForecastResponse,
} from "./types";

export { CognitoAuthError } from "./cognito";
export type { CognitoAuthResult } from "./cognito";
export type {
  Entry,
  CreateEntryInput,
  UpdateEntryInput,
  MonthlySummary,
  CategorySummary,
  CashFlowPoint,
  Goal,
  NotificationPrefs,
  Category,
  Sale,
  ExpectedReceivable,
  SalesResponse,
  ReceivablesResponse,
  ForecastResponse,
  PaymentForecastPoint,
} from "./types";

const API_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8081";

/**
 * The address of one entry: /entries/<transactionDate>/<id>.
 *
 * An entry's row key is its transaction date plus its id, so the API cannot
 * find it from the id alone. That also means an edit which moves the date
 * moves the entry's address — read the new one off the response rather than
 * reusing the path you sent the request to.
 */
function entryPath(date: string, id: string): string {
  return `/entries/${encodeURIComponent(date)}/${encodeURIComponent(id)}`;
}

interface ApiOptions extends RequestInit {
  _retry?: boolean;
}

async function httpClient<T>(
  path: string,
  options: ApiOptions = {},
): Promise<T> {
  const tokens = authService.getTokens();
  const headers: HeadersInit = {
    "Content-Type": "application/json",
    ...(tokens?.idToken ? { Authorization: `Bearer ${tokens.idToken}` } : {}),
    ...(options.headers ?? {}),
  };

  let res: Response;
  try {
    res = await fetch(`${API_URL}${path}`, { ...options, headers });
  } catch (err) {
    if (err instanceof TypeError) {
      throw new NetworkError();
    }
    throw err;
  }

  if (res.status === 401) {
    if (!options._retry && (await authService.refresh())) {
      return httpClient<T>(path, { ...options, _retry: true });
    }
    throw new UnauthorizedError();
  }

  if (res.status === 403) {
    throw new ForbiddenError();
  }

  if (!res.ok) {
    const body = await res.json().catch(() => undefined);
    throw new ApiError(res.status, body);
  }

  if (res.status === 204) return {} as T;
  return res.json();
}

export const api = {
  auth: {
    login: async (email: string, password: string) => {
      const result = await cognitoInitiateAuth("USER_PASSWORD_AUTH", {
        USERNAME: email,
        PASSWORD: password,
      });
      return result;
    },
  },

  entries: {
    list: (params?: Record<string, string>) => {
      const qs = params ? "?" + new URLSearchParams(params).toString() : "";
      return httpClient<{ entries: Entry[]; count: number }>(`/entries${qs}`);
    },
    // One entry is addressed by its transaction date *and* its id: together
    // they are the row's key, so the date is not a filter here — it is half
    // the address. entryPath keeps every caller building it the same way.
    get: (date: string, id: string) => httpClient<Entry>(entryPath(date, id)),
    create: (data: CreateEntryInput) =>
      httpClient<Entry>("/entries", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (date: string, id: string, data: UpdateEntryInput) =>
      httpClient<Entry>(entryPath(date, id), {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (date: string, id: string) =>
      httpClient<void>(entryPath(date, id), { method: "DELETE" }),
  },

  summary: {
    monthly: (month?: string) => {
      const qs = month ? `?month=${month}` : "";
      return httpClient<MonthlySummary>(`/summary/monthly${qs}`);
    },
    categories: (from?: string, to?: string) => {
      const qs = new URLSearchParams();
      if (from) qs.set("from", from);
      if (to) qs.set("to", to);
      return httpClient<{ categories: CategorySummary[] }>(
        `/summary/categories?${qs}`,
      );
    },
    cashflow: (month: string) =>
      httpClient<{ points: CashFlowPoint[] }>(
        `/summary/cashflow?month=${month}`,
      ),
  },

  analysis: {
    // The backend assembles the whole analysis, so this is one call where the
    // page used to make five and build it in the browser.
    monthly: (month?: string) => {
      const qs = month ? `?month=${month}` : "";
      return httpClient<Analysis>(`/analysis/monthly${qs}`);
    },
  },

  categories: {
    list: () => httpClient<{ categories: Category[] }>("/categories"),
  },

  goals: {
    get: (month?: string) => {
      const qs = month ? `?month=${month}` : "";
      return httpClient<{ goal: Goal | null; month: string }>(`/goals${qs}`);
    },
    save: (
      month: string,
      data: { revenue_target?: number; expense_target?: number },
    ) =>
      httpClient<{ goal: Goal }>("/goals", {
        method: "PUT",
        body: JSON.stringify({ month, ...data }),
      }),
  },

  notifications: {
    getPreferences: () =>
      httpClient<{ preferences: NotificationPrefs }>(
        "/notifications/preferences",
      ),
    savePreferences: (data: Partial<NotificationPrefs>) =>
      httpClient<{ preferences: NotificationPrefs }>(
        "/notifications/preferences",
        {
          method: "PUT",
          body: JSON.stringify(data),
        },
      ),
  },

  // Imported payment-processor data (read-only; ingestion is out-of-band via S3).
  payments: {
    sales: (from?: string, to?: string) => {
      const qs = new URLSearchParams();
      if (from) qs.set("from", from);
      if (to) qs.set("to", to);
      return httpClient<SalesResponse>(`/payments/sales?${qs}`);
    },
    receivables: (from?: string, to?: string) => {
      const qs = new URLSearchParams();
      if (from) qs.set("from", from);
      if (to) qs.set("to", to);
      return httpClient<ReceivablesResponse>(`/payments/receivables?${qs}`);
    },
    forecast: (month?: string) => {
      const qs = month ? `?month=${month}` : "";
      return httpClient<ForecastResponse>(`/payments/forecast${qs}`);
    },
  },
};
