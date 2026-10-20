// Terminal tabs (desktop-design-5-6 2.25, 7.4; TERM): a shell (or a command,
// such as the external editor, 2.26) in a pseudo-terminal on the Rust side
// (pty.rs), drawn by the terminal widget (@xterm/xterm) in the middle column.
// A port of the session host's terminal sessions
// (internal/ui/sessionhost/terminal.go, session.go, host.go, input.go).
//
// `terminals` is the window's tab list, in order, for the left list (LST)
// and the phone share's setup step (SHARE): {id, title, cwd, exited, state,
// error}, where exited is a Promise of {code, signal?, error?} and state is
// "starting", "running" or "failed". The rest of a tab (the widget, the pty)
// stays here. Nothing is kept: output is not saved, as the close box says.

import { Channel } from "@tauri-apps/api/core";
import { tick } from "svelte";
import TerminalTab from "../../components/terminal/TerminalTab.svelte";
import { closeDialog, dialogs, hasDialog, openDialog } from "../dialogs.js";
import { app_info, clipboard_write, ptyClose, ptyCloseAll, ptyCwd, ptyResize, ptySpawn, ptyWrite } from "../engine/commands.js";
import { closeMain, mainView, showMain } from "../mainview.js";
import { focusField } from "../stores/composer.svelte.js";
import { theme } from "../themes/current.svelte.js";
import * as K from "./keys.js";
import { pageZoom, themeFromPage, xtermType } from "./xtermtheme.js";

export const terminals = $state([]); // [{id, title, cwd, exited, state, error}]

const tabs = new Map(); // id → the tab's own parts (below)
let count = 0;
let home = "";
let hostName = ""; // this computer's name (app_info's host), for OSC 7 (terminal.go:100-115)
const status = $state({ windowFocused: typeof document === "undefined" ? false : document.hasFocus() });

// The xterm modules, loaded on the first tab.
let xterm = null;
function loadXterm() {
  xterm ??= Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit"), import("@xterm/addon-image")]).then(
    ([core, fit, image]) => ({ Terminal: core.Terminal, FitAddon: fit.FitAddon, ImageAddon: image.ImageAddon }),
  );
  return xterm;
}

// -- The interface (7.4) --

// Opens a tab running argv (else the user's shell) in cwd, titled title
// ("Terminal", terminal.go:50), shows it at once, and returns {id, exited,
// started}. A failed start shows its error in the tab (look 5.18), resolves
// exited with {code: null, error} and started with {error}; started resolves
// with {error: null} once the shell runs (or the tab closed first), so the
// folder picker's failure line can follow it (LIST-048).
export function openTerminal({ cwd, argv = null, title = "Terminal" } = {}) {
  return openTab({ cwd, argv, title });
}

// The tab's view in the middle column (the chat stays mounted underneath).
export function showTerminal(id) {
  const tab = tabs.get(id);
  if (!tab) return;
  if (shownId() === id) {
    tab.focus?.();
    return;
  }
  showMain({ kind: "terminal", component: TerminalTab, props: { id } });
}

// The list's moves and drags (LIST-009): `terminals` is the one tab order,
// so the tab shown after a close is the row that followed it (host.go:412-441).
export function moveTab(id, to) {
  const from = terminals.findIndex((entry) => entry.id === id);
  if (from < 0) return false;
  const target = Math.min(Math.max(to, 0), terminals.length - 1);
  if (target === from) return false;
  const [entry] = terminals.splice(from, 1);
  terminals.splice(target, 0, entry);
  return true;
}

// Alt+W or the row's ×: the close box (TERM's sheet terminal_close) over the
// shown view; another tab's box gives way to it (input.go:44-46, host.go:473-478).
export function askCloseTerminal(id) {
  if (!tabs.has(id)) return;
  if (hasDialog("terminal_close")) closeDialog("terminal_close");
  openDialog("terminal_close", { id });
}

// Closing the window or quitting (the close step `terminals`, 2.23): every
// tab stops, at most 6 s (pty_close_all, run.go:32-51); an open editor's
// file is read and deleted on the way (2.26).
export async function closeAllTerminals() {
  const all = [...tabs.values()];
  for (const tab of all) {
    tab.closed = true;
    removeTab(tab, { show: false });
  }
  if (mainView.current?.kind === "terminal") closeMain();
  await ptyCloseAll().catch(() => {});
  await Promise.race([Promise.allSettled(all.map((tab) => tab.after)), sleep(2000)]);
}

// -- For TERM's own parts (the tab view, the close box, the editor, the plugin) --

// The tab's parts, or undefined.
export function getTab(id) {
  return tabs.get(id);
}

// The id of the tab the middle column shows, or null.
export function shownId() {
  const view = mainView.current;
  return view?.kind === "terminal" ? (view.props?.id ?? null) : null;
}

export function shownTab() {
  const id = shownId();
  return id === null ? undefined : tabs.get(id);
}

// The close box's "Close" (answerClose, host.go:480-496): the tab leaves the
// list at once, then its programs are stopped (pty_close, 2.25).
export function closeTerminal(id) {
  const tab = tabs.get(id);
  if (!tab) return;
  tab.closed = true;
  removeTab(tab, { show: true });
  if (tab.ptyId) ptyClose(tab.ptyId).catch(() => {});
}

// A tab whose process ends shows the chat afterwards (the editor, 2.26 step
// 5); otherwise the next tab, else the one before (host.go:409-438).
export function openTab({ cwd, argv = null, title = "Terminal", backToChat = false, after = null }) {
  const id = `term-${++count}`;
  let resolveExited, resolveStarted;
  const exited = new Promise((resolve) => (resolveExited = resolve));
  const started = new Promise((resolve) => (resolveStarted = resolve));
  terminals.push({ id, title, cwd: cwd ?? "", exited, state: "starting", error: "" });
  const tab = {
    id,
    entry: terminals[terminals.length - 1],
    argv,
    backToChat,
    exited,
    resolveExited,
    resolveStarted,
    after: null, // the work that waits for the end (the editor's file)
    term: null,
    fit: null,
    created: null, // a Promise of the widget
    opened: false,
    ptyId: null,
    pid: null,
    closed: false,
    gone: false,
    outbox: [],
    writing: false,
    wheel: { rest: 0 },
    focusState: null,
    scrolledBack: false,
    oscFolder: null,
    cwdTimer: null,
    sized: null, // resolves once the view gave the widget its size
    focus: null, // set by the view while it shows this tab
  };
  tab.sized = new Promise((resolve) => (tab.markSized = resolve));
  tabs.set(id, tab);
  if (after) tab.after = after(exited);
  tab.created = create(tab);
  showTerminal(id);
  start(tab, cwd);
  return { id, exited, started };
}

// -- Starting --

async function create(tab) {
  const { Terminal, FitAddon } = await loadXterm();
  const term = new Terminal({
    ...xtermType(pageZoom()),
    theme: themeFromPage(),
    scrollback: 10000, // x/vt's DefaultScrollbackSize (scrollback.go:10)
    cursorStyle: "block",
    cursorBlink: false,
    scrollOnUserInput: true,
    allowProposedApi: true,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  tab.term = term;
  tab.fit = fit;
  wire(tab);
  return term;
}

async function start(tab, cwd) {
  await tab.created;
  // The view sizes the grid first; a tab that is not shown starts at 80×24.
  await Promise.race([tab.sized, sleep(1000)]);
  if (tab.closed) return finishStart(tab, null);
  if (!home) {
    const info = await app_info().catch(() => null);
    home = info?.home ?? "";
    hostName = info?.host ?? "";
  }
  const channel = new Channel();
  channel.onmessage = (message) => fromPty(tab, message);
  try {
    const spawned = await ptySpawn(cwd, tab.argv, tab.term.cols, tab.term.rows, channel);
    tab.ptyId = spawned.id;
    tab.pid = spawned.pid;
    tab.resolveStarted({ error: null });
    if (tab.closed) {
      ptyClose(spawned.id).catch(() => {});
      return;
    }
    if (tab.entry.state === "starting") tab.entry.state = "running";
    pumpWrites(tab);
    refreshCwd(tab);
  } catch (error) {
    finishStart(tab, error?.message ?? String(error));
  }
}

// A start that failed (or a tab closed while starting): the error stays in
// the tab, and exited resolves.
function finishStart(tab, error) {
  tab.outbox = [];
  if (error !== null) {
    tab.entry.state = "failed";
    tab.entry.error = error;
  }
  tab.gone = true;
  tab.resolveStarted({ error });
  tab.resolveExited({ code: null, error: error ?? "closed" });
}

// -- Output, input and the end --

function fromPty(tab, message) {
  if (message?.data !== undefined) {
    if (tab.term && !tab.disposed) tab.term.write(K.fromBase64(message.data));
    scheduleCwd(tab);
  } else if (message?.exit) {
    ended(tab, message.exit);
  }
}

// The shell or command ended: the tab closes by itself (LIST-046), unless the
// user closed it already.
function ended(tab, exit) {
  tab.gone = true;
  tab.outbox = [];
  tab.resolveExited({ code: exit.code ?? null, signal: exit.signal ?? null, closed: tab.closed });
  if (!tab.closed) removeTab(tab, { show: true });
}

// Takes the tab out of the list; when it was shown, shows the next tab, else
// the one before, else the chat (host.go:412-441); an open close box for it
// goes too (host.go:423-425).
function removeTab(tab, { show }) {
  const at = terminals.findIndex((entry) => entry.id === tab.id);
  const wasShown = shownId() === tab.id;
  if (at >= 0) terminals.splice(at, 1);
  tabs.delete(tab.id);
  if (closeBoxFor() === tab.id) closeDialog("terminal_close");
  if (wasShown && show) {
    const next = tab.backToChat ? null : (terminals[at] ?? terminals[at - 1] ?? null);
    if (next) showTerminal(next.id);
    else if (closeMain()) refocusChat();
  }
  clearTimeout(tab.cwdTimer);
  // The widget goes once its view let go of it.
  queueMicrotask(() => {
    tab.disposed = true;
    tab.term?.dispose();
  });
}

// closeMain focuses the typing box while the chat is still hidden (it shows
// on the next redraw), so the focus is given again once it shows.
function refocusChat() {
  tick().then(() => {
    if (!mainView.current) focusField();
  });
}

function closeBoxFor() {
  const box = dialogs.find((entry) => entry.id === "terminal_close");
  return box?.props?.id ?? null;
}

// Input in order: one pty_write at a time, the rest gathered meanwhile.
export function sendInput(tab, bytes) {
  if (tab.gone) return;
  tab.outbox.push(typeof bytes === "string" ? K.utf8(bytes) : bytes);
  pumpWrites(tab);
}

async function pumpWrites(tab) {
  if (tab.writing || !tab.ptyId) return;
  tab.writing = true;
  while (tab.outbox.length > 0 && !tab.gone) {
    const parts = tab.outbox.splice(0);
    const all = new Uint8Array(parts.reduce((n, part) => n + part.length, 0));
    let at = 0;
    for (const part of parts) {
      all.set(part, at);
      at += part.length;
    }
    try {
      await ptyWrite(tab.ptyId, K.toBase64(all));
    } catch {
      tab.outbox = [];
    }
  }
  tab.writing = false;
}

// A paste: jumps back to the newest output and keeps the selection
// (LIST-051; host.go:199-202). Nothing while a sheet is open (input.go:137-142).
export function pasteInto(tab, text) {
  if (!tab?.term || !text || dialogs.length > 0) return;
  sendInput(tab, K.pasteData(text, tab.term.modes.bracketedPasteMode));
  tab.term.scrollToBottom();
}

// The selection to the clipboard (on release, and Ctrl+Shift+C).
export function copySelection(tab) {
  const text = tab?.term?.hasSelection() ? tab.term.getSelection() : "";
  if (text) clipboard_write(text).catch(() => {});
  return text !== "";
}

// -- The widget's handlers --

function wire(tab) {
  const term = tab.term;
  term.onData((data) => {
    if (K.isFocusReport(data)) return;
    sendInput(tab, data);
  });
  term.onBinary((data) => sendInput(tab, K.binaryBytes(data)));
  // A program's copy sets the clipboard; reads are refused (LIST-023).
  term.parser.registerOscHandler(52, (data) => {
    const text = K.osc52Text(data);
    if (text !== null) clipboard_write(text).catch(() => {});
    return true;
  });
  // The folder a shell reports (terminal.go:100-117); /proc wins where it can be read.
  term.parser.registerOscHandler(7, (data) => {
    const folder = K.osc7Folder(data, hostName);
    if (folder) {
      tab.oscFolder = folder;
      refreshCwd(tab);
    }
    return true;
  });
  // Keys: the zoom keys stay the window's; nothing reaches the program while
  // a sheet is open, as the close box takes every key (input.go:13-26).
  term.attachCustomKeyEventHandler((event) => {
    if (dialogs.length > 0) return false;
    if ((event.ctrlKey || event.metaKey) && !event.altKey && ["+", "=", "-", "_", "0"].includes(event.key)) return false;
    return true;
  });
  term.onScroll(() => cursorFollowsScroll(tab));
  term.buffer.onBufferChange(() => cursorFollowsScroll(tab));
  term.onRender(() => hideCursorWhileBack(tab));
}

// The cursor hides while scrolled back, except on the alternate screen
// (draw.go:192-203): the widget draws it, so the drawn cell loses its cursor
// classes after each render while back.
function cursorFollowsScroll(tab) {
  const buffer = tab.term.buffer.active;
  const back = buffer.type === "normal" && buffer.viewportY < buffer.baseY;
  if (back === tab.scrolledBack) return;
  tab.scrolledBack = back;
  tab.term.refresh(0, tab.term.rows - 1);
}

const CURSOR_CLASSES = ["xterm-cursor", "xterm-cursor-blink", "xterm-cursor-block", "xterm-cursor-bar", "xterm-cursor-underline", "xterm-cursor-outline"];

function hideCursorWhileBack(tab) {
  if (!tab.scrolledBack || !tab.term.element) return;
  for (const cell of tab.term.element.querySelectorAll(".xterm-rows .xterm-cursor")) cell.classList.remove(...CURSOR_CLASSES);
}

// The wheel (terminal.go:159-188): a program that asked for the mouse gets
// it; otherwise 3 lines a notch, or ↑/↓ three times on the alternate screen.
export function wheel(tab, event) {
  const term = tab.term;
  if (!term || term.modes.mouseTrackingMode !== "none") return false;
  const notches = K.wheelNotches(tab.wheel, event);
  if (notches !== 0) {
    if (term.buffer.active.type === "alternate") {
      sendInput(tab, K.arrowKeys(notches < 0, term.modes.applicationCursorKeysMode, K.WHEEL_LINES * Math.abs(notches)));
    } else {
      term.scrollLines(notches * K.WHEEL_LINES);
    }
  }
  return true;
}

// Shift+PgUp / Shift+PgDn (terminal.go:140-157).
export function shiftPage(tab, down) {
  const term = tab?.term;
  if (!term) return;
  if (term.buffer.active.type === "alternate") sendInput(tab, K.shiftPageKey(down));
  else term.scrollLines((down ? 1 : -1) * Math.max(term.rows - 1, 1));
}

// Shift+Enter: Ctrl+J, typed (it jumps back and clears the selection, input.go:58-69).
export function shiftEnter(tab) {
  tab?.term?.input(K.SHIFT_ENTER, true);
}

// -- Size, folder, focus and colors --

// The view gives the grid its size; a change goes to the program and clears
// the selection (session.go:372-376).
export function fitTab(tab) {
  const term = tab?.term;
  if (!term?.element?.isConnected) return;
  const type = xtermType(pageZoom());
  if (term.options.fontSize !== type.fontSize) term.options.fontSize = type.fontSize;
  if (term.options.lineHeight !== type.lineHeight) term.options.lineHeight = type.lineHeight;
  const before = `${term.cols}x${term.rows}`;
  tab.fit.fit();
  if (`${term.cols}x${term.rows}` !== before) {
    term.clearSelection();
    if (tab.ptyId && !tab.gone) ptyResize(tab.ptyId, term.cols, term.rows).catch(() => {});
  }
  tab.markSized();
}

function scheduleCwd(tab) {
  if (tab.cwdTimer) return;
  tab.cwdTimer = setTimeout(() => {
    tab.cwdTimer = null;
    refreshCwd(tab);
  }, 200);
}

// The folder: the shell's /proc entry, else what OSC 7 reported, else the
// one it started in (workingDir, terminal.go:89-98).
async function refreshCwd(tab) {
  if (!tab.ptyId || tab.gone) return;
  const dir = await ptyCwd(tab.ptyId).catch(() => null);
  const next = dir || tab.oscFolder;
  if (next && !tab.gone && tab.entry.cwd !== next) tab.entry.cwd = next;
}

// The folder as the head's path line: the home folder as "~".
export function shortFolder(dir) {
  if (!dir) return "";
  if (home && (dir === home || dir.startsWith(`${home}/`))) return `~${dir.slice(home.length)}`;
  return dir;
}

// Focus reports follow whether the tab is shown and the window focused
// (LIST-023, LIST-052's rule; host.go:224-233, :343-351).
function reportFocus() {
  const shown = shownId();
  for (const tab of tabs.values()) {
    const focused = tab.id === shown && status.windowFocused;
    if (tab.focusState === focused) continue;
    tab.focusState = focused;
    if (tab.term?.modes.sendFocusMode && tab.ptyId) sendInput(tab, K.focusReport(focused));
  }
}

if (typeof window !== "undefined") {
  window.addEventListener("focus", () => (status.windowFocused = true));
  window.addEventListener("blur", () => (status.windowFocused = false));
}

$effect.root(() => {
  $effect(() => {
    void mainView.current;
    void status.windowFocused;
    reportFocus();
  });
  // A theme change recolors every tab at once (terminal.go:61-80).
  $effect(() => {
    void theme.revision;
    void theme.name;
    void theme.dark;
    const colors = themeFromPage();
    for (const tab of tabs.values()) if (tab.term) tab.term.options.theme = colors;
  });
});

// -- The probe's `terminal` section (8.2) --

export function probeLight() {
  const box = typeof document === "undefined" ? null : document.querySelector('[data-sheet="terminal_close"]');
  const tokens = typeof document === "undefined" ? null : themeFromPage();
  return {
    shown: shownId(),
    window_focused: status.windowFocused,
    tokens: tokens && { background: tokens.background, foreground: tokens.foreground, ansi1: tokens.red, selection: tokens.selectionBackground },
    close_box: box && {
      for: closeBoxFor(),
      question: box.querySelector(".question")?.textContent ?? "",
      detail: box.querySelector(".detail")?.textContent ?? "",
      buttons: [...box.querySelectorAll(".buttons button")].map((button) => button.textContent.trim()),
      selected: box.querySelector("[data-selected]")?.dataset.selected ?? null,
    },
    tabs: terminals.map((entry) => {
      const tab = tabs.get(entry.id);
      const term = tab?.term;
      const buffer = term?.buffer.active;
      return {
        id: entry.id,
        title: entry.title,
        cwd: entry.cwd,
        osc_folder: tab?.oscFolder ?? null,
        state: entry.state,
        error: entry.error,
        pid: tab?.pid ?? null,
        cols: term?.cols ?? null,
        rows: term?.rows ?? null,
        lines: term ? lastLines(term, 30) : [],
        screen: buffer?.type ?? null,
        scrolled_back: Boolean(buffer && buffer.viewportY < buffer.baseY),
        viewport_y: buffer?.viewportY ?? null,
        base_y: buffer?.baseY ?? null,
        selection: term?.getSelection() ?? "",
        modes: term
          ? {
              bracketed_paste: term.modes.bracketedPasteMode,
              mouse: term.modes.mouseTrackingMode,
              focus: term.modes.sendFocusMode,
              app_cursor: term.modes.applicationCursorKeysMode,
            }
          : null,
        focus_reported: tab?.focusState ?? null,
        cursor: term ? cursorState(term) : null,
        colors: term ? { background: term.options.theme?.background, foreground: term.options.theme?.foreground, ansi1: term.options.theme?.red, selection: term.options.theme?.selectionBackground } : null,
      };
    }),
  };
}

export function probeHeavy() {
  const box = document.querySelector('[data-sheet="terminal_close"]');
  const closeButtons = box ? [...box.querySelectorAll(".buttons button")].map((button) => ({ label: button.textContent.trim(), rect: rectOf(button) })) : [];
  const tab = shownTab();
  const term = tab?.term;
  const screen = term?.element?.querySelector(".xterm-screen");
  if (!screen) return { shown: null, close_buttons: closeButtons };
  const buffer = term.buffer.active;
  const rows = [];
  for (let y = 0; y < term.rows; y++) rows.push(buffer.getLine(buffer.viewportY + y)?.translateToString(true) ?? "");
  const rect = rectOf(screen);
  return {
    shown: tab.id,
    screen: rect,
    cell: { w: rect.w / term.cols, h: rect.h / term.rows },
    rows,
    background: getComputedStyle(term.element.querySelector(".xterm-scrollable-element") ?? term.element).backgroundColor,
    image_cells: imageCells(tab),
    close_buttons: closeButtons,
  };
}

function rectOf(element) {
  const rect = element.getBoundingClientRect();
  return { x: rect.x, y: rect.y, w: rect.width, h: rect.height };
}

// The newest lines of the buffer (scrollback included), wrapped rows joined.
function lastLines(term, n) {
  const buffer = term.buffer.active;
  const out = [];
  let line = "";
  for (let y = 0; y < buffer.length; y++) {
    const row = buffer.getLine(y);
    if (!row) continue;
    if (row.isWrapped) line += row.translateToString(true);
    else {
      if (y > 0) out.push(line);
      line = row.translateToString(true);
    }
  }
  out.push(line);
  while (out.length > 0 && out[out.length - 1] === "") out.pop();
  return out.slice(-n);
}

// The drawn cursor: its shape and blink from the cell's classes; none when hidden.
function cursorState(term) {
  const cell = term.element?.querySelector(".xterm-rows .xterm-cursor");
  if (!cell) return { visible: false, style: null, blink: false };
  const style = ["block", "bar", "underline", "outline"].find((name) => cell.classList.contains(`xterm-cursor-${name}`)) ?? null;
  return { visible: true, style, blink: cell.classList.contains("xterm-cursor-blink") };
}

// Pictures the image add-on holds for the visible cells.
function imageCells(tab) {
  const addon = tab.images;
  if (!addon?.getImageAtBufferCell) return 0;
  const buffer = tab.term.buffer.active;
  let found = 0;
  for (let y = buffer.viewportY; y < buffer.viewportY + tab.term.rows; y++) {
    for (let x = 0; x < tab.term.cols; x += 1) if (addon.getImageAtBufferCell(x, y)) found++;
  }
  return found;
}

// The image add-on, loaded once the widget is open (the view calls it).
export async function addPictures(tab) {
  if (tab.images || !tab.term) return;
  const { ImageAddon } = await loadXterm();
  tab.images = new ImageAddon();
  tab.term.loadAddon(tab.images);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
