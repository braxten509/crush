// The app's motion (owner, 2026-10-19: "paper unfold"): menus and sheets
// unroll from the control that opened them like a sheet of paper, pages slide
// sideways, and a raised piece gets a light paper lift. About 0.22 s, easing
// out. Everything that opens also closes (owner, 2026-10-20: "EVERYTHING
// needs an open and close animation"): the opening played backwards, quicker
// (0.14 s, the owner's pick), ending wholly rolled away, as nothing fades.
// With the system's reduce-motion setting on, everything is instant.
// Svelte transitions: <div transition:unfold={{ from: "bottom" }}>.
import { cubicIn, cubicOut } from "svelte/easing";

export const MOTION_MS = 220;
export const CLOSE_MS = 140;
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

// A transition for both ways: Svelte asks for it with the direction once the
// page has updated, so opening runs `duration` easing out and closing runs
// CLOSE_MS easing in (the opening's time run backwards). `away(u)` adds the
// roll that hides a closing piece fully by its end. With `overlay`, a
// closing piece lies over whatever takes its place (Settings giving the
// column back to the chats), so the newcomer lays out at once beneath it:
// true covers the positioned parent, a string places it its own way.
const OVER = "position: absolute; inset: 0; z-index: 3; background: var(--paper);";
export function paper(css, { duration = MOTION_MS, delay = 0, away = null, off = false, overlay = false } = {}) {
  if (off || reducedMotion()) return { duration: 0 };
  return ({ direction }) =>
    direction === "out"
      ? { duration: CLOSE_MS, easing: cubicIn, css: (t, u) => (overlay === true ? OVER : overlay || "") + css(t, u) + (away ? ` clip-path: ${away(u)};` : "") }
      : { delay, duration, easing: cubicOut, css };
}

export function unfold(node, { from = "bottom", duration = MOTION_MS, delay = 0, overlay = false, off = false } = {}) {
  const sign = from === "top" ? -1 : 1;
  return paper((t, u) => `clip-path: ${unfoldInset(from, u)}; transform: translateY(${(sign * u * RISE).toFixed(2)}px);${tick(t)}`, { duration, delay, overlay, off });
}

// The inset that hides a piece leaving to one side: `dir` 1 rolls it away
// to the right (its left edge follows), -1 to the left.
export function sideInset(dir, u) {
  const hidden = `${(u * 100).toFixed(2)}%`;
  return dir === 1 ? `inset(0 0 0 ${hidden})` : `inset(0 ${hidden} 0 0)`;
}

// A page or column coming in from one side: `dir` 1 from the right, -1 from
// the left; closing, it slides back that way and rolls away toward it.
export function slidePage(node, { dir = 1, duration = MOTION_MS, delay = 0, overlay = false, off = false } = {}) {
  // No fade: a page drawn from its first frame never blinks (owner, 2026-10-19).
  return paper((t, u) => `transform: translateX(${(dir * u * 14).toFixed(2)}px);${tick(t)}`, { duration, delay, overlay, off, away: (u) => sideInset(dir, u) });
}

// A small piece settling in place (rows, notices, new messages); closing, it
// sinks back and rolls up from its lower edge. With `fold`, the room it
// takes in the page grows and shrinks with it, so what lies below follows it
// both ways instead of jumping once it has gone (owner, 2026-10-20).
export function settle(node, { duration = MOTION_MS, delay = 0, rise = RISE, off = false, fold = false } = {}) {
  const room = fold ? foldRoom(node) : () => "";
  return paper((t, u) => `transform: translateY(${(u * rise).toFixed(2)}px);${room(t)}${tick(t)}`, { duration, delay, off, away: (u) => unfoldInset("top", u) });
}

// The piece's height and the space round it, scaled by t (Svelte's slide).
export function foldRoom(node) {
  const style = getComputedStyle(node);
  const sides = ["height", "padding-top", "padding-bottom", "margin-top", "margin-bottom", "border-top-width", "border-bottom-width"];
  const sizes = sides.map((side) => [side, parseFloat(style.getPropertyValue(side)) || 0]);
  return (t) => ` overflow: hidden; ${sizes.map(([side, size]) => `${side}: ${(t * size).toFixed(2)}px;`).join(" ")}`;
}

// A sheet from the window's right edge (the Changes sheet): it slides its
// whole width in, and back out.
export function drawer(node, { duration = MOTION_MS } = {}) {
  return paper((t, u) => `transform: translateX(${(u * 100).toFixed(2)}%);${tick(t)}`, { duration });
}

// The veil's scrim (the one thing that fades: a dimming, not a piece).
export function scrim(node, { duration = 140, to = 1 } = {}) {
  return paper((t) => `opacity: ${(t * to).toFixed(3)};${tick(t)}`, { duration });
}
