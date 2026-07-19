const API_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8081'
const COGNITO_ENDPOINT = import.meta.env.VITE_COGNITO_ENDPOINT ?? 'http://localhost:9229'
const COGNITO_CLIENT_ID = import.meta.env.VITE_COGNITO_CLIENT_ID ?? ''

function getToken(): string | null {
  return localStorage.getItem('access_token')
}

// --- Cognito (InitiateAuth over plain JSON — no AWS SDK needed) ---

export class CognitoAuthError extends Error {
  type: string
  constructor(type: string, message: string) {
    super(message)
    this.type = type
  }
}

interface CognitoAuthResult {
  AccessToken: string
  IdToken: string
  RefreshToken?: string
  ExpiresIn: number
  TokenType: string
}

async function cognitoInitiateAuth(
  authFlow: 'USER_PASSWORD_AUTH' | 'REFRESH_TOKEN_AUTH',
  authParameters: Record<string, string>,
): Promise<CognitoAuthResult> {
  const res = await fetch(`${COGNITO_ENDPOINT}/`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-amz-json-1.1',
      'X-Amz-Target': 'AWSCognitoIdentityProviderService.InitiateAuth',
    },
    body: JSON.stringify({
      AuthFlow: authFlow,
      ClientId: COGNITO_CLIENT_ID,
      AuthParameters: authParameters,
    }),
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new CognitoAuthError(body.__type ?? 'UnknownError', body.message ?? 'Authentication failed')
  }
  return body.AuthenticationResult
}

function base64UrlDecode(input: string): string {
  const base64 = input.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), '=')
  const bytes = Uint8Array.from(atob(padded), c => c.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

function decodeIdToken(idToken: string): { name?: string; email?: string } {
  try {
    return JSON.parse(base64UrlDecode(idToken.split('.')[1]))
  } catch {
    return {}
  }
}

function storeAuthResult(result: CognitoAuthResult) {
  localStorage.setItem('access_token', result.AccessToken)
  if (result.RefreshToken) localStorage.setItem('refresh_token', result.RefreshToken)
  const { name, email } = decodeIdToken(result.IdToken)
  localStorage.setItem('user_name', name ?? email ?? '')
}

// Cognito's REFRESH_TOKEN_AUTH flow doesn't rotate the refresh token, so a
// missing RefreshToken in the result (handled by storeAuthResult) is expected
// there, not an error.
let refreshInFlight: Promise<boolean> | null = null

// A page load can fire several requests in parallel; if the access token is
// stale, they'd all hit 401 at once and — without this — each would kick off
// its own REFRESH_TOKEN_AUTH call. Sharing one in-flight promise collapses
// that burst into a single refresh, which every caller then awaits.
function trySilentRefresh(): Promise<boolean> {
  if (refreshInFlight) return refreshInFlight

  const refreshToken = localStorage.getItem('refresh_token')
  if (!refreshToken) return Promise.resolve(false)

  refreshInFlight = (async () => {
    try {
      const result = await cognitoInitiateAuth('REFRESH_TOKEN_AUTH', { REFRESH_TOKEN: refreshToken })
      storeAuthResult(result)
      return true
    } catch {
      return false
    } finally {
      refreshInFlight = null
    }
  })()

  return refreshInFlight
}

async function request<T>(path: string, options: RequestInit = {}, isRetry = false): Promise<T> {
  const token = getToken()
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(options.headers ?? {}),
  }
  const res = await fetch(`${API_URL}${path}`, { ...options, headers })

  if (res.status === 401) {
    if (!isRetry && (await trySilentRefresh())) {
      return request<T>(path, options, true)
    }
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    window.location.href = '/login'
    throw new Error('Unauthorized')
  }

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? 'Request failed')
  }

  if (res.status === 204) return {} as T
  return res.json()
}

export const api = {
  auth: {
    login: async (email: string, password: string) => {
      const result = await cognitoInitiateAuth('USER_PASSWORD_AUTH', { USERNAME: email, PASSWORD: password })
      storeAuthResult(result)
    },
  },

  entries: {
    list: (params?: Record<string, string>) => {
      const qs = params ? '?' + new URLSearchParams(params).toString() : ''
      return request<{ entries: Entry[]; count: number }>(`/entries${qs}`)
    },
    create: (data: CreateEntryInput) =>
      request<Entry>('/entries', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: string, data: Partial<CreateEntryInput>) =>
      request<Entry>(`/entries/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: string) =>
      request<void>(`/entries/${id}`, { method: 'DELETE' }),
  },

  summary: {
    monthly: (month?: string) => {
      const qs = month ? `?month=${month}` : ''
      return request<MonthlySummary>(`/summary/monthly${qs}`)
    },
    categories: (from?: string, to?: string) => {
      const qs = new URLSearchParams()
      if (from) qs.set('from', from)
      if (to) qs.set('to', to)
      return request<{ categories: CategorySummary[] }>(`/summary/categories?${qs}`)
    },
    cashflow: (month: string) =>
      request<{ points: CashFlowPoint[] }>(`/summary/cashflow?month=${month}`),
  },

  categories: {
    list: () => request<{ categories: Category[] }>('/categories'),
  },

  goals: {
    get: (month?: string) => {
      const qs = month ? `?month=${month}` : ''
      return request<{ goal: Goal | null; month: string }>(`/goals${qs}`)
    },
  },
}

// --- Types ---

export interface Entry {
  UserID: string
  EntryID: string
  Date: string
  Amount: number
  Category: string
  Type: 'expense' | 'income'
  Description: string
  DueDate: string | null
  PaymentStatus: 'pending' | 'paid'
  PaymentDate: string | null
  Supplier: string
  Source: string
}

export interface CreateEntryInput {
  date: string
  amount: number
  category: string
  type: 'expense' | 'income'
  description: string
  due_date?: string
  payment_status: 'pending' | 'paid'
  supplier?: string
}

export interface MonthlySummary {
  Month: string
  TotalIncome: number
  TotalExpense: number
  Balance: number
}

export interface CategorySummary {
  Category: string
  Type: 'expense' | 'income'
  Total: number
  Count: number
}

export interface CashFlowPoint {
  Date: string
  ProjectedIncome: number
  ProjectedExpense: number
  RunningBalance: number
}

export interface Goal {
  UserID: string
  Month: string
  RevenueTarget: number
  ExpenseTarget: number
}

export interface Category {
  UserID: string
  Slug: string
  Label: string
  Type: 'expense' | 'income'
  Default: boolean
}

export function formatBRL(centavos: number): string {
  return (centavos / 100).toLocaleString('pt-BR', {
    style: 'currency',
    currency: 'BRL',
  })
}
