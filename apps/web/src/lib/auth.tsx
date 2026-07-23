import { createContext, useContext, useEffect, useState } from "react"
import type { ReactNode } from "react"
import { Navigate, Outlet } from "react-router-dom"
import { authService } from "@/api/auth-service"
import type { AuthSnapshot } from "@/api/auth-service"
import { queryClient } from "@/api/queryClient"
import type { AuthTokens } from "@/api/token-storage"

interface AuthContextValue extends AuthSnapshot {
  login: (tokens: AuthTokens) => void
  logout: () => void
}

const AuthContext = createContext<AuthContextValue>({
  ...authService.snapshot(),
  login: (tokens) => authService.login(tokens),
  logout: () => authService.logout(),
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [snapshot, setSnapshot] = useState<AuthSnapshot>(() =>
    authService.snapshot(),
  )

  useEffect(() => {
    return authService.subscribe((next) => {
      // Drop the previous session's cached data whenever it ends — logout or
      // expiry — so the next login can't briefly see another user's queries.
      if (!next.isAuthenticated) queryClient.removeQueries()
      setSnapshot(next)
    })
  }, [])

  const value: AuthContextValue = {
    ...snapshot,
    login: (tokens) => authService.login(tokens),
    logout: () => authService.logout(),
  }

  return <AuthContext value={value}>{children}</AuthContext>
}

// oxlint-disable-next-line react/only-export-components
export function useAuth() {
  return useContext(AuthContext)
}

export function ProtectedRoute() {
  const { isAuthenticated, sessionExpired } = useAuth()
  if (!isAuthenticated) {
    // Carry ?expired=1 only when a live session dropped, so the login screen
    // can tell "your session expired" from a plain "please sign in".
    return (
      <Navigate to={sessionExpired ? "/login?expired=1" : "/login"} replace />
    )
  }
  return <Outlet />
}
