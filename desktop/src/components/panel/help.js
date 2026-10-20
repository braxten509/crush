// The help sheet's entries (SIDE-029, SIDE-035, TYPE-039): a port of the
// terminal's full help, internal/ui/model/help.go:139-225, with its words
// (internal/ui/model/keys.go:93-341). The desktop always tells keys apart,
// so models and newline carry the words the terminal shows then
// (update_ui.go:86-91: "ctrl+m models", "shift+enter newline"). Ctrl+End,
// which the terminal lists with no words, takes lib/keys.js's ("jump to the
// bottom and follow"). Pure: HelpSheet gives it the state.
//
//   helpGroups(state) → [{id, label, entries: [{key, action}]}]
//
// state:
//   screen          "init" (the set-up prompt, START-002), "landing" (no chat
//                   shown) or "chat"
//   focus           ui.focus: composer, timeline, panel, list, tray or slot
//   busy, queued    the shown chat works; its queued prompt count
//   hasSession      a chat is open
//   typed           the field holds text
//   focusOnly       options.tui.composer_focus_only
//   images          the model takes pictures
//   chips           the field has attachments or kept skills (hasChips)
//   backgroundable  a running command can move to the background (phase 6)
//   pills           the to-dos are open with unfinished ones and a queue
//   slot            null, or the slot form's own help: {entries, collapsedHelp?}
//
// Groups follow the terminal's rows in its order; rows of one area share a
// group, whose small-caps label is the desktop's (4.14).

const e = (key, action) => ({ key, action });

const K = {
  quit: e("ctrl+c", "quit"),
  less: e("ctrl+g", "less"),
  models: e("ctrl+m", "models"),
  sessions: e("ctrl+s", "sessions"),
  yolo: e("ctrl+y", "toggle yolo"),
  shiftTab: e("shift+tab", "mode"),
  newSession: e("ctrl+n", "new chat"),
  endFollow: e("ctrl+end", "jump to the bottom and follow"),
  background: e("ctrl+b", "background command"),
  cancel: e("esc", "cancel"),
  interruptEsc: e("esc", "interrupt, send queued"),
  interrupt: e("ctrl+enter", "interrupt, send queued"),
  newline: e("shift+enter", "newline"),
  mention: e("@", "mention file"),
  openEditor: e("ctrl+o", "open editor"),
  pasteText: e("ctrl+shift+v", "paste text"),
  selectAll: e("ctrl+shift+a", "select all"),
  copySelection: e("ctrl+shift+c", "copy selection"),
  cutSelection: e("ctrl+shift+x", "cut selection"),
  addImage: e("ctrl+f", "add image"),
  pasteImage: e("ctrl+v", "paste image from clipboard"),
  deleteMode: e("ctrl+r+{i}", "delete attachment at index i"),
  deleteAll: e("ctrl+r+r", "delete all attachments"),
  cancelDelete: e("esc", "cancel delete mode"),
  upDown: e("↑↓", "scroll"),
  upDownOne: e("shift+↑↓", "scroll one item"),
  pageUp: e("b/pgup", "page up"),
  pageDown: e("f/pgdn", "page down"),
  halfUp: e("u", "half page up"),
  halfDown: e("d", "half page down"),
  home: e("g", "home"),
  end: e("G", "end"),
  copy: e("c/y", "copy"),
  clear: e("esc", "clear selection"),
  focusSidebar: e("l/→", "focus sidebar"),
  focusChat: e("h/←", "focus chat"),
  pills: e("←/→", "switch section"),
};

// "/ or ctrl+p" while the field has focus and is empty (help.go:19-26).
const commands = (s) => e(s.focus === "composer" && !s.typed ? "/ or ctrl+p" : "ctrl+p", "commands");

// Tab's words for the focus; hidden with composer_focus_only, where Tab does
// nothing (help.go:211-225). In the chat list Tab walks its rows' buttons,
// so it is hidden there too (the list is the desktop's own region).
function tab(s) {
  if (s.focusOnly || s.focus === "list") return null;
  return e("tab", s.focus === "composer" ? "focus chat" : "focus editor");
}

function editorRows(s) {
  const rows = [
    {
      id: "field",
      label: "FIELD",
      entries: [K.newline, K.mention, K.openEditor, K.pasteText, K.selectAll, K.copySelection, K.cutSelection, ...(s.images ? [K.addImage, K.pasteImage] : [])],
    },
  ];
  if (s.chips) rows.push({ id: "attachments", label: "ATTACHMENTS", entries: [K.deleteMode, K.deleteAll, K.cancelDelete] });
  return rows;
}

// The focused pane's rows (help.go:185-209). The tray is a tab stop of its
// own on the desktop, so its section keys show while it has focus
// (SIDE-033), where the terminal shows them in the chat pane.
function focusRows(s) {
  switch (s.focus) {
    case "composer":
      return editorRows(s);
    case "panel":
      return [{ id: "panel", label: "PANEL", entries: [K.upDown, K.focusChat, K.home, K.end] }];
    case "timeline":
      return [
        {
          id: "timeline",
          label: "TIMELINE",
          entries: [K.upDown, K.upDownOne, K.pageUp, K.pageDown, K.halfUp, K.halfDown, K.home, K.end, K.endFollow, K.focusSidebar, K.copy, K.clear],
        },
      ];
    case "tray":
      return s.pills ? [{ id: "tray", label: "TRAY", entries: [K.pills] }] : [];
    default:
      return [];
  }
}

function running(s) {
  if (!s.busy) return [];
  return s.queued > 0 ? [K.interruptEsc, K.interrupt] : [K.cancel];
}

const last = () => ({ id: "help", label: "HELP", entries: [K.less, K.quit] });

export function helpGroups(state = {}) {
  const s = { screen: "landing", focus: "composer", queued: 0, ...state };
  // A slot form shows its own help while it has focus (help.go:143-153).
  if (s.slot) {
    if (s.focus === "slot" || s.focus === "composer") {
      const entries = (s.slot.entries ?? []).filter((x) => x?.key && x?.action);
      return entries.length > 0 ? [{ id: "form", label: "FORM", entries }] : [];
    }
    const description = s.slot.collapsedHelp || "focus editor";
    return [
      { id: "form", label: "FORM", entries: [e(s.focusOnly ? "click" : "tab", description)] },
      { id: "timeline", label: "TIMELINE", entries: [K.upDown, K.upDownOne, K.pageUp, K.pageDown, K.halfUp, K.halfDown, K.home, K.end] },
    ];
  }
  const groups = [];
  if (s.screen === "init") {
    groups.push({ id: "setup", label: "SET UP", entries: [K.quit] });
  } else if (s.screen === "chat") {
    const run = running(s);
    if (run.length > 0) groups.push({ id: "running", label: "RUNNING", entries: run });
    const main = [tab(s), K.shiftTab, commands(s), K.models, K.sessions, K.yolo].filter(Boolean);
    if (s.hasSession) main.push(K.newSession, K.endFollow);
    if (s.backgroundable) main.push(K.background);
    groups.push({ id: "chat", label: "CHAT", entries: main });
    groups.push(...focusRows(s));
  } else if (!s.hasSession) {
    // The landing page: the full help's own rows, no Tab (SIDE-035).
    groups.push({ id: "chat", label: "CHAT", entries: [K.shiftTab, commands(s), K.models, K.sessions, K.yolo] });
    groups.push(...editorRows(s));
  }
  groups.push(last());
  return groups;
}

// The entries in order, as "key action" lines (the probe and the tests).
export function helpLines(groups) {
  return groups.flatMap((g) => g.entries.map((x) => `${x.key} ${x.action}`));
}
