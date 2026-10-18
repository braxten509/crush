// Test runs only: when the app was started with --probe, the page reports what
// is on screen (positions, texts, state) to a file the hidden-screen tests read.
//
// The report is today's snapshot plus one object per registered section
// (desktop-design 1.9). probeSection(name, light, heavy): light() reads stores
// and values components already keep, never the page's layout, and runs every
// 200 ms; heavy() reads the page (rects, words, selection, scans) and runs only
// when a driver asks by writing "<probe file>.request.json" with a new {"id": N}.
// That tick writes every section's heavy fields under "heavy" with
// "request_id": N; they stay until the next request. A request may also carry
// "invoke": [{"command", "args"}], which the page runs once (the scaffold's
// command check). No request is served while a measured stretch runs.
import { invoke } from "@tauri-apps/api/core";
import { dialogs } from "./dialogs.js";
import { lastClipboardWrite } from "./engine/commands.js";
import { ui } from "./stores/ui.svelte.js";

let inlineResult = null;

// Test runs only: tries to run code the way a crafted AI reply would (an
// inline script and an inline event handler). The window's content rule must
// block both; the tests check the answer.
export function inlineCodeBlocked(doc = document, win = window) {
  win.__probeInline = false;
  const script = doc.createElement("script");
  script.textContent = "window.__probeInline = true";
  doc.head.appendChild(script);
  script.remove();
  const el = doc.createElement("div");
  el.setAttribute("onclick", "window.__probeInline = true");
  doc.body.appendChild(el);
  el.click();
  el.remove();
  return win.__probeInline !== true;
}

export function snapshot(doc = document) {
  const shell = doc.querySelector('[data-probe="shell"]');
  const rect = shell ? shell.getBoundingClientRect() : null;
  // Look captures wait for the fonts and the zoom, so they never catch a fallback face or a half-applied size.
  const zoom = doc.documentElement?.style.getPropertyValue("--zoom") ?? "";
  return {
    ready: Boolean(shell),
    title: doc.title,
    heading: doc.querySelector("h1")?.textContent ?? "",
    shell: rect && { x: rect.x, y: rect.y, w: rect.width, h: rect.height },
    fonts: doc.fonts?.status ?? null,
    zoom: zoom === "" ? null : Number(zoom),
    inlineBlocked: inlineResult,
    at: Date.now(),
  };
}

const sections = new Map(); // name → {light, heavy}

// Pinned names and fields: desktop-design 1.9. Each piece registers its own.
export function probeSection(name, light, heavy = null) {
  sections.set(name, { light, heavy });
}

// The frame meter (1.9): records frames and key-to-frame times during a
// measured stretch, which Ctrl+Alt+F12 starts and ends.
const frames = { measuring: false, mark: 0, times: [], over50: [], inputs: 0, inputOver50: [] };
let lastFrame = 0;
let pendingKeys = [];

export function framesReport() {
  const sorted = [...frames.times].sort((a, b) => a - b);
  const p95 = sorted.length ? sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * 0.95) - 1)] : 0;
  return {
    measuring: frames.measuring,
    frames_mark: frames.mark,
    count: frames.times.length,
    p95_ms: round(p95),
    max_ms: round(sorted.length ? sorted[sorted.length - 1] : 0),
    over50_at: frames.over50,
    input_over50_at: frames.inputOver50,
    input_count: frames.inputs,
  };
}

function round(ms) {
  return Math.round(ms * 10) / 10;
}

function toggleStretch() {
  if (frames.measuring) {
    frames.measuring = false;
    return;
  }
  Object.assign(frames, { measuring: true, mark: Date.now(), times: [], over50: [], inputs: 0, inputOver50: [] });
  pendingKeys = [];
  lastFrame = performance.now();
  requestAnimationFrame(meterFrame);
}

function meterFrame(now) {
  if (!frames.measuring) return;
  const at = performance.now();
  const spent = now - lastFrame;
  lastFrame = now;
  frames.times.push(spent);
  if (spent > 50) frames.over50.push(Math.round(Date.now() - frames.mark));
  for (const keyAt of pendingKeys) {
    if (at - keyAt > 50) frames.inputOver50.push(Math.round(Date.now() - frames.mark));
  }
  pendingKeys = [];
  requestAnimationFrame(meterFrame);
}

function meterKey(event) {
  if (event.ctrlKey && event.altKey && event.key === "F12") {
    event.preventDefault();
    if (!event.repeat) toggleStretch();
    return;
  }
  if (!frames.measuring) return;
  frames.inputs += 1;
  pendingKeys.push(performance.now());
}

let lastRequestId = null;
let heavy = { request_id: null, sections: null, invoke: null };

async function serveRequest() {
  let request = null;
  try {
    request = await invoke("probe_request");
  } catch {
    return;
  }
  if (!request || request.id === undefined || request.id === lastRequestId || frames.measuring) return;
  lastRequestId = request.id;
  const out = {};
  for (const [name, section] of sections) {
    if (section.heavy) out[name] = guarded(section.heavy);
  }
  let invoked = null;
  if (Array.isArray(request.invoke)) {
    invoked = [];
    for (const call of request.invoke) {
      try {
        invoked.push({ command: call.command, ok: true, value: (await invoke(call.command, call.args ?? {})) ?? null });
      } catch (error) {
        // An AppError arrives as a plain object; anything else as its text.
        const plain = error !== null && typeof error === "object" && !(error instanceof Error);
        invoked.push({ command: call.command, ok: false, error: plain ? error : String(error) });
      }
    }
  }
  heavy = { request_id: request.id, sections: out, invoke: invoked };
}

function guarded(fn) {
  try {
    return fn();
  } catch (error) {
    return { probe_error: String(error) };
  }
}

function report() {
  const out = snapshot();
  for (const [name, section] of sections) out[name] = guarded(section.light);
  out.clipboard = { last: lastClipboardWrite() };
  out.request_id = heavy.request_id;
  out.heavy = heavy.sections;
  out.invoked = heavy.invoke;
  return out;
}

export async function startProbe() {
  let enabled = false;
  try {
    enabled = (await invoke("app_info")).probe;
  } catch {
    return; // Not running inside the app window (unit tests, plain browser).
  }
  if (!enabled) return;
  inlineResult = inlineCodeBlocked();
  probeSection("overlay", () => ({
    viewer: ui.viewer ?? null,
    dialogs: dialogs.map((entry) => entry.id),
    help: ui.help,
    notice: ui.notice && { kind: ui.notice.kind, text: ui.notice.text },
  }));
  probeSection("frames", framesReport);
  window.addEventListener("keydown", meterKey, { capture: true });
  let busy = false;
  const tick = async () => {
    if (busy) return;
    busy = true;
    try {
      await serveRequest();
      await invoke("probe_write", { json: JSON.stringify(report()) });
    } catch {
      // The next tick tries again.
    } finally {
      busy = false;
    }
  };
  tick();
  setInterval(tick, 200);
}
