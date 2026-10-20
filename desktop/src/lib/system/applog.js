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

// The scroll test (CRUSH_SCROLL_TEST, refresh.rs; owner 2026-10-20): for each
// wheel or touchpad scroll, the frames the page drew and on how many the list
// under the pointer really moved, logged when it has been still for 0.4 s.
// The owner's screen draws 170 a second; a list moving on about 60 shows
// where scrolling falls behind.
function scrollable(node) {
  for (let el = node instanceof Element ? node : null; el; el = el.parentElement) {
    const style = getComputedStyle(el);
    if (/(auto|scroll)/.test(style.overflowY) && el.scrollHeight > el.clientHeight) return el;
  }
  return document.scrollingElement;
}

function watchScrolling() {
  let run = null;
  window.addEventListener(
    "wheel",
    (event) => {
      const now = performance.now();
      if (run) {
        run.wheels++;
        run.lastWheel = now;
        return;
      }
      const list = scrollable(event.target);
      run = { list, wheels: 1, lastWheel: now, first: 0, last: 0, frames: 0, moved: 0, top: list?.scrollTop ?? 0, longest: 0, sinceMove: 0 };
      const step = (t) => {
        const r = run;
        if (!r.first) r.first = t;
        else r.frames++;
        const top = r.list?.scrollTop ?? 0;
        if (top !== r.top) {
          r.moved++;
          r.top = top;
          if (r.sinceMove) r.longest = Math.max(r.longest, t - r.sinceMove);
          r.sinceMove = t;
        }
        r.last = t;
        if (performance.now() - r.lastWheel < 400) {
          requestAnimationFrame(step);
          return;
        }
        const seconds = (r.last - r.first) / 1000;
        if (seconds > 0.1) {
          const text = `${seconds.toFixed(1)} s, ${r.frames} frames (${(r.frames / seconds).toFixed(0)}/s), list moved on ${r.moved} (${(r.moved / seconds).toFixed(0)}/s), longest gap between moves ${r.longest.toFixed(1)} ms, ${r.wheels} wheel events`;
          invoke("scroll_test_report", { text }).catch(() => {});
        }
        run = null;
      };
      requestAnimationFrame(step);
    },
    { capture: true, passive: true },
  );
}

export function startAppLog() {
  invoke("scroll_test_mode")
    .then((mode) => mode && watchScrolling())
    .catch(() => {});
  window.addEventListener("error", (e) => log("error", `${e.message} (${e.filename}:${e.lineno})`));
  window.addEventListener("unhandledrejection", (e) => log("error", `unhandled: ${e.reason?.message ?? e.reason}`));
  setTimeout(sample, 3000);
  window.addEventListener("focus", () => setTimeout(sample, 500));
  document.addEventListener("visibilitychange", () => setTimeout(sample, 500));
}
