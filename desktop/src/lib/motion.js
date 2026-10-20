// The app's motion (owner, 2026-10-19: "paper unfold"): menus and sheets
// unroll from the control that opened them like a sheet of paper, pages slide
// sideways, and a raised piece gets a light paper lift. About 0.22 s, easing
// out. With the system's reduce-motion setting on, everything is instant.
// Svelte transitions: <div transition:unfold={{ from: "bottom" }}>.
import { cubicOut } from "svelte/easing";

export const MOTION_MS = 220;
const RISE = 4; // CSS px a piece travels while it unrolls

// Every motion also runs the invisible --paper-tick (app.css): WebKitGTK
// otherwise draws one stale frame as a transform ends, and a sliding page
// jumps back for a frame (owner, 2026-10-20: 9 of 16 settings slides).
export function tick(t) {
  return ` --paper-tick: ${t.toFixed(3)};`;
}

export function reducedMotion(win = globalThis) {
  return win.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
}

// The inset that hides the part still rolled up: from "bottom" a piece grows
// up from its lower edge (a menu over its button), from "top" down from its
// upper edge.
export function unfoldInset(from, u) {
  const hidden = `${(u * 100).toFixed(2)}%`;
  return from === "top" ? `inset(0 0 ${hidden} 0)` : `inset(${hidden} 0 0 0)`;
}

export function unfold(node, { from = "bottom", duration = MOTION_MS, delay = 0 } = {}) {
  if (reducedMotion()) return { duration: 0 };
  const sign = from === "top" ? -1 : 1;
  return {
    delay,
    duration,
    easing: cubicOut,
    css: (t, u) => `clip-path: ${unfoldInset(from, u)}; transform: translateY(${(sign * u * RISE).toFixed(2)}px);${tick(t)}`,
  };
}

// A page coming in from one side: `dir` 1 from the right, -1 from the left.
export function slidePage(node, { dir = 1, duration = MOTION_MS, delay = 0 } = {}) {
  if (reducedMotion()) return { duration: 0 };
  return {
    delay,
    duration,
    easing: cubicOut,
    // No fade: a page drawn from its first frame never blinks (owner, 2026-10-19).
    css: (t, u) => `transform: translateX(${(dir * u * 14).toFixed(2)}px);${tick(t)}`,
  };
}

// A small piece settling in place (rows, notices, new messages).
export function settle(node, { duration = MOTION_MS, delay = 0, rise = RISE } = {}) {
  if (reducedMotion()) return { duration: 0 };
  return {
    delay,
    duration,
    easing: cubicOut,
    css: (t, u) => `transform: translateY(${(u * rise).toFixed(2)}px);${tick(t)}`,
  };
}
