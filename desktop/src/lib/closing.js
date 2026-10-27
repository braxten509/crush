// Closing this window (desktop-design-5-6 2.23, 6.4; scaffold S5, complete).
// Pieces register close steps; closeWindow() runs them by order, each
// awaited, a failing step logged and skipped, then window_close, which leaves
// the engine and ends the window (the last window ends the app). The system
// close button sends the window event close-requested, which runs the same;
// Rust closes the window anyway when it has not closed 10 s later. First the
// close question may ask (lib/closeask.js): Cancel closes nothing, and Stop
// engine has window_close stop the engines that run work.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { askBeforeClose } from "./closeask.js";
import { windowClose } from "./engine/commands.js";

const steps = new Map(); // id → {id, order, run}
let closing = null;

// run is async; a step registered again under its id replaces the earlier one.
export function registerCloseStep({ id, order, run }) {
  steps.set(id, { id, order, run });
  return () => {
    if (steps.get(id)?.run === run) steps.delete(id);
  };
}

// Runs once per window: a second call while closing waits for the first.
// A cancelled close leaves the window closable again.
export function closeWindow() {
  if (!closing) closing = runSteps();
  return closing;
}

async function runSteps() {
  const { choice, stop } = await askBeforeClose();
  if (choice === "cancel") {
    closing = null;
    return;
  }
  for (const step of [...steps.values()].sort((a, b) => a.order - b.order)) {
    try {
      await step.run();
    } catch (error) {
      console.error(`close step "${step.id}" failed`, error?.message ?? error);
    }
  }
  await windowClose(stop);
}

try {
  getCurrentWebviewWindow()
    .listen("close-requested", () => closeWindow())
    .catch(() => {});
} catch {
  // Not inside the app window (unit tests).
}
