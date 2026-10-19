// The right panel's own state and the values its parts share (P3.8).
//
//   panel                  {detailsOpen, focused, wheeled}. detailsOpen: the details
//                          overlay (SIDE-003), kept while the panel shows, as the
//                          terminal keeps detailsOpen (keypress.go:588-591), drawn only
//                          while the panel is hidden. focused, wheeled: focus is in the
//                          panel; a wheel turned it in the last 2 s (its scrollbar shows)
//   toggleDetails(), closeDetails()
//   meterNow(now)          meterModel for the shown chat (SIDE-007, SIDE-014, SIDE-019 to SIDE-024)
//   modelNow(now)          modelInfo for the details overlay's header
//   sectionRows({max})     the FILES, LSP, MCP and SKILLS rows (SIDE-008 to SIDE-011)
//   setPanelBody(el), panelBody()   the panel's scrolling body, for the panel keys (SIDE-012)
//   viewing                {fit, state, name, size}: what the picture viewer shows (4.5),
//                          kept by ImageViewer for the probe; state "idle", "loading",
//                          "shown" or "failed"
//   customizing            {cursor, picked}: the open Customize Composer form's rows
//   helpNow()              the help sheet's state for panel/help.js (SIDE-029, SIDE-035, TYPE-039)
//   toField(from)          focus back to the typing box (SIDE-012 Tab, SIDE-002)
//
// Everything is read from the stores' state (design 1.5.3); nothing here asks
// the engine for anything. panel.needsInit is the answer HelpSheet asks for
// when help opens with no chat shown.
import { compactionLimit } from "../../lib/compaction.js";
import { cliKind, footerItems, limitWindows, meterModel, modelInfo } from "../../lib/footer.js";
import { fileRows, modifiedFiles } from "../../lib/modified.js";
import { composer, focusField, hasChips } from "../../lib/stores/composer.svelte.js";
import { conn } from "../../lib/stores/connection.svelte.js";
import { limits } from "../../lib/stores/limits.svelte.js";
import { queue } from "../../lib/stores/queue.svelte.js";
import { busy, sessions } from "../../lib/stores/sessions.svelte.js";
import { todos } from "../../lib/stores/todos.svelte.js";
import { setFocus, ui } from "../../lib/stores/ui.svelte.js";
import { ws } from "../../lib/stores/workspace.svelte.js";
import { lspRows, mcpRows, skillRows } from "./rows.js";

export const panel = $state({ detailsOpen: false, focused: false, wheeled: false, needsInit: false });

export const customizing = $state({ cursor: 0, picked: [] });

export const viewing = $state({ fit: true, state: "idle", name: "", size: "" });

export function toggleDetails() {
  panel.detailsOpen = !panel.detailsOpen;
}

export function closeDetails() {
  panel.detailsOpen = false;
}

const shownSession = () => (sessions.currentId ? (sessions.byId[sessions.currentId] ?? null) : null);

// The agent CLI's windows for the model: null while checking, [] for none.
function windowsNow(now) {
  const kind = cliKind(ws.agent, ws.providers);
  return kind ? limitWindows(limits.byKind[kind], ws.agent?.model_cfg?.model ?? "", now) : [];
}

export function meterNow(now = Date.now()) {
  const session = shownSession();
  const limit = compactionLimit({ agent: ws.agent, session, providers: ws.providers, options: ws.config?.options });
  return meterModel({ items: footerItems(ws.config), session, limit, limits: windowsNow(now) });
}

export function modelNow(now = Date.now()) {
  return modelInfo({ agent: ws.agent, providers: ws.providers, session: shownSession(), limits: windowsNow(now) ?? [] });
}

// Each list as {rows, more}; files' more is its "…and N more" text, the
// others' a count (rows.js).
export function sectionRows({ max = Infinity } = {}) {
  const sid = sessions.currentId;
  return {
    files: fileRows(modifiedFiles(sid ? ws.files[sid] : []), { cwd: conn.project.path || conn.project.cwd, max }),
    lsp: lspRows(ws.lsps, { max }),
    mcp: mcpRows(ws.mcps, ws.config?.mcp, { max }),
    skills: skillRows(ws.skills, ws.config?.options?.disabled_skills, { max }),
  };
}

let body = null;

export function setPanelBody(element) {
  body = element;
}

export function panelBody() {
  return body;
}

export function helpNow() {
  const sid = sessions.currentId;
  const queued = sid ? (queue.bySession[sid]?.count ?? 0) : 0;
  const unfinished = (sessions.byId[sid]?.todos ?? []).some((t) => t?.status !== "completed");
  const slot = ui.slot ? { entries: ui.slot.props?.help ?? [], collapsedHelp: ui.slot.props?.collapsedHelp ?? "" } : null;
  return {
    screen: sid ? "chat" : panel.needsInit ? "init" : "landing",
    focus: ui.focus,
    busy: sid ? busy(sid) : false,
    queued,
    hasSession: Boolean(sid),
    typed: Boolean(composer.drafts[sid ?? ""]?.text),
    focusOnly: ws.config?.options?.tui?.composer_focus_only === true,
    images: ws.agent?.model?.supports_attachments === true,
    chips: hasChips(),
    backgroundable: false, // phase 6 brings background commands (help.go:44-46)
    pills: todos.open && unfinished && queued > 0,
    slot,
  };
}

// The composer's own focusField (P3.7) first; while that leaves focus where
// it was (in `from`, or nowhere), the conversation's typing field itself.
export function toField(from = null, doc = globalThis.document) {
  focusField();
  const active = doc?.activeElement;
  if (doc && (!active || active === doc.body || from?.contains(active))) {
    doc.querySelector("main.conversation textarea, main.conversation [contenteditable='true']")?.focus({ preventScroll: true });
  }
  if (ui.focus === "panel") setFocus(ui.slot ? "slot" : "composer");
}
