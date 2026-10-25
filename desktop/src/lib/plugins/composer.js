// The composer's registrations (desktop-design 1.10): the yolo.toggle and
// mode.cycle actions, the tray's queue section,
// the keys of the field, its `@` popup and delete mode and the global ones
// (1.8), and the probe's `composer` section (1.9).
import QueueSection from "../../components/composer/QueueSection.svelte";
import { registerAction } from "../actions.js";
import { deleteModeKey } from "../composer/chips.js";
import { atEnd, onFirstLine, onLastLine } from "../composer/history.js";
import { modelButtonText } from "../composer/modes.js";
import { queueRows } from "../composer/trayrows.js";
import { areaEdit, areaState } from "../kit/areakeys.js";
import { applyEdit } from "../kit/fieldkeys.js";
import { bindKey } from "../keys.js";
import { agentNow } from "../live/helpers.svelte.js";
import { probeSection } from "../probe.js";
import * as C from "../stores/composer.svelte.js";
import { visibleItems } from "../stores/queue.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { todos } from "../stores/todos.svelte.js";
import { ui } from "../stores/ui.svelte.js";
import { ws } from "../stores/workspace.svelte.js";
import { focusTray, registerTraySection, traySections } from "../tray.js";

const queued = () => (sessions.currentId ? visibleItems(sessions.currentId) : []);

// -- Actions --

registerAction({ id: "yolo.toggle", title: "Toggle Yolo mode", keys: ["Ctrl+Y"], run: () => C.toggleYolo() });
registerAction({ id: "mode.cycle", title: "Switch input mode", keys: ["Shift+Tab"], run: () => C.cycleMode() });

// -- The tray's sections (TYPE-044, TYPE-045). The to-do list is the right
// panel's (owner, 2026-10-19). --

registerTraySection({
  id: "queue",
  order: 20,
  // Without chips the queue sits on Complete's row instead (QueueLine.svelte).
  visible: () => queued().length > 0 && C.hasChips(),
  header: () => `QUEUE ${queued().length}`,
  component: QueueSection,
});

// -- Keys --

const field = () => C.fieldElement();
const inField = (event) => Boolean(field()) && event.target === field();

// Delete mode (TYPE-038): Esc leaves, "r" removes every attachment, a digit
// removes that chip and leaves, every other key is eaten.
function deleteKey(combo) {
  const action = deleteModeKey(combo, { deleting: true, count: C.draftOf().attachments.length });
  if (action?.leave) C.leaveDeleteMode();
  else if (action?.clear) C.clearAttachments();
  else if (action && "remove" in action) {
    C.removeAttachment(action.remove);
    C.leaveDeleteMode();
  }
  return true;
}

// A key of the focused field: delete mode first, then fn (true takes it).
function fieldKey(combo, fn) {
  bindKey("composer", combo, (event) => {
    if (!inField(event)) return false;
    if (C.composer.deleteMode) return deleteKey(combo);
    return fn(event) === true;
  });
}

function caret() {
  const el = field();
  return { value: el?.value ?? "", start: el?.selectionStart ?? 0, end: el?.selectionEnd ?? 0 };
}

fieldKey("Enter", () => {
  C.send();
  return true;
});
// Shift+Enter is the textarea's own new line; Ctrl+J puts one in too.
fieldKey("Shift+Enter", () => false);
fieldKey("Ctrl+J", () => {
  C.closePopup();
  C.insertText("\n");
  return true;
});
fieldKey("Backspace", () => C.backspaceLeavesBang());
// Alt+Backspace deletes the word before the caret, as in the sheets' editors
// (lib/kit/areakeys.js; owner, 2026-10-20). The browser's own delete keeps
// the field's undo and tells the store through the input event.
fieldKey("Alt+Backspace", () => {
  const el = field();
  if (!el) return false;
  const edit = areaEdit("Alt+Backspace", areaState(el.value, el.selectionStart, el.selectionEnd, el.selectionDirection));
  if (!edit?.chars) return false;
  applyEdit(el, edit);
  return true;
});

// TYPE-017: the first ↑ goes to the first line's start, the next one older.
fieldKey("ArrowUp", () => {
  const { value, start, end } = caret();
  if (value.length === 0 || (start === 0 && end === 0)) {
    C.historyUp();
    return true;
  }
  if (start === end && onFirstLine(value, start)) {
    C.caretToEdge("start");
    return true;
  }
  return false;
});

// ↓ at the end: the tray's section that asks for it, else newer history.
fieldKey("ArrowDown", () => {
  const { value, start, end } = caret();
  if (start !== end) return false;
  if (!C.browsingHistory() && atEnd(value, end) && focusTray({ down: true })) return true;
  if (atEnd(value, start)) {
    C.historyDown();
    return true;
  }
  if (onLastLine(value, start)) {
    C.caretToEdge("end");
    return true;
  }
  return false;
});

fieldKey("Tab", () => {
  C.focusTimeline();
  return true;
});

// Ctrl+A selects all, as in any desktop text box (owner, 2026-10-21; the
// terminal's line start stays on Home).
fieldKey("Home", () => (C.caretToLineEdge("start"), true));
fieldKey("Ctrl+A", () => {
  field()?.select();
  return true;
});
for (const combo of ["Ctrl+E", "End"]) fieldKey(combo, () => (C.caretToLineEdge("end"), true));
fieldKey("Ctrl+Shift+A", () => {
  field()?.select();
  return true;
});
fieldKey("Ctrl+Shift+C", () => {
  C.copySelection();
  return true;
});
fieldKey("Ctrl+Shift+X", () => {
  C.copySelection({ cut: true });
  return true;
});
fieldKey("Ctrl+Shift+V", () => {
  C.pasteFromClipboard();
  return true;
});

// Ctrl+R enters delete mode (always taken, so the page never reloads);
// "r" and the digits act only inside it.
bindKey("composer", "Ctrl+R", (event) => {
  if (!inField(event)) return false;
  if (!C.composer.deleteMode) C.enterDeleteMode();
  return true;
});
for (const combo of ["r", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"]) {
  bindKey("composer", combo, (event) => inField(event) && C.composer.deleteMode && deleteKey(combo));
}

// Esc (TYPE-046): delete mode, then history, then stopping the run.
bindKey("composer", "Escape", (event) => {
  if (inField(event)) {
    if (C.composer.deleteMode) return deleteKey("Escape");
    if (C.historyEscape()) return true;
  }
  if (!C.working()) return false;
  C.stop();
  return true;
});

// Ctrl+Enter (TYPE-047): key repeat is ignored.
for (const scope of ["composer", "timeline"]) {
  bindKey(scope, "Ctrl+Enter", (event) => {
    if (!C.working() && !C.interruptPending()) return false;
    if (!event.repeat) C.interruptAndSend();
    return true;
  });
  bindKey(scope, "Shift+Tab", () => {
    C.cycleMode();
    return true;
  });
}

// The popup (TYPE-028): while it shows rows it takes ↑ ↓ Enter Tab; while open,
// Ctrl+N, Ctrl+P and Ctrl+Y never reach new chat or YOLO.
const popupOpen = () => Boolean(C.composer.popup);
const popupRows = () => (C.composer.popup?.rows.length ?? 0) > 0;
bindKey("popup", "ArrowUp", () => popupRows() && (C.movePopup("up"), true));
bindKey("popup", "ArrowDown", () => popupRows() && (C.movePopup("down"), true));
for (const combo of ["Enter", "Tab"]) bindKey("popup", combo, () => popupRows() && (C.pickRow(), true));
bindKey("popup", "Ctrl+Y", () => {
  if (!popupOpen()) return false;
  if (popupRows()) C.pickRow();
  return true;
});
bindKey("popup", "Ctrl+N", () => popupOpen() && (C.previewRow("next"), true));
bindKey("popup", "Ctrl+P", () => popupOpen() && (C.previewRow("previous"), true));
bindKey("popup", "Escape", () => popupOpen() && (C.closePopup(), true));

// Global keys.
bindKey("global", "Ctrl+Y", () => {
  C.toggleYolo();
  return true;
});
// Ctrl+C copies a selection in the field (TYPE-009); without one it is left
// for the quit key (phase 5).
bindKey("global", "Ctrl+C", (event) => {
  if (!inField(event)) return false;
  const { start, end } = caret();
  if (start === end) return false;
  C.copySelection();
  return true;
});

// -- Probe (1.9) --

function rect(el) {
  if (!el) return null;
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

probeSection(
  "composer",
  () => {
    const sid = sessions.currentId;
    const draft = C.draftOf(sid);
    const el = field();
    const popup = C.composer.popup;
    return {
      session_id: sid,
      value: draft.text,
      focused: ui.focus === "composer" && Boolean(el) && globalThis.document?.activeElement === el,
      mode: C.currentMode(),
      placeholder: C.currentPlaceholder(),
      selection: el ? [el.selectionStart, el.selectionEnd] : [0, 0],
      attachments: draft.attachments.map((a) => ({ name: a.name, kind: a.kind, mime: a.mime })),
      kept: C.keptSkills().map((skill) => skill.name || skill.id),
      delete_mode: C.composer.deleteMode,
      popup: popup && { query: popup.query, loading: popup.loading, focused: popup.focused, rows: popup.rows.slice(0, 50).map((r) => r.row.text) },
      tray: traySections.map((section) => ({ kind: section.id, text: section.header() })),
      todos_open: todos.open,
      queue: queueRows(queued()),
      slot: ui.slot && { kind: ui.slot.kind, buttons: C.composer.slotButtons.map((b) => ({ label: b.label, selected: b.selected })) },
      model: modelButtonText(agentNow(), ws.config?.options?.tui?.composer_footer),
      input: C.composer.input,
      yolo: C.composer.yolo,
      switching: C.composer.switching,
      plan_handoff: C.composer.planHandoff,
      shells: Object.values(C.composer.shells).map((s) => ({
        request_id: s.requestId,
        session_id: s.sessionId,
        command: s.command,
        output: s.output.slice(-4000),
        running: s.running,
        exit_code: s.exitCode,
      })),
    };
  },
  () => {
    const doc = globalThis.document;
    const root = doc.querySelector('[data-probe="composer"]');
    const popup = doc.querySelector('[data-probe="popup"]');
    const slot = doc.querySelector('[data-probe="slot"]');
    const el = field();
    const lineHeight = el ? parseFloat(getComputedStyle(el).lineHeight) || 1 : 1;
    return {
      rect: rect(root),
      field: rect(root?.querySelector(".field")),
      textarea: rect(el),
      lines: el ? Math.round(el.getBoundingClientRect().height / lineHeight) : 0,
      scroll_height: el ? Math.round(el.scrollHeight / lineHeight) : 0,
      mark: rect(root?.querySelector("[data-mark]")),
      mark_kind: root?.querySelector("[data-mark]")?.getAttribute("data-mark") ?? null,
      text_left: el ? el.getBoundingClientRect().x : null,
      chips: [...(root?.querySelectorAll("[data-chip]") ?? [])].map((chip) => ({ text: chip.textContent.trim(), rect: rect(chip) })),
      more: root?.querySelector("[data-more]") ? { text: root.querySelector("[data-more]").textContent.trim(), rect: rect(root.querySelector("[data-more]")) } : null,
      model: rect(root?.querySelector(".model")),
      send: rect(root?.querySelector(".send")),
      popup: popup && { rect: rect(popup), rows: [...popup.querySelectorAll("[data-row]")].map((row) => ({ text: row.textContent, rect: rect(row) })) },
      tray: rect(doc.querySelector('[data-probe="tray"]')),
      tray_rows: [...doc.querySelectorAll('[data-probe="tray"] [data-tray-row]')].map((row) => ({ text: row.textContent.replace(/\s+/g, " ").trim(), rect: rect(row) })),
      slot: slot && {
        rect: rect(slot),
        content: rect(slot.querySelector("[data-slot-content]")),
        buttons: [...slot.querySelectorAll("[data-slot-button]")].map((button) => ({
          label: button.getAttribute("data-slot-button"),
          selected: button.getAttribute("aria-pressed") === "true",
          rect: rect(button),
        })),
      },
      window: { w: globalThis.innerWidth, h: globalThis.innerHeight },
    };
  },
);

C.startComposer();
