import { useSyncExternalStore } from "react";

/**
 * Subscribes to a CSS media query, so a component can change *what it renders*
 * at a breakpoint rather than only how it looks.
 *
 * CSS handles the second case and should keep handling it: hiding a column with
 * `sm:` costs nothing and survives a resize. This exists for the first — a chart
 * whose data series has a different length on a phone cannot be expressed as a
 * class, and rendering seven columns and hiding four still pays for seven.
 *
 * `useSyncExternalStore` rather than an effect with `useState`: the first paint
 * then already knows the answer, instead of rendering the wide branch and
 * swapping it a frame later.
 */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const list = window.matchMedia(query);
      list.addEventListener("change", onChange);
      return () => list.removeEventListener("change", onChange);
    },
    () => window.matchMedia(query).matches,
  );
}
