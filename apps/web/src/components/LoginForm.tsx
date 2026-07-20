import { InvalidCredentialsError, useLoginMutation } from "@/api/queries";
import { useState } from "react";
import { Input } from "./ui/input";
import { PasswordInput } from "./PasswordInput";
import { Button } from "./ui/button";
import { useNavigate } from "react-router-dom";
import { AlertCircle, Loader2, Pill } from "lucide-react";

export function LoginForm() {
  const [form, setForm] = useState({
    email: "",
    password: "",
  })

  const login = useLoginMutation()
  const navigate = useNavigate()
  const error =
    login.isError &&
      login.error instanceof InvalidCredentialsError
      ? "E-mail ou senha inválidos."
      : login.isError
        ? "Erro ao fazer login."
        : null
  const canSubmit =
    form.email.trim() !== "" &&
    form.password.trim() !== "";
  const submit: NonNullable<React.ComponentProps<"form">["onSubmit"]> = async (
    e,
  ) => {
    e.preventDefault()

    try {
      await login.mutateAsync(form)
      navigate("/")
    } catch { }
  }

  return (
    <div className="rounded-2xl border bg-card/80 p-8 shadow-xl backdrop-blur-md">
      <div className="p-2">
        <div className="m-4 text-center">
          <span className="mx-auto m-2 grid size-14 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/25">
            <Pill className="size-7" />
          </span>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Drogaria Nova Farma</h1>
          <p className="m-1 text-sm text-muted-foreground">Painel Financeiro</p>
        </div>
      </div>
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-1.5">
          <label htmlFor="email" className="text-sm font-medium">Email</label>
          <Input
            id="email"
            name="email"
            type="email"
            autoComplete="email"
            value={form.email}
            required
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                email: e.target.value,
              }))
            }
            disabled={login.isPending}
          />
        </div>
        <div className="space-y-1.5">
          <label htmlFor="password" className="text-sm font-medium">Senha</label>
          <PasswordInput
            id="password"
            name="password"
            autoComplete="current-password"
            value={form.password}
            required
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                password: e.target.value,
              }))
            }
            disabled={login.isPending}
          />
        </div>

        {error && (
          <p className="flex items-center gap-2 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <AlertCircle className="size-4" />
            {error}
          </p>
        )}

        <Button
          className="w-full"
          type="submit"
          disabled={login.isPending || !canSubmit}
        >
          {login.isPending ? <Loader2 className="size-4 animate-spin" /> : "Entrar"}
        </Button>
      </form>
      <p className="mt-6 text-center text-xs text-muted-foreground">
        Emerbot · Farmácia Financeira
      </p>
    </div>
  )
}
