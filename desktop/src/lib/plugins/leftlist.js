// The left list's registrations (desktop-design-5-6 2.20 to 2.23; LST): the
// list sections "terminals" (after the chats) and "servers" (at the bottom)
// and the "‹‹" control (in the header place, drawn on the wordmark's row);
// the phase 6 row state "Done"; the sheet "folder_picker"; the close step
// "draft"; window-request's newChat; the list keys; the reads that keep the
// chats' states, the server poll and the list's saved collapsed choice; and
// the probe's "leftlist" section.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { untrack } from "svelte";
import CollapseButton from "../../components/leftlist/CollapseButton.svelte";
import FolderPicker from "../../components/leftlist/FolderPicker.svelte";
import TerminalRows from "../../components/leftlist/TerminalRows.svelte";
import { runAction } from "../actions.js";
import { registerCloseStep } from "../closing.js";
import { registerDialog } from "../dialogs.js";
import { app_info, settings_get } from "../engine/commands.js";
import { onEvent, onResync } from "../engine/events.js";
import { bindKey } from "../keys.js";
import { isShown, listRows, moveShown, orderedTerminals, showNumber, shownTerminal } from "../leftlist/numbers.js";
import { SERVER_HINT, foldOf, isPicked, readServers, rowTexts, serverKey, shownRows, unpick } from "../leftlist/servers.js";
import { chatOfEvent, chatState, forgetChat, phase3Kind, pollWorking, readSoon, rowStateOf, unknown } from "../leftlist/states.js";
import { applySettings, leftList, openPicker, toggleList, watch } from "../leftlist/store.svelte.js";
import { forget, noteState, unread } from "../leftlist/unread.js";
import { DRAFT_STEP, closeThisWindow, discardDraft } from "../leftlist/windowclose.js";
import { registerListSection } from "../listsections.js";
import { closeMain, mainView } from "../mainview.js";
import { probeSection } from "../probe.js";
import { registerRowState } from "../rowstates.js";
import { conn } from "../stores/connection.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { askCloseTerminal, terminals } from "../terminal/tabs.svelte.js";

registerListSection({ id: "collapse", order: 0, place: "header", component: CollapseButton });
registerListSection({ id: "terminals", order: 10, place: "rows", visible: () => terminals.length > 0, component: TerminalRows });
// The servers are the right panel's SERVERS (owner, 2026-10-19); the list
// still reads them (below).
registerRowState(rowStateOf);
registerDialog("folder_picker", FolderPicker);
registerCloseStep({ ...DRAFT_STEP, run: discardDraft });

// -- Keys (input.go:31-57). The session host takes its keys before the tab
// does, so they are bound for a terminal tab's focus ("main") too. --

function listKey(combo, handler) {
  bindKey("global", combo, handler);
  bindKey("main", combo, handler);
}

// Alt+W: the shown terminal tab's close box, else P4.1's chat.close (2.25).
function closeShown() {
  const id = shownTerminal();
  if (id) askCloseTerminal(id);
  else runAction("chat.close");
  return true;
}

listKey("Alt+N", () => (openPicker("session"), true));
listKey("Alt+T", () => (openPicker("terminal"), true));
listKey("Alt+S", toggleList);
listKey("Alt+W", closeShown);
listKey("Alt+Shift+ArrowUp", () => (moveShown(-1), true));
listKey("Alt+Shift+ArrowDown", () => (moveShown(1), true));
for (let n = 1; n <= 9; n += 1) listKey(`Alt+${n}`, () => (showNumber(n), true));
// Also while a terminal has the focus, when "global" is not asked (LIST-026).
listKey("Ctrl+Shift+W", closeThisWindow);
// A picked server row takes its keys first; any other key lets go of it.
bindKey("list", "*", serverKey);

if (typeof document !== "undefined") {
  // A click anywhere but a server row lets go of the picked one (input.go:164-166).
  document.addEventListener(
    "pointerdown",
    (event) => {
      if (leftList.picked && !event.target?.closest?.(".servers-part .server")) unpick();
    },
    true,
  );
  // A click on a chat row while a terminal tab is shown shows the chat again.
  document.addEventListener(
    "click",
    (event) => {
      if (mainView.current?.kind === "terminal" && event.target?.closest?.("nav.chats .chat[data-id] .row")) closeMain();
    },
    true,
  );
}

// -- The chats' states (2.20) --

// Unread: a chat whose work ends while this window does not show it.
watch(() => {
  const shownChat = shownTerminal() ? "" : sessions.currentId;
  for (const id of sessions.order) noteState(id, phase3Kind(id), id === shownChat);
});

// A read when an event names a chat; a new chat is unknown until one lands.
for (const type of ["message", "session", "run_complete", "agent_event", "helper"]) {
  onEvent(type, (kind, payload) => {
    if (type === "session" && payload?.id) {
      if (kind === "deleted") {
        forget(payload.id);
        forgetChat(payload.id);
        return;
      }
      if (kind === "created" && !payload.parent_session_id) unknown.add(payload.id);
    }
    readSoon(chatOfEvent(type, payload));
  });
}
onResync(() => {
  for (const id of [...unknown]) readSoon(id);
});

// Every 1 s, the chats that work or run in the background. The timer runs
// while connected and pollWorking picks those chats on each tick: an effect
// that read the chat records would restart it on every event-driven read,
// and a busy chat's reads could then put the checks off for good (2.20).
watch(() => {
  if (conn.state !== "connected") return;
  const timer = setInterval(pollWorking, 1000);
  return () => clearInterval(timer);
});

// -- Server rows (2.22): G-18 every 1 s while connected --

watch(() => {
  if (conn.state !== "connected") return;
  untrack(readServers);
  const timer = setInterval(readServers, 1000);
  return () => clearInterval(timer);
});

// -- The saved collapsed choice, the home folder, a new chat on request --

settings_get()
  .then(applySettings)
  .catch(() => {});
app_info()
  .then((info) => (leftList.home = typeof info?.home === "string" ? info.home : ""))
  .catch(() => {});
try {
  const win = getCurrentWebviewWindow();
  win.listen("settings-changed", (event) => applySettings(event?.payload)).catch(() => {});
  // window_open focused this window for a new chat (2.21; 6.4 window-request).
  win
    .listen("window-request", (event) => {
      if (event?.payload?.newChat === true) runAction("chat.new");
    })
    .catch(() => {});
} catch {
  // Not inside the app window (unit tests).
}

// -- The probe (1.9) --

const PICKER_HINT = "↑↓ pick · tab open folder · enter start · esc cancel"; // draw.go:673

function rectOf(element) {
  if (!element) return null;
  const r = element.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

function all(selector) {
  return [...document.querySelectorAll(selector)];
}

function one(selector) {
  return document.querySelector(selector);
}

// The drawn text of an element cut from the front: true when part is hidden.
function cut(element) {
  return Boolean(element) && element.scrollWidth > element.clientWidth + 1;
}

probeSection(
  "leftlist",
  () => {
    const shown = shownRows();
    const picker = leftList.picker;
    return {
      collapsed: leftList.collapsed,
      forced: leftList.forced,
      strip: leftList.strip,
      failure: leftList.failure,
      home: leftList.home,
      rows: listRows().map((row) => ({
        kind: row.kind,
        id: row.id,
        title: row.title,
        number: row.number,
        state: row.kind === "terminal" ? "terminal" : chatState(row.id),
        shown: isShown(row),
      })),
      unread: [...unread],
      unknown: [...unknown],
      terminals: orderedTerminals().map((tab) => tab.id),
      dragging: leftList.dragging,
      servers: shown.map((row) => ({
        ...rowTexts(row),
        port: row.server.port,
        session: row.server.session,
        ports: row.ports,
        child: row.child,
        fold: row.ports > 0 ? foldOf(row.server) : "",
        open: row.ports > 0 && Boolean(leftList.openFolds[foldOf(row.server)]),
        picked: isPicked(row),
      })),
      server_count: leftList.servers.length,
      server_room: Number.isFinite(leftList.serverRoom) ? leftList.serverRoom : null,
      hint: shown.some((row) => isPicked(row)) ? SERVER_HINT : "",
      picker: picker && {
        kind: picker.kind,
        title: picker.kind === "terminal" ? "New terminal in…" : "New session in…",
        hint: PICKER_HINT,
        base: picker.base,
        input: picker.input,
        cursor: picker.cursor,
        sel: picker.sel,
        matches: [...picker.matches],
        recent: [...picker.recent],
        err: picker.err,
      },
    };
  },
  () => {
    const field = one(".folder-picker .path-field");
    const scroll = one("nav.chats .scroll");
    return {
      px: parseFloat(getComputedStyle(document.documentElement).fontSize) / 16,
      column: rectOf(one(".chats-column")),
      collapse: rectOf(one("nav.chats .collapse")),
      wordmark: rectOf(one("nav.chats .wordmark")),
      strip: rectOf(one("nav.strip")),
      strip_expand: rectOf(one("nav.strip .expand")),
      strip_new_session: rectOf(one("nav.strip .new-session")),
      strip_new_terminal: rectOf(one("nav.strip .new-terminal")),
      strip_rule: rectOf(one("nav.strip .group-rule")),
      strip_cells: all("nav.strip .cell").map((el) => ({
        row: el.dataset.row,
        number: el.querySelector(".number")?.textContent ?? "",
        rect: rectOf(el),
        mark: rectOf(el.querySelector(".icon")),
        state: [...el.classList].find((c) => ["terminal", "waiting", "working", "background", "unread", "ready", "unknown"].includes(c)) ?? "",
        color: getComputedStyle(el).color,
        shown: el.classList.contains("shown"),
      })),
      scroll: scroll && { top: scroll.scrollTop, height: scroll.clientHeight, content: scroll.scrollHeight, rect: rectOf(scroll) },
      terminals: {
        heading: rectOf(one("nav.chats .terminals .group")),
        heading_text: one("nav.chats .terminals .group .label")?.textContent ?? "",
        add: rectOf(one("nav.chats .terminals .add")),
        rows: all("nav.chats li.terminal").map((el) => ({ id: el.dataset.id, rect: rectOf(el), selected: el.classList.contains("selected") })),
      },
      failure: rectOf(one("nav.chats .start-failure")),
      failure_text: one("nav.chats .start-failure")?.textContent ?? "",
      servers: {
        rule: rectOf(one("nav.chats .servers-part .servers-rule")),
        rows: all("nav.chats .servers-part .server").map((el) => ({
          port: Number(el.dataset.port),
          rect: rectOf(el),
          label: rectOf(el.querySelector(".label")),
          right: rectOf(el.querySelector(".right")),
          stop: rectOf(el.querySelector(".stop")),
          cross: rectOf(el.querySelector(".stop .icon")),
          picked: el.classList.contains("picked"),
        })),
        hint: rectOf(one("nav.chats .servers-part .hint")),
        hint_text: one("nav.chats .servers-part .hint")?.textContent ?? "",
      },
      picker: field && {
        frame: rectOf(one(".folder-picker")?.closest('[role="dialog"]')),
        field: rectOf(one(".folder-picker .path")),
        field_value: field.value,
        caret: field.selectionStart,
        focused: document.activeElement === field,
        browse: rectOf(one(".folder-picker .browse")),
        error: rectOf(one(".folder-picker .error")),
        error_cut: cut(one(".folder-picker .error")),
        heading: rectOf(one(".folder-picker .heading")),
        rows: all(".folder-picker .row").map((el) => ({ path: el.dataset.path, text: el.textContent, rect: rectOf(el), picked: el.classList.contains("picked") })),
      },
      // Each chat row's state line as drawn (phase 3's or a registered one) and its mark.
      chat_rows: all("nav.chats li.chat[data-id]").map((li) => ({
        id: li.dataset.id,
        rect: rectOf(li),
        state_text: li.querySelector(".state")?.textContent.trim() ?? "",
        registered: Boolean(li.querySelector(".state.registered")),
        mark: rectOf(li.querySelector(".state .icon")),
      })),
      // The tool lines of the shown chat (a click opens a tool and its picture).
      tool_lines: all("[data-tool] > .tool-line, .group > .tool-row").map((el) => ({
        tool: el.closest("[data-tool]")?.dataset.tool ?? "",
        rect: rectOf(el),
      })),
      // Pictures in the page (not in a sheet): whether each is on top at its centre.
      images: all("img")
        .filter((img) => !img.closest(".veil") && img.getBoundingClientRect().width > 0)
        .map((img) => {
          const r = img.getBoundingClientRect();
          return { rect: rectOf(img), on_top: document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2) === img };
        }),
    };
  },
);
