// Test runs only: when the app was started with --probe, the page reports what
// is on screen (positions, texts, state) to a file the hidden-screen tests read.
import { invoke } from "@tauri-apps/api/core";

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

export async function startProbe() {
  let enabled = false;
  try {
    enabled = (await invoke("app_info")).probe;
  } catch {
    return; // Not running inside the app window (unit tests, plain browser).
  }
  if (!enabled) return;
  inlineResult = inlineCodeBlocked();
  const write = () => invoke("probe_write", { json: JSON.stringify(snapshot()) }).catch(() => {});
  write();
  setInterval(write, 200);
}
