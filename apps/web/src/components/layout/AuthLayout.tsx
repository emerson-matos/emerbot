import { Outlet } from "react-router-dom";

export function AuthLayout() {
  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden bg-background p-4">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 opacity-70"
        style={{
          backgroundImage: `
            radial-gradient(
              40rem 30rem at 70% -10%,
              color-mix(in oklch, var(--primary) 18%, transparent),
              transparent
            ),
            radial-gradient(
              36rem 30rem at 10% 110%,
              color-mix(in oklch, var(--info) 12%, transparent),
              transparent
            )
          `,
        }}
      />

      <div className="relative w-full max-w-sm"><Outlet /></div>
    </div>
  )
}
