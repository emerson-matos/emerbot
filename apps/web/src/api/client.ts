const API_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8081'

function getToken(): string | null {
  return localStorage.getItem('access_token')
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = getToken()
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(options.headers ?? {}),
  }
  const res = await fetch(`${API_URL}${path}`, { ...options, headers })

  if (res.status === 401) {
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
    login: (email: string, password: string) =>
      request<{ access_token: string; refresh_token: string; name: string }>('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      }),
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
    cashflow: (days = 30) =>
      request<{ points: CashFlowPoint[] }>(`/summary/cashflow?days=${days}`),
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
