import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Testing Library does not auto-clean without the global `afterEach` that
// `globals: true` would provide, and this project imports its test helpers
// explicitly — so unmount here instead of repeating it in every suite.
afterEach(cleanup);

// jsdom ships no matchMedia, and components that pick a layout from a
// breakpoint (useMediaQuery) call it during render — without this they throw
// before asserting anything. The default is the narrow branch, which is what an
// unset jsdom viewport is: `setMatchMedia` overrides it per test.
setMatchMedia(false);

/**
 * Makes every media query answer `matches`. Call it at the top of a test that
 * cares which branch renders — the default is "no query matches", i.e. the
 * smallest layout.
 */
export function setMatchMedia(matches: boolean) {
  window.matchMedia = ((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}
