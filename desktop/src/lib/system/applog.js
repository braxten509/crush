// The app log from the page (applog.rs, owner 2026-10-19): log() writes one
// line to ~/.local/state/crush-desktop/desktop.log. startAppLog() also logs
// page errors and the page's frame rate: 2 s of animation frames, measured
// once the window has been shown for 3 s and again when it comes back after
// a minute or more away, so a 60-a-second cap shows up without a screenshot.
import { invoke } from "@tauri-apps/api/core";

export function log(level, message) {
  invoke("app_log", { level, message: String(message) }).catch(() => {});
}

function measure() {
  return new Promise((resolve) => {
    let frames = 0;
    let first = 0;
    const step = (t) => {
      if (!first) first = t;
      else frames++;
      if (t - first < 2000) requestAnimationFrame(step);
      else resolve((frames * 1000) / (t - first));
    };
    requestAnimationFrame(step);
  });
}

let last = 0;
async function sample() {
  if (document.visibilityState !== "visible" || Date.now() - last < 60_000) return;
  last = Date.now();
  const rate = await measure();
  invoke("frame_rate_report", { rate }).catch(() => {});
}

export function startAppLog() {
  window.addEventListener("error", (e) => log("error", `${e.message} (${e.filename}:${e.lineno})`));
  window.addEventListener("unhandledrejection", (e) => log("error", `unhandled: ${e.reason?.message ?? e.reason}`));
  setTimeout(sample, 3000);
  window.addEventListener("focus", () => setTimeout(sample, 500));
  document.addEventListener("visibilitychange", () => setTimeout(sample, 500));
}
