import { createContext, useContext, useEffect, useState } from "react"
import type { ReactNode } from "react"
import { Navigate, Outlet } from "react-router-dom"
import { authService } from "@/api/auth-service"
import { queryClient } from "@/api/queryClient"
import type { AuthTokens } from "@/api/token-storage"

interface AuthContextValue {
  isAuthenticated: boolean
  login: (tokens: AuthTokens) => void
  logout: () => void
}

const AuthContext = createContext<AuthContextValue>({
  isAuthenticated: authService.isAuthenticated,
  login: (tokens) => authService.login(tokens),
  logout: () => authService.logout(),
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(authService.isAuthenticated)

  useEffect(() => {
    return authService.subscribe(({ isAuthenticated }) => {
      setIsAuthenticated(isAuthenticated)
    })
  }, [])

  const login = (tokens: AuthTokens) => authService.login(tokens)
  const logout = () => {
    authService.logout()
    queryClient.removeQueries()
  }

  return (
    <AuthContext value={{ isAuthenticated, login, logout }}>
      {children}
    </AuthContext>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}

export function ProtectedRoute() {
  const { isAuthenticated } = useAuth()
  if (!isAuthenticated) return <Navigate to="/login" replace />
  return <Outlet />
}
