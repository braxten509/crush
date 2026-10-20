// The app log from the page (applog.rs, owner 2026-10-19): log() writes one
// line to ~/.local/state/crush-desktop/desktop.log. startAppLog() also logs
// page errors and the page's frame rate: 2 s of animation frames, measured
// once the window has been shown for 3 s and again when it comes back after
// a minute or more away, so a 60-a-second cap shows up without a screenshot.
import { invoke } from "@tauri-apps/api/core";
import { tick } from "svelte";

const BENIGN = /^ResizeObserver loop (completed with undelivered notifications|limit exceeded)/;

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
// The owner's screen draws 170 a second, but the list moved on about 25-50,
// so it also logs what kept the page busy: the time the app's own code took
// after each scroll event (the list's handler and the screen updates it
// causes), the time WebKit took after each frame to lay out, measure and draw,
// slow frames, and how many chat rows were put on the page or built new.
// The nearest list under `node` that scrolls, or the page's.
function scroller(node) {
  for (let el = node instanceof Element ? node : node?.parentElement; el; el = el.parentElement) {
    const style = getComputedStyle(el);
    if (/(auto|scroll)/.test(style.overflowY) && el.scrollHeight > el.clientHeight) return el;
  }
  return document.scrollingElement;
}

function timing() {
  return { count: 0, total: 0, most: 0 };
}

function add(time, ms) {
  time.count++;
  time.total += ms;
  time.most = Math.max(time.most, ms);
}

function said(time) {
  return time.count ? `${(time.total / time.count).toFixed(1)} ms average, ${time.most.toFixed(1)} ms most` : "none";
}

function watchScrolling() {
  let run = null;
  const after = new MessageChannel();
  after.port1.onmessage = (event) => run && add(run.drawing, performance.now() - event.data);
  const rows = new MutationObserver((records) => {
    if (!run) return;
    for (const record of records) {
      if (!record.addedNodes.length) continue;
      if (record.target.hasAttribute?.("data-key")) run.shown++;
      else if (record.target.classList?.contains("row") && record.target.parentElement?.hasAttribute("data-key")) run.built++;
    }
  });
  // Capture comes before the list's own handler; tick() waits for the screen
  // updates its changes cause.
  document.addEventListener(
    "scroll",
    () => {
      if (!run) return;
      const start = performance.now();
      Promise.resolve()
        .then(tick)
        .then(() => run && add(run.code, performance.now() - start));
    },
    { capture: true, passive: true },
  );
  window.addEventListener(
    "wheel",
    (event) => {
      const now = performance.now();
      if (run) {
        run.wheels++;
        run.lastWheel = now;
        return;
      }
      const list = scroller(event.target);
      run = {
        list,
        wheels: 1,
        lastWheel: now,
        first: 0,
        last: 0,
        frames: 0,
        moved: 0,
        top: list?.scrollTop ?? 0,
        longest: 0,
        sinceMove: 0,
        gaps: [],
        code: timing(),
        drawing: timing(),
        shown: 0,
        built: 0,
      };
      if (list) rows.observe(list, { childList: true, subtree: true });
      const step = (t) => {
        const r = run;
        after.port2.postMessage(performance.now());
        if (!r.first) r.first = t;
        else {
          r.frames++;
          r.gaps.push(t - r.last);
        }
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
        rows.disconnect();
        const seconds = (r.last - r.first) / 1000;
        if (seconds > 0.1) {
          // A slow frame took over 1.5 times the usual (middle) frame. The
          // page's clock counts whole milliseconds, so the quickest frame
          // can read 1 ms and is no measure.
          const usual = [...r.gaps].sort((a, b) => a - b)[r.gaps.length >> 1];
          const slow = r.gaps.filter((gap) => gap > 1.5 * usual);
          const text =
            `${seconds.toFixed(1)} s, ${r.frames} frames (${(r.frames / seconds).toFixed(0)}/s), ` +
            `list moved on ${r.moved} (${(r.moved / seconds).toFixed(0)}/s), longest gap between moves ${r.longest.toFixed(1)} ms, ` +
            `${r.wheels} wheel events; usual frame ${usual.toFixed(1)} ms, ${slow.length} slow frames ` +
            `(${slow.reduce((sum, gap) => sum + gap, 0).toFixed(0)} ms in all); ` +
            `app code per scroll event ${said(r.code)} (${r.code.count} events); ` +
            `layout and drawing per frame ${said(r.drawing)}; chat rows put on the page ${r.shown}, built new ${r.built}`;
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
  window.addEventListener("error", (e) => {
    // Chromium's note that a ResizeObserver change waits for the next frame
    // (the lists measure their rows that way); it comes many times a second
    // while scrolling and is no fault.
    if (BENIGN.test(e.message ?? "")) return;
    log("error", `${e.message} (${e.filename}:${e.lineno})`);
  });
  window.addEventListener("unhandledrejection", (e) => log("error", `unhandled: ${e.reason?.message ?? e.reason}`));
  setTimeout(sample, 3000);
  window.addEventListener("focus", () => setTimeout(sample, 500));
  document.addEventListener("visibilitychange", () => setTimeout(sample, 500));
}
