import { authService } from "./auth-service";
import {
  ApiError,
  NetworkError,
  UnauthorizedError,
  ForbiddenError,
} from "./api-error";
import { cognitoInitiateAuth } from "./cognito";
import type {
  Entry,
  CreateEntryInput,
  MonthlySummary,
  CategorySummary,
  CashFlowPoint,
  Goal,
  NotificationPrefs,
  Category,
} from "./types";

export { CognitoAuthError } from "./cognito";
export type { CognitoAuthResult } from "./cognito";
export type {
  Entry,
  CreateEntryInput,
  MonthlySummary,
  CategorySummary,
  CashFlowPoint,
  Goal,
  NotificationPrefs,
  Category,
} from "./types";

const API_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8081";

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
    ...(tokens ? { Authorization: `Bearer ${tokens.accessToken}` } : {}),
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
    create: (data: CreateEntryInput) =>
      httpClient<Entry>("/entries", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: string, data: Partial<CreateEntryInput>) =>
      httpClient<Entry>(`/entries/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: string) =>
      httpClient<void>(`/entries/${id}`, { method: "DELETE" }),
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
};

export function formatBRL(centavos: number): string {
  return (centavos / 100).toLocaleString("pt-BR", {
    style: "currency",
    currency: "BRL",
  });
}
