import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    css: true,
    coverage: {
      provider: "v8",
      reporter: ["text", "html"],
      include: ["src/**/*.{ts,tsx}"],
      exclude: [
        // Entry points and generated/vendored code carry no logic of our own.
        "src/main.tsx",
        "src/vite-env.d.ts",
        "src/components/ui/**",
        // Type-only modules compile away to nothing.
        "src/api/types.ts",
        "src/lib/analytics/types.ts",
        // The test harness itself.
        "src/test/**",
        "src/**/*.test.{ts,tsx}",
      ],
    },
  },
});
