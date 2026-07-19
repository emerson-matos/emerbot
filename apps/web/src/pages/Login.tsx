import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Pill, AlertCircle, Loader2 } from 'lucide-react'
import { api, CognitoAuthError } from '../api/client'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

export default function Login() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await api.auth.login(email, password)
      navigate('/')
    } catch (err) {
      if (err instanceof CognitoAuthError &&
        (err.type === 'NotAuthorizedException' || err.type === 'UserNotFoundException')) {
        setError('E-mail ou senha inválidos.')
      } else {
        setError('Erro ao fazer login')
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden bg-background p-4">
      {/* Apothecary atmosphere */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 opacity-70"
        style={{
          backgroundImage:
            'radial-gradient(40rem 30rem at 70% -10%, color-mix(in oklch, var(--primary) 18%, transparent), transparent), radial-gradient(36rem 30rem at 10% 110%, color-mix(in oklch, var(--info) 12%, transparent), transparent)',
        }}
      />

      <div className="relative w-full max-w-sm">
        <div className="rounded-2xl bg-card/80 p-8 shadow-xl ring-1 ring-foreground/10 backdrop-blur-md">
          <div className="mb-8 text-center">
            <span className="mx-auto mb-4 grid size-14 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/25">
              <Pill className="size-7" />
            </span>
            <h1 className="font-heading text-2xl font-semibold tracking-tight">Farmácia</h1>
            <p className="mt-1 text-sm text-muted-foreground">Painel Financeiro</p>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <label htmlFor="email" className="text-sm font-medium">Email</label>
              <Input
                id="email"
                type="email"
                value={email}
                onChange={e => setEmail(e.target.value)}
                required
                autoComplete="email"
                placeholder="seu@email.com"
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="password" className="text-sm font-medium">Senha</label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                required
                autoComplete="current-password"
                placeholder="••••••••"
              />
            </div>

            {error && (
              <p className="flex items-center gap-2 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
                <AlertCircle className="size-4 shrink-0" />
                {error}
              </p>
            )}

            <Button type="submit" disabled={loading} size="lg" className="w-full">
              {loading && <Loader2 className="size-4 animate-spin" />}
              {loading ? 'Entrando...' : 'Entrar'}
            </Button>
          </form>
        </div>
        <p className="mt-6 text-center text-xs text-muted-foreground">
          Emerbot · Farmácia Financeira
        </p>
      </div>
    </div>
  )
}
