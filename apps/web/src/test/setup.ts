import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Testing Library does not auto-clean without the global `afterEach` that
// `globals: true` would provide, and this project imports its test helpers
// explicitly — so unmount here instead of repeating it in every suite.
afterEach(cleanup);
