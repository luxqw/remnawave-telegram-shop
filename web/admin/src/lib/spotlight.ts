// Cursor spotlight for .glass-card surfaces, ported from web/policy/script.js's
// `.sheet` pointermove handler. Uses one delegated `pointermove` listener on `document`
// (via event.target.closest) instead of a per-card listener/useEffect — Preact re-renders
// and unmounts/remounts DOM nodes constantly, so a delegated listener survives that without
// needing a MutationObserver to re-attach per-card listeners.

const SPOTLIGHT_SELECTOR = ".glass-card";

function handlePointerMove(event: PointerEvent): void {
  const target = event.target;
  if (!(target instanceof Element)) return;

  const card = target.closest<HTMLElement>(SPOTLIGHT_SELECTOR);
  if (!card) return;

  const rect = card.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0) return;

  const x = ((event.clientX - rect.left) / rect.width) * 100;
  const y = ((event.clientY - rect.top) / rect.height) * 100;
  card.style.setProperty("--mx", `${x}%`);
  card.style.setProperty("--my", `${y}%`);
}

// Call once at app startup (see main.tsx). No-ops entirely — not even attaching the
// listener — when the user prefers reduced motion or is on a coarse (touch) pointer,
// mirroring web/policy's script.js guard exactly.
export function initSpotlight(): void {
  const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const hasFinePointer = window.matchMedia("(pointer: fine)").matches;
  if (prefersReducedMotion || !hasFinePointer) return;

  document.addEventListener("pointermove", handlePointerMove);
}
