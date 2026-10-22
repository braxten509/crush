// The typing box (desktop-design 1.5.3): each chat's draft, the one mode mark
// and the placeholder (TYPE-002, TYPE-003), sending (TYPE-004 to TYPE-008,
// TYPE-041 to TYPE-049), `!` commands (TYPE-020, TYPE-021), stopping
// (TYPE-046, TYPE-047), the input mode and YOLO (TYPE-023 to TYPE-025), the
// prompt history and queue recall (TYPE-017 to TYPE-019), pastes and `@`
// picks (TYPE-010 to TYPE-014, TYPE-026 to TYPE-030), the chips (TYPE-035 to
// TYPE-040, SIDE-026) and the plan handoff (TYPE-052 to TYPE-058). The rules
// are lib/composer's; this store runs them against the engine and the field.
// The field (Field.svelte) registers its textarea, so the caret can follow
// what the store puts in it; the slot's form registers how to focus it.
import { untrack } from "svelte";
import { openDialog } from "../dialogs.js";
import { api, uuid } from "../engine/api.js";
import { clipboard_read, clipboard_write, file_info, file_read, files_list } from "../engine/commands.js";
import { onEvent, onResync } from "../engine/events.js";
import { bangAfterPaste, bangAfterTyping, bangFromValue, historyEntries, joinDrafts, PromptHistory } from "../composer/history.js";
import { absolutePath, fileRows, insertPick, mentionQuery, mentionSpan, moveFocus, opensAt, pickFile, previewPick, rankRows, rankSlash, slashOpens, slashQuery } from "../composer/mentions.js";
import { AGENT_FOR, bannerFor, BUSY_SWITCH, cycleSteps, isQuitWord, markOf, pickPlaceholders, placeholderFor, SWITCHING, YOLO_BANNER, YOLO_OFF } from "../composer/modes.js";
import {
  attachPath,
  dropText,
  EMPTY_CLIPBOARD,
  NO_PICTURES,
  normalize,
  NOT_A_PICTURE,
  pastedPictures,
  pasteRoute,
  pictureAttachment,
  supportsImages,
  textAttachment,
  unreadable,
} from "../composer/paste.js";
import { planReadyPresent } from "../render/plan.js";
import { conn, refusedWhileFailed } from "./connection.svelte.js";
import { addPending, finishPending, markUncertain, messages, resolvePending, returnDraft, takeReturned } from "./messages.svelte.js";
import { queue, recall as recallQueued, recalled, refresh as refreshQueue } from "./queue.svelte.js";
import { busy, deferSend, ensureSession, sessions, upsertSession } from "./sessions.svelte.js";
import { notify, requestEnd, setFocus, ui } from "./ui.svelte.js";
import { loadAgent, ws } from "./workspace.svelte.js";

export const composer = $state({
  drafts: {}, // sid ("" = the landing page) → { text, attachments: Attachment[] (lib/types.js), bang, bangEmpty }
  mode: "normal", // "normal" | "plan" | "yolo" | "bang": the one mark shown (TYPE-002)
  placeholder: "", // TYPE-003
  switching: false, // a mode switch is in flight (TYPE-008)
  sending: false, // a send call is on its way
  deleteMode: false, // TYPE-038
  popup: null, // null | { kind: "@" | "/", start, query, rows: [{row, chars}], focused, loading } (TYPE-026 to TYPE-028; "/" the command menu)
  shells: {}, // request_id → ShellState (lib/types.js): live `!` commands, read by lib/timeline.js (P3.5)
  planHandoff: null, // null | { sessionId, open } (TYPE-052): a ready plan waits; open: its form is meant to show
  input: "code", // "code" | "plan": the input mode of the engine's main agent (TYPE-023)
  yolo: false, // the engine skips permission prompts (TYPE-024), as last read or set here
  picks: pickPlaceholders(), // this round's ready and working placeholders (TYPE-003)
  pendingKept: [], // [{ id, name, by_agent }]: skills turned on before the chat exists (TYPE-041)
  slotButtons: [], // the slot form's buttons [{ label, selected }], for the probe (1.9)
  slotFull: false, // the permission prompt covers the middle column (DLG-138)
  slotTall: false, // the slot form takes all of its 60%: a diff (contentViewportHeight, permissions.go:365-373)
});

const NOT_READY = "coder agent is not initialized"; // workspace.ErrAgentNotInitialized (internal/workspace/workspace.go:34)
const RECOVERED_IN_BANG = "Queued message recovered. Press Up outside shell mode to edit it."; // history.go:145
const IMPLEMENT = "Implement the plan."; // input_mode.go:103
const POLL_MS = 2000; // the queue and YOLO backstop (workspace_cache.go:42-46 polls faster, on every terminal message)

function errorText(error) {
  return error?.message ?? String(error);
}

// -- Drafts --

const NO_DRAFT = Object.freeze({ text: "", attachments: Object.freeze([]), bang: false, bangEmpty: false });

// The chat the field types into: the one opening while a switch is on its
// way, as a send then is (deferSend), else the shown one. Text typed while a
// chat loads stays when it shows (owner, 2026-10-20: "the text I type gets
// deleted").
export function typingChat() {
  return sessions.opening?.id ?? sessions.currentId;
}

// The chat's draft as stored, read only (a chat with none has an empty one).
export function draftOf(sid = typingChat()) {
  return composer.drafts[sid ?? ""] ?? NO_DRAFT;
}

function draftFor(sid = typingChat()) {
  const key = sid ?? "";
  if (!composer.drafts[key]) composer.drafts[key] = { text: "", attachments: [], bang: false, bangEmpty: false };
  return composer.drafts[key];
}

// -- The field and the slot form --

let field = null; // the textarea, while the field is on screen
let slotFocus = null; // focuses the slot's form, while one is open

export function attachField(el) {
  field = el;
  return () => {
    if (field === el) field = null;
  };
}

export function attachSlot(focus) {
  slotFocus = focus;
  return () => {
    if (slotFocus === focus) slotFocus = null;
  };
}

export function fieldElement() {
  return field;
}

// The field, unless a slot form is open (then the form).
export function focusField() {
  if (ui.slot && slotFocus) {
    setFocus("slot");
    slotFocus();
    return;
  }
  setFocus("composer");
  field?.focus({ preventScroll: true });
}

function selection() {
  const length = draftOf().text.length;
  return field ? [field.selectionStart ?? length, field.selectionEnd ?? length] : [length, length];
}

// Puts value in chat sid's field and the caret at `at`. The element is set at
// once too: a change the store does not see as one (a "!" typed into an empty
// field leaves the stored text empty) would otherwise stay on screen.
function place(value, at = value.length, sid = typingChat()) {
  draftFor(sid).text = value;
  if (!field || sid !== typingChat()) return;
  if (field.value !== value) field.value = value;
  field.setSelectionRange(at, at);
}

// -- Mode mark and placeholder (TYPE-002, TYPE-003) --

function shellRunning(sid) {
  return Object.values(composer.shells).some((shell) => shell.sessionId === sid && shell.running);
}

// isAgentBusy (send.go:23-31): the chat replies, or this window's `!` command runs.
export function working(sid = sessions.currentId) {
  return Boolean(sid) && (busy(sid) || shellRunning(sid));
}

export function currentMode() {
  return markOf({ bang: draftOf().bang, plan: composer.input === "plan", yolo: composer.yolo });
}

export function currentPlaceholder() {
  return placeholderFor({ bang: draftOf().bang, plan: composer.input === "plan", yolo: composer.yolo, busy: working(), picks: composer.picks });
}

function syncMode() {
  composer.mode = currentMode();
  composer.placeholder = currentPlaceholder();
}

// -- Text, attachments and kept skills --

const history = new PromptHistory(); // the shown chat's (TYPE-017)

export function setText(text) {
  const draft = draftFor();
  if (draft.text === text) return;
  draft.text = text;
  history.edited(text);
}

// The external editor's result for a chat that is no longer shown (2.26
// step 5): it goes into that chat's own draft, shell mode following a
// leading "!" as applyEditorText's does for the shown chat.
export function setDraftOf(sid, text) {
  const draft = draftFor(sid);
  draft.bang = text.startsWith("!");
  draft.text = draft.bang ? text.slice(1) : text;
  draft.bangEmpty = draft.bang && draft.text === "";
}

// Phase 5's file picker uses it too (Add Image, DLG-255).
export function addAttachment(attachment) {
  if (!attachment) return;
  draftFor().attachments.push(attachment);
}

export function removeAttachment(index) {
  const list = draftFor().attachments;
  if (index < 0 || index >= list.length) return;
  list.splice(index, 1);
  if (list.length === 0) composer.deleteMode = false;
}

// Phase 5's palette skill entries (CMD-057, CMD-058): the skill's text as a
// Markdown attachment (attachSkill, internal/ui/model/kept_skills.go:21-39).
export async function attachSkill({ id, name }) {
  const sid = sessions.currentId;
  try {
    const read = await api.readSkill(id);
    const fileName = read?.result?.name || name;
    draftFor(sid).attachments.push({ kind: "text", name: fileName, mime: "text/markdown", path: fileName, base64: read?.content ?? "" });
  } catch (error) {
    notify("error", errorText(error));
  }
}

// The skills the shown chat keeps (SIDE-026), or the ones turned on before
// the chat exists (keptSkills, kept_skills.go:50-55).
export function keptSkills() {
  const sid = sessions.currentId;
  if (!sid) return composer.pendingKept;
  return sessions.byId[sid]?.kept_skills ?? [];
}

// setKeptSkill (kept_skills.go:96-110): before the chat exists the list waits
// for the first send (TYPE-041); in a chat it goes to `kept-skills`, whose
// answer is the chat.
export async function keepSkill({ id, name = "", by_agent = false }, on) {
  const sid = sessions.currentId;
  if (!sid) {
    const rest = composer.pendingKept.filter((skill) => skill.id !== id);
    composer.pendingKept = on ? [...rest, { id, name, by_agent }] : rest;
    return true;
  }
  try {
    const chat = await api.setKeptSkill(sid, { id, name, on, toggleable: by_agent });
    if (chat?.id) upsertSession(chat, { fromEvent: true });
    return true;
  } catch (error) {
    notify("error", errorText(error));
    return false;
  }
}

// True when the shown chat's draft has attachments or kept skills (TYPE-039).
export function hasChips() {
  return draftOf().attachments.length > 0 || keptSkills().length > 0;
}

// -- Delete mode (TYPE-038; the terminal's attachments.Update) --

export function enterDeleteMode() {
  if (draftOf().attachments.length === 0) return false;
  composer.deleteMode = true;
  return true;
}

export function leaveDeleteMode() {
  composer.deleteMode = false;
}

export function clearAttachments() {
  draftFor().attachments.splice(0);
  composer.deleteMode = false;
}

// -- Prompt history (TYPE-017 to TYPE-019) --

let historyRead = 0;

// loadPromptHistory (history.go:23-62): the chat's prompts, or every chat's on
// the landing page.
export async function loadHistory(sid = sessions.currentId) {
  const turn = ++historyRead;
  let list = [];
  try {
    list = sid ? await api.userMessages(sid) : await api.allUserMessages();
  } catch (error) {
    console.error("Failed to load prompt history", errorText(error));
  }
  if (turn !== historyRead || sid !== sessions.currentId) return;
  history.load(historyEntries(list));
}

export function browsingHistory() {
  return history.browsing();
}

// A history entry in the field: an entry starting with "!" turns bang mode
// on (syncBangModeFromTextarea, history.go:225-240).
function showEntry(entry, at) {
  const draft = draftFor();
  const next = bangFromValue(entry, draft.bang);
  draft.bang = next.bang;
  if (next.wasEmpty !== null) draft.bangEmpty = next.wasEmpty;
  place(next.value, at === "end" ? next.value.length : 0);
  syncMode();
}

// historyUpFromStart (history.go:89-96): the next older entry, if any.
function olderEntry() {
  const entry = history.prev(draftOf().text);
  if (entry === null) return false;
  showEntry(entry, "start");
  return true;
}

let recallInFlight = false;
const failedRecalls = new Set();

// ↑ with the caret at the start (or an empty field), as handleHistoryUp
// (history.go:66-87): drafts given back first, then the engine's queue while
// the chat works or has one, then the history. Resolves true when it acted.
export async function historyUp() {
  const sid = sessions.currentId;
  const draft = draftOf(sid);
  if (!draft.bang && returnedCountOf(sid) > 0) return restoreReturned(sid);
  if (!draft.bang && sid && ws.agent?.is_ready && (working(sid) || (queue.bySession[sid]?.count ?? 0) > 0 || failedRecalls.has(sid))) {
    return recallQueuedPrompt(sid);
  }
  return olderEntry();
}

// ↓ with the caret at the end (history.go:184-203).
export function historyDown() {
  const entry = history.next();
  if (entry === null) return false;
  showEntry(entry, "end");
  return true;
}

// Esc while browsing (history.go:206-219): back to the draft.
export function historyEscape() {
  const entry = history.escape();
  if (entry === null) return false;
  showEntry(entry, "end");
  return true;
}

// recallQueuedPrompt and applyRecalledPrompt (history.go:106-153).
async function recallQueuedPrompt(sid) {
  if (recallInFlight) return true;
  recallInFlight = true;
  const before = draftOf(sid).text;
  const index = history.index;
  let prompt = null;
  let failed = null;
  try {
    prompt = await recallQueued(sid);
  } catch (error) {
    failed = error;
  } finally {
    recallInFlight = false;
  }
  if (failed) {
    failedRecalls.add(sid);
    notify("error", errorText(failed));
    return true;
  }
  failedRecalls.delete(sid);
  if (prompt) {
    finishPending(prompt.submissionId, false);
    returnDraft(sid, { text: prompt.text, attachments: prompt.attachments });
    recalled(sid);
  }
  if (sid !== sessions.currentId) return true;
  if (prompt) {
    restoreReturned(sid);
    if (draftOf(sid).bang) notify("info", RECOVERED_IN_BANG);
  } else if (draftOf(sid).text === before && history.index === index && selection()[0] === 0) {
    olderEntry();
  }
  return true;
}

// Prompts given back to chat sid's field: a failed send (TYPE-048), a held
// send whose chat did not open (TYPE-049), a recalled queue entry (TYPE-019).
function returnedCountOf(sid) {
  return messages.returned[sid ?? ""]?.length ?? 0;
}

// restoreRecalledDrafts (history.go:155-181): the drafts go after the field's
// text, their attachments join the chips, the caret goes to the end. Not in
// bang mode: they wait there for ↑ outside it.
export function restoreReturned(sid = sessions.currentId) {
  const draft = draftFor(sid);
  if (draft.bang) return false;
  const drafts = takeReturned(sid);
  if (drafts.length === 0) return false;
  const text = joinDrafts(draft.text, drafts);
  for (const returned of drafts) draft.attachments.push(...returned.attachments);
  place(text, text.length, sid);
  if (sid === sessions.currentId) {
    history.edited(text);
    closePopup();
    syncMode();
  }
  return true;
}

// -- Typing, the `@` popup and pasting --

// The field's input event (typeInEditor, keypress.go:381-421, and
// checkBangModeAfterPaste, paste.go:36-53): bang mode, the history draft and
// the `@` popup follow the text. before is the text before the event, data
// the characters it typed.
export function typed({ before, value, caret, data = "", paste = false }) {
  const sid = typingChat();
  const draft = draftFor(sid);
  let next;
  if (paste) next = bangAfterPaste({ value, caret, bang: draft.bang }) ?? { bang: draft.bang, wasEmpty: draft.bangEmpty, value, caret };
  else next = bangAfterTyping({ before, value, caret, bang: draft.bang, wasEmpty: draft.bangEmpty });
  draft.bang = next.bang;
  draft.bangEmpty = next.wasEmpty;
  if (next.value !== value) place(next.value, next.caret, sid);
  else draft.text = value;
  if (draft.text !== before) history.edited(draft.text);
  syncMode();
  if (data === "@" && !paste) {
    // triggers (mention.go:27-34); the "@" that opened it is not followed.
    if (!composer.popup && opensAt(next.value, next.caret - 1)) openMention(next.caret - 1);
  } else if (data === "/" && !paste && !draft.bang && !composer.popup && slashOpens(next.value, next.caret)) {
    openSlash();
  } else if (composer.popup) {
    followMention(next.value, next.caret);
  } else {
    resumePopup();
  }
}

// Backspace on an empty bang prompt leaves bang mode (keypress.go:382-388).
export function backspaceLeavesBang() {
  const draft = draftOf();
  if (!draft.bang || !draft.bangEmpty || draft.text !== "") return false;
  const writable = draftFor();
  writable.bang = false;
  writable.bangEmpty = false;
  syncMode();
  return true;
}

// The caret moved without typing (arrows, a click): the popup follows it.
export function caretMoved() {
  if (!composer.popup) {
    resumePopup();
    return;
  }
  const [at] = selection();
  followMention(draftOf().text, at);
}

let fileList = []; // every row of the open popup, as files_list gave them
let resumable = null; // {kind, start} of a popup closed because the field lost focus
let listTurn = 0;

// The project's folder (the engine's working directory for this window).
export function projectDir() {
  return conn.workspace?.path || conn.project.path || conn.project.cwd || "";
}
const workDir = projectDir;

// options.tui.completions (Completions.Limits, internal/config/options.go:87-89).
function completionLimits() {
  const limits = ws.config?.options?.tui?.completions ?? {};
  return { depth: Number(limits.max_depth) || 0, items: Number(limits.max_items) || 0 };
}

// begin (mention.go:36-43): the popup opens for the "@" at `start` and reads
// the project's files; MCP resources are not listed (no engine route lists them).
export async function openMention(start) {
  composer.popup = { kind: "@", start, query: "", rows: [], focused: 0, loading: true };
  const turn = ++listTurn;
  const { depth, items } = completionLimits();
  let entries = [];
  try {
    entries = await files_list(workDir(), depth, items);
  } catch (error) {
    console.error("file list failed", errorText(error));
  }
  if (turn !== listTurn || composer.popup?.start !== start) return;
  fileList = fileRows(entries);
  composer.popup.loading = false;
  rank();
}

function rank() {
  const popup = composer.popup;
  if (!popup) return;
  popup.rows = popup.kind === "/" ? rankSlash(slash?.slashRows() ?? [], popup.query) : rankRows(fileList, popup.query);
  popup.focused = 0;
}

// The `/` menu (owner, 2026-10-20): every palette command, filtered by what
// follows the "/". Its module loads on first use (lib/palette/slash.js).
let slash = null;
let slashStop = null;

export async function openSlash() {
  composer.popup = { kind: "/", start: 0, query: "", rows: [], focused: 0, loading: true };
  const turn = ++listTurn;
  slash ??= await import("../palette/slash.js");
  if (turn !== listTurn || composer.popup?.kind !== "/") return;
  slashStop?.();
  slashStop = slash.watchSlash();
  composer.popup.loading = false;
  rank();
  // The custom commands and MCP prompts join when they arrive; the focused
  // row stays the same command.
  await slash.readSlash();
  const popup = composer.popup;
  if (turn !== listTurn || popup?.kind !== "/") return;
  const focused = popup.rows[popup.focused]?.row.entry.id;
  rank();
  const at = popup.rows.findIndex(({ row }) => row.entry.id === focused);
  if (at >= 0) popup.focused = at;
}

// follow (mention.go:49-63).
function followMention(value, caret) {
  const popup = composer.popup;
  if (!popup) return;
  const query = popup.kind === "/" ? slashQuery(value, caret) : mentionQuery(value, caret, popup.start);
  if (query === null) {
    closePopup();
    return;
  }
  if (query === popup.query) return;
  popup.query = query;
  if (!popup.loading) rank();
}

export function closePopup() {
  resumable = null;
  if (!composer.popup) return;
  listTurn += 1;
  composer.popup = null;
  fileList = [];
  slashStop?.();
  slashStop = null;
}

// The field lost focus (another window, a click elsewhere): the popup closes
// but comes back on focus or the next keystroke while the caret is still in
// its word (owner, 2026-10-21). Escape and picks close it for good.
export function blurPopup() {
  const popup = composer.popup;
  closePopup();
  if (popup) resumable = { kind: popup.kind, start: popup.start };
}

export function resumePopup() {
  if (!resumable || composer.popup) return;
  // A click's focus comes before its caret: until the caret is back in the
  // word the popup stays remembered (caretMoved tries again).
  const { kind, start } = resumable;
  const text = draftOf().text;
  const [at] = selection();
  if (draftOf().bang && kind === "/") return;
  if ((kind === "/" ? slashQuery(text, at) : mentionQuery(text, at, start)) === null) return;
  resumable = null;
  if (kind === "/") openSlash();
  else openMention(start);
  followMention(text, at);
}

// ↑ and ↓ in the popup (moveFocus: the best row is drawn lowest).
export function movePopup(direction) {
  const popup = composer.popup;
  if (!popup) return;
  popup.focused = moveFocus(popup.focused, popup.rows.length, direction);
}

// The pointer rests on a row (TYPE-061).
export function focusRow(index) {
  const popup = composer.popup;
  if (popup && index >= 0 && index < popup.rows.length && popup.focused !== index) popup.focused = index;
}

let reads = []; // absolute paths picked before the chat exists (sessionFileReads)

// Enter, Tab, Ctrl+Y or a click on a row (handleCompletionsKey, keypress.go:
// 209-230; insertFileCompletion, editor.go:294-368): the row's text and a
// space in place of "@query", then the file as an attachment.
export async function pickRow(index = composer.popup?.focused ?? 0) {
  const popup = composer.popup;
  const entry = popup?.rows[index];
  if (!entry) return false;
  if (popup.kind === "/") return runSlash(entry.row.entry);
  const sid = sessions.currentId;
  const value = draftOf(sid).text;
  const placed = insertPick(value, mentionSpan(value, popup.start), entry.row.text);
  closePopup();
  await place(placed.value, placed.caret, sid);
  history.edited(placed.value);
  if (entry.row.kind === "file" && !entry.row.dir) attachPicked(entry.row.path, sid);
  return true;
}

// A `/` pick: the "/word" leaves the field, then the command runs as the
// palette runs it (lib/palette/run.js).
async function runSlash(command) {
  const sid = sessions.currentId;
  const value = draftOf(sid).text;
  const span = mentionSpan(value, 0);
  const rest = value.slice(span.end).replace(/^[ \t]/, "");
  closePopup();
  await place(rest, 0, sid);
  history.edited(rest);
  command.run();
  return true;
}

// Ctrl+N and Ctrl+P: the next or previous row's text in place of the
// mention, the popup staying open; nothing is attached until a pick. The `/`
// menu only moves its focus.
export function previewRow(direction) {
  const popup = composer.popup;
  if (!popup || popup.rows.length === 0) return;
  if (popup.kind === "/") {
    popup.focused = moveFocus(popup.focused, popup.rows.length, direction === "next" ? "down" : "up");
    return;
  }
  popup.focused = moveFocus(popup.focused, popup.rows.length, direction === "next" ? "down" : "up");
  const value = draftOf().text;
  const shown = previewPick(value, mentionSpan(value, popup.start), popup.rows[popup.focused].row.text);
  place(shown.value, shown.span.end);
  history.edited(shown.value);
}

async function attachPicked(path, sid) {
  const dir = workDir();
  const result = await pickFile(path, {
    sessionId: sid,
    absolute: absolutePath(dir, path),
    supportsImages: supportsImages(ws.agent),
    reads,
    stat: (p) => file_info(p, dir),
    read: (p, max) => file_read(p, dir, max),
    lastRead: (chat, absolute) => api.lastRead(chat, absolute),
  });
  if (result.warn) notify("warn", result.warn);
  if (result.attachment) draftFor(sid).attachments.push(result.attachment);
}

// A text paste (handlePasteMsg, paste.go:55-120). Returns false when the
// field should take the text itself (then `typed` runs with paste: true);
// otherwise the store handles it: a long paste becomes paste_N.txt, picture
// paths become attachments, and text whose paths are not all pictures is put
// in at the selection.
export function pasteText(raw) {
  const text = normalize(raw);
  const route = pasteRoute(text);
  if (route === "text") return false;
  const sid = typingChat();
  if (route === "attach") {
    const result = textAttachment(text, draftOf(sid).attachments);
    if (result.warn) notify("warn", result.warn);
    else draftFor(sid).attachments.push(result.attachment);
    return true;
  }
  const [start, end] = selection();
  pastePaths(text, sid, start, end);
  return true;
}

async function pastePaths(text, sid, start, end) {
  const dir = workDir();
  const stat = (p) => file_info(p, dir);
  const paths = await pastedPictures(text, stat);
  if (!paths) {
    if (sid !== typingChat()) return;
    const value = draftOf(sid).text;
    const next = value.slice(0, start) + text + value.slice(end);
    await place(next, start + text.length, sid);
    typed({ before: value, value: next, caret: start + text.length, paste: true });
    return;
  }
  if (!supportsImages(ws.agent)) {
    notify("warn", NO_PICTURES);
    return;
  }
  for (const path of paths) {
    const result = await attachPath(path, { stat, read: (p, max) => file_read(p, dir, max) });
    if (result.error) notify("error", result.error);
    else if (result.warn) notify("warn", result.warn);
    else draftFor(sid).attachments.push(result.attachment);
  }
}

// Ctrl+Shift+V (pasteTextFromClipboard, paste.go:171-180).
export async function pasteFromClipboard() {
  let text = null;
  try {
    text = await clipboard_read();
  } catch {
    text = null;
  }
  if (!text) {
    notify("error", EMPTY_CLIPBOARD);
    return false;
  }
  if (pasteText(text)) return true;
  const [start, end] = selection();
  const value = draftOf().text;
  const next = value.slice(0, start) + normalize(text) + value.slice(end);
  const at = start + normalize(text).length;
  await place(next, at);
  typed({ before: value, value: next, caret: at, paste: true });
  return true;
}

// A picture on the clipboard (pasteImageFromClipboard, paste.go:186-259):
// file is the clipboard's File; a type that is not a picture asks for one.
export async function pastePicture(file) {
  if (!supportsImages(ws.agent)) {
    notify("warn", NO_PICTURES);
    return;
  }
  if (!file.type.startsWith("image/")) {
    notify("info", NOT_A_PICTURE);
    return;
  }
  const sid = sessions.currentId;
  let data;
  try {
    data = new Uint8Array(await file.arrayBuffer());
  } catch (error) {
    notify("error", unreadable(errorText(error)));
    return;
  }
  const result = pictureAttachment(data, draftOf(sid).attachments, true);
  if (result.error) notify("error", result.error);
  else if (result.warn) notify("warn", result.warn);
  else draftFor(sid).attachments.push(result.attachment);
}

// Files dropped on the window (TYPE-012's GUI version): the paste of their
// escaped paths, as a terminal pastes a drop.
export function dropPaths(paths) {
  if (!Array.isArray(paths) || paths.length === 0) return;
  focusField();
  const text = dropText(paths);
  if (pasteText(text)) return;
  const [start, end] = selection();
  const value = draftOf().text;
  const next = value.slice(0, start) + text + value.slice(end);
  place(next, start + text.length);
  typed({ before: value, value: next, caret: start + text.length, paste: true });
}

// Text put in at the selection, as typed (Ctrl+J's new line, keypress.go:270-274).
export function insertText(text) {
  const [start, end] = selection();
  const value = draftOf().text;
  const next = value.slice(0, start) + text + value.slice(end);
  place(next, start + text.length);
  typed({ before: value, value: next, caret: start + text.length, data: text });
}

// Home, Ctrl+E and End (TYPE-009): the caret to the start or the
// end of its line (the textarea's CursorStart and CursorEnd).
export function caretToLineEdge(edge) {
  if (!field) return;
  const value = field.value;
  const at = field.selectionStart ?? 0;
  const next = edge === "start" ? value.lastIndexOf("\n", at - 1) + 1 : value.indexOf("\n", at) === -1 ? value.length : value.indexOf("\n", at);
  field.setSelectionRange(next, next);
  caretMoved();
}

// The caret to the very start or end of the text (the first ↑ and the last ↓).
export function caretToEdge(edge) {
  if (!field) return;
  const at = edge === "start" ? 0 : field.value.length;
  field.setSelectionRange(at, at);
  caretMoved();
}

// Tab (TYPE-022; keypress.go:262-267): focus goes to the timeline, except on
// the landing page and with options.tui.composer_focus_only. P3.5 selects
// its last item when the timeline takes focus.
export function focusTimeline() {
  if (!sessions.currentId || ws.config?.options?.tui?.composer_focus_only === true) return false;
  closePopup();
  field?.blur();
  setFocus("timeline");
  return true;
}

// Ctrl+Shift+C and Ctrl+Shift+X (keypress.go:275-292), and Ctrl+C with a
// selection: false when nothing is selected.
export async function copySelection({ cut = false } = {}) {
  const [start, end] = selection();
  if (start === end) return false;
  const value = draftOf().text;
  const text = value.slice(start, end);
  try {
    await clipboard_write(text);
  } catch (error) {
    notify("error", errorText(error));
    return true;
  }
  notify("info", cut ? "Selection cut to clipboard" : "Selection copied to clipboard");
  if (cut) {
    const next = value.slice(0, start) + value.slice(end);
    await place(next, start);
    typed({ before: value, value: next, caret: start });
  } else if (field) {
    field.setSelectionRange(end, end);
  }
  return true;
}

// -- Sending (TYPE-004 to TYPE-008, TYPE-041 to TYPE-049) --

let interrupting = new Set(); // sids with an interrupt on its way (queue.interruptInFlight)
let landingSend = false; // a send from the landing page is making its chat

function reportSendFailure(submissionId, error) {
  const code = error?.code ?? "";
  if (code === "aborted") {
    if (submissionId) finishPending(submissionId, false);
    return;
  }
  if (!refusedWhileFailed(error)) notify("error", errorText(error));
  if (!submissionId) return;
  if (code === "transport" || code === "timeout") {
    // The engine may have taken it (update_agent.go:134-141): the item stays.
    markUncertain(submissionId);
    resolvePending(submissionId, false);
  } else if (typeof error?.status === "number") {
    // The engine refused it: back to the field unless it was saved.
    resolvePending(submissionId, true);
  } else {
    // Not sent (engine_unavailable and the app's own refusals).
    finishPending(submissionId, true);
  }
}

const wire = (attachments) =>
  attachments.map((a) => ({ file_path: a.path || a.name, file_name: a.name, mime_type: a.mime, content: a.base64 }));

// sendMessageInternal (send.go:63-148).
// Phase 5's palette (custom commands, MCP prompts, Initialize Project): the
// text goes out as a prompt, never as a shell command, and the draft stays
// as it is (the terminal's sendMessage(content), dialog_actions.go:298).
export async function sendPromptText(content) {
  if (!ws.agent?.is_ready) {
    notify("error", NOT_READY);
    return false;
  }
  const sent = await sendMessage(content);
  loadHistory();
  return sent;
}

async function sendMessage(content, attachments = [], { hidden = false } = {}) {
  if (!ws.agent?.is_ready) {
    notify("error", NOT_READY);
    return false;
  }
  if (deferSend({ run: () => sendMessage(content, attachments, { hidden }), text: content, attachments, hidden })) return true;
  // Any new prompt supersedes a pending plan (send.go:73-74).
  composer.planHandoff = null;
  const prior = sessions.currentId;
  const kept = prior ? [] : composer.pendingKept;
  let sid;
  if (!prior) landingSend = true;
  try {
    sid = await ensureSession();
  } catch (error) {
    landingSend = false;
    if (!refusedWhileFailed(error)) notify("error", errorText(error));
    if (!hidden) returnDraft(prior, { text: content, attachments });
    return false;
  }
  if (!prior) composer.pendingKept = [];
  const queued = prior !== "" && prior === sid && (busy(sid) || interrupting.has(sid));
  const submissionId = hidden ? "" : addPending({ sessionId: sid, text: content, attachments, held: queued });
  if (!hidden) requestEnd();
  composer.sending = true;
  try {
    for (const skill of kept) {
      try {
        await api.setKeptSkill(sid, { id: skill.id, name: skill.name, on: true, toggleable: skill.by_agent });
      } catch (error) {
        console.warn("Failed to keep skill on for new chat", skill.name, errorText(error));
      }
    }
    await api.send({
      session_id: sid,
      prompt: content,
      attachments: attachments.length ? wire(attachments) : undefined,
      submission_id: submissionId || undefined,
      hidden_user_message: hidden || undefined,
    });
  } catch (error) {
    reportSendFailure(submissionId, error);
  } finally {
    composer.sending = false;
    refreshQueue(sid);
  }
  return true;
}

// Enter (submitEditor, keypress.go:320-364). Resolves true when something was
// sent or run.
export async function send() {
  if (composer.switching) {
    notify("info", SWITCHING);
    return false;
  }
  const sid = typingChat();
  const draft = draftFor(sid);
  const value = draft.text;
  if (value.endsWith("\\")) {
    // TYPE-006: the backslash becomes a new line.
    await place(`${value.slice(0, -1)}\n`);
    history.edited(draftOf(sid).text);
    return false;
  }
  const text = value.trim();
  if (isQuitWord(text)) {
    // TYPE-007: the quit sheet is phase 5's; until it exists the text stays.
    if (openDialog("quit")) place("", 0, sid);
    return false;
  }
  closePopup();
  if (draft.bang && text !== "") {
    draft.text = "";
    draft.bang = false;
    draft.bangEmpty = false;
    composer.picks = pickPlaceholders();
    history.reset();
    syncMode();
    runShell(text);
    requestEnd();
    return true;
  }
  const attachments = [...draft.attachments];
  if (text === "" && attachments.length === 0) {
    draft.text = "";
    // TYPE-052: a dismissed ready plan comes back.
    if (composer.input === "plan" && sid && composer.planHandoff?.sessionId === sid) composer.planHandoff = { sessionId: sid, open: true };
    return false;
  }
  if (!ws.agent?.is_ready) {
    // TYPE-043: the text stays (the terminal empties the field first).
    notify("error", NOT_READY);
    return false;
  }
  draft.text = "";
  draft.attachments = [];
  composer.deleteMode = false;
  composer.picks = pickPlaceholders();
  history.reset();
  syncMode();
  const sent = await sendMessage(text, attachments);
  loadHistory();
  return sent;
}

// -- `!` commands (TYPE-020, TYPE-021) --

// The command's width in the timeline's mono characters, at most 120
// (send.go:184: the chat's width less 2, at most 120).
function termWidth() {
  const column = field?.closest("main") ?? null;
  if (!column) return 120;
  const width = column.getBoundingClientRect().width;
  const size = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
  const cell = size * (17 / 16) * 0.5; // Inconsolata at the code size: half an em per character
  return Math.max(20, Math.min(120, Math.floor(width / cell) - 2));
}

// runShellCommandInternal (send.go:159-241): the chat is made first when
// there is none; the command runs in the engine inside its own call, whose
// request id is also the live item's key.
export async function runShell(command) {
  if (deferSend({ run: () => runShell(command), text: command, attachments: [], shell: true })) return true;
  let sid = sessions.currentId;
  let first = false;
  if (!sid) {
    landingSend = true;
    try {
      sid = await ensureSession();
      first = true;
    } catch (error) {
      landingSend = false;
      notify("error", errorText(error));
      return false;
    }
  }
  const requestId = uuid();
  composer.shells[requestId] = { requestId, sessionId: sid, command, output: "", lastSeq: -1, running: true, exitCode: null, startedAt: Date.now() };
  syncMode();
  try {
    const answer = await api.runShell(sid, { command, term_width: termWidth(), is_first_message: first }, requestId);
    const shell = composer.shells[requestId];
    if (shell?.running) {
      shell.output = answer?.output ?? shell.output;
      shell.exitCode = answer?.exit_code ?? 0;
      shell.running = false;
    }
  } catch (error) {
    const shell = composer.shells[requestId];
    if (error?.code === "aborted") {
      if (shell?.running) {
        shell.running = false;
        shell.exitCode = 130;
      }
    } else {
      delete composer.shells[requestId];
      notify("error", `shell: ${errorText(error)}`);
    }
  } finally {
    held.delete(requestId);
    syncMode();
  }
  loadHistory();
  return true;
}

const held = new Map(); // request id → {seq: chunk}: output that came before an earlier chunk

// shell_output (engine design 5.9 a): chunks join in seq order, from 0.
onEvent("shell_output", (_kind, out) => {
  const shell = composer.shells[out?.request_id];
  if (!shell || !shell.running || typeof out.seq !== "number" || out.seq <= shell.lastSeq) return;
  const waiting = held.get(shell.requestId) ?? new Map();
  waiting.set(out.seq, out.chunk ?? "");
  let output = shell.output;
  let seq = shell.lastSeq;
  while (waiting.has(seq + 1)) {
    seq += 1;
    output += waiting.get(seq);
    waiting.delete(seq);
  }
  shell.output = output;
  shell.lastSeq = seq;
  if (waiting.size > 0) held.set(shell.requestId, waiting);
  else held.delete(shell.requestId);
});

// A command's saved message (a user message with a shell_command part)
// takes the place of its live item, which the timeline then stops drawing.
// A stopped command is saved nowhere and stays (TYPE-021).
onEvent("message", (kind, m) => {
  if (kind === "deleted" || m?.role !== "user") return;
  for (const part of m.parts ?? []) {
    if (part?.type !== "shell_command") continue;
    const command = part.data?.command ?? "";
    const live = Object.values(composer.shells)
      .filter((shell) => shell.sessionId === m.session_id && shell.command === command && shell.exitCode !== 130)
      .sort((a, b) => a.startedAt - b.startedAt)[0];
    if (live) delete composer.shells[live.requestId];
  }
});

// -- Stopping (TYPE-021, TYPE-046, TYPE-047) --

// interruptAgent (send.go:245-302): this window's running `!` commands of the
// chat stop through their own call (api.abort), then the run is interrupted;
// one interrupt per chat at a time.
async function interruptShown() {
  const sid = sessions.currentId;
  if (!sid) return false;
  for (const shell of Object.values(composer.shells)) {
    if (shell.sessionId !== sid || !shell.running) continue;
    api
      .abort(shell.requestId)
      .then((stopped) => {
        const live = composer.shells[shell.requestId];
        if (stopped && live?.running) {
          live.running = false;
          live.exitCode = 130;
          syncMode();
        }
      })
      .catch((error) => console.error("stopping the command failed", errorText(error)));
  }
  if (interrupting.has(sid)) return true;
  interrupting = new Set([...interrupting, sid]);
  try {
    await api.interrupt(sid);
  } catch (error) {
    console.error("interrupt failed", errorText(error));
  } finally {
    const rest = new Set(interrupting);
    rest.delete(sid);
    interrupting = rest;
    refreshQueue(sid);
    syncMode();
  }
  return true;
}

// Esc while the chat works (TYPE-046, TYPE-021).
export async function stop() {
  if (!working()) return false;
  return interruptShown();
}

// Ctrl+Enter (TYPE-047): also taken while an interrupt is on its way.
export async function interruptAndSend() {
  const sid = sessions.currentId;
  if (!working() && !interrupting.has(sid)) return false;
  return interruptShown();
}

export function interruptPending(sid = sessions.currentId) {
  return interrupting.has(sid);
}

// -- Input mode and YOLO (TYPE-023 to TYPE-025) --

let cycleYolo = false; // YOLO the Shift+Tab cycle turned on, which only it turns off
let yoloTurn = 0;
const yoloRead = { running: false, again: false };

// toggleYoloMode (workspace_cache.go:352-367), written through at once and
// put back when the engine refuses.
async function setYolo(on) {
  yoloTurn += 1;
  const before = composer.yolo;
  composer.yolo = on;
  syncMode();
  try {
    await api.setPermissionsSkip(on);
    return true;
  } catch (error) {
    yoloTurn += 1;
    composer.yolo = before;
    syncMode();
    notify("error", errorText(error));
    return false;
  }
}

// GET permissions/skip; reads never overlap and a change made here meanwhile wins.
export async function readYolo() {
  if (yoloRead.running) {
    yoloRead.again = true;
    return;
  }
  yoloRead.running = true;
  try {
    do {
      yoloRead.again = false;
      const turn = yoloTurn;
      try {
        const answer = await api.permissionsSkip();
        if (turn === yoloTurn && typeof answer?.skip === "boolean" && answer.skip !== composer.yolo) {
          composer.yolo = answer.skip;
          syncMode();
        }
      } catch (error) {
        console.error("YOLO read failed", errorText(error));
      }
    } while (yoloRead.again);
  } finally {
    yoloRead.running = false;
  }
}

// setInputMode and applyModeSwitch (input_mode.go:62-114): the mode changes
// only once the engine switched its agent; a failure keeps the old one.
async function switchInput(target, { continueSid = "" } = {}) {
  const yolo = target === "code" && composer.yolo;
  composer.switching = true;
  try {
    await api.setMainAgent(AGENT_FOR[target]);
    await api.updateAgent();
  } catch (error) {
    notify("error", errorText(error));
    return false;
  } finally {
    composer.switching = false;
  }
  composer.input = target;
  syncMode();
  loadAgent().catch(() => {});
  if (continueSid && sessions.currentId === continueSid) sendMessage(IMPLEMENT, [], { hidden: true });
  const banner = bannerFor(target, yolo);
  notify(banner.kind, banner.text);
  return true;
}

// G-26 (desktop-design-5-6 7.1, TYPE-002's order): on every agent-info read
// the mode follows the engine's main agent, so a switch another app made shows
// here too: "plan" gives plan, "coder" gives YOLO while the skip switch is on,
// else normal. "" (no coordinator yet) changes nothing, nor does a read while
// this window switches or the field is in bang mode.
function followAgent(agent) {
  if (agent !== "plan" && agent !== "coder") return;
  if (composer.switching || draftOf().bang) return;
  const input = agent === "plan" ? "plan" : "code";
  if (composer.input === input) return;
  composer.input = input;
  syncMode();
}

// Shift+Tab (toggleInputMode, input_mode.go:16-36).
export async function cycleMode() {
  const steps = cycleSteps({ plan: composer.input === "plan", skip: composer.yolo, cycleYolo, busy: working(), switching: composer.switching });
  if (steps.warn) {
    notify("warn", steps.warn);
    return false;
  }
  if (steps.yoloOff) {
    cycleYolo = false;
    await setYolo(false);
    notify("info", steps.info);
    return true;
  }
  if (steps.yoloOn) {
    cycleYolo = false;
    if (!(await setYolo(true))) return false;
  }
  if (steps.claim) cycleYolo = true;
  return switchInput(steps.switchTo);
}

// switchPlanToYolo (input_mode.go:43-54).
async function planToYolo() {
  if (working() || composer.switching) {
    notify("warn", BUSY_SWITCH);
    return false;
  }
  if (!composer.yolo && !(await setYolo(true))) return false;
  cycleYolo = false;
  return switchInput("code");
}

// Ctrl+Y (keypress.go:634-643): in plan mode it switches to YOLO coding.
export async function toggleYolo() {
  if (composer.input === "plan") return planToYolo();
  const on = !composer.yolo;
  cycleYolo = false;
  notify(on ? "yolo" : "info", on ? YOLO_BANNER : YOLO_OFF);
  return setYolo(on);
}

// -- The plan handoff (TYPE-052 to TYPE-058; input_mode.go:136-219) --

// handlePlanHandoff (input_mode.go:138-158): a plan run of the shown chat
// that ended with the ready marker.
onEvent("run_complete", (_kind, done) => {
  if (composer.input !== "plan" || done?.error || done?.cancelled) return;
  const sid = sessions.currentId;
  if (!sid || done?.session_id !== sid || !planReadyPresent(done.text ?? "")) return;
  composer.planHandoff = { sessionId: sid, open: ui.slot?.kind !== "secure" };
});

// Start coding (yolo false) or Code with YOLO (yolo true): permission
// skipping as asked, code mode, and the hidden "Implement the plan.".
export async function confirmPlan(yolo) {
  const sid = composer.planHandoff?.sessionId;
  if (!sid || sid !== sessions.currentId) return false;
  if (composer.yolo !== yolo) {
    cycleYolo = false;
    if (!(await setYolo(yolo))) return false;
  }
  composer.planHandoff = null;
  return switchInput("code", { continueSid: sid });
}

// Revise plan: the feedback goes to the planning agent as a prompt.
export async function revisePlan(feedback) {
  const text = String(feedback ?? "").trim();
  if (!text) return false;
  return sendMessage(text, []);
}

// Esc on the buttons: the form closes, the plan stays ready (Enter on an
// empty field brings it back).
export function dismissPlan() {
  if (composer.planHandoff) composer.planHandoff = { ...composer.planHandoff, open: false };
  focusField();
}

// resetPlanModeState (input_mode.go:163-180): another chat drops the ready
// plan and leaves plan mode, unless its run still goes (the engine refuses
// agent switches then).
function resetPlan() {
  composer.planHandoff = null;
  if (composer.input === "plan" && !working()) switchInput("code");
}

// -- The shown chat --

let shownChat = null;

// The window shows another chat (or the landing page).
export function chatShown(sid) {
  const prev = shownChat;
  shownChat = sid;
  if (prev === sid) return;
  const own = prev === "" && landingSend;
  landingSend = false;
  if (own) {
    // The landing page's draft (what was typed while the chat was made) moves with it.
    const landing = composer.drafts[""];
    if (landing && !composer.drafts[sid]) {
      composer.drafts[sid] = landing;
      delete composer.drafts[""];
    }
  } else if (prev !== null) {
    resetPlan();
    reads = [];
  }
  composer.deleteMode = false;
  closePopup();
  history.reset();
  history.load([]);
  loadHistory(sid);
  restoreReturned(sid);
  syncMode();
}

// The probe's view of the slot's buttons; the form keeps it current.
export function setSlotButtons(buttons) {
  composer.slotButtons = buttons ?? [];
}

// After a reconnect: YOLO and the history are read again.
onResync(() => {
  readYolo();
  loadHistory();
});

// Called once by lib/plugins/composer.js: the mark and placeholder follow
// other stores (busy), the shown chat's changes and given-back prompts are
// applied, and the 2 s backstop reads the queue while the chat works or has
// one and YOLO while connected (the terminal's TTL backstop,
// internal/ui/model/workspace_cache.go:316-346).
let started = false;
export function startComposer({ connected = () => conn.state === "connected" } = {}) {
  if (started) return;
  started = true;
  $effect.root(() => {
    $effect(() => {
      composer.mode = currentMode();
      composer.placeholder = currentPlaceholder();
    });
    $effect(() => {
      const sid = sessions.currentId;
      untrack(() => chatShown(sid));
    });
    $effect(() => {
      const info = ws.agent;
      untrack(() => followAgent(info?.agent));
    });
    $effect(() => {
      const sid = sessions.currentId;
      if (returnedCountOf(sid) > 0) untrack(() => restoreReturned(sid));
    });
    $effect(() => {
      if (!connected()) return;
      untrack(() => {
        readYolo();
        loadHistory();
      });
    });
  });
  setInterval(() => {
    if (!connected()) return;
    if (globalThis.document?.visibilityState !== "hidden") readYolo();
    const sid = sessions.currentId;
    if (sid && (working(sid) || (queue.bySession[sid]?.count ?? 0) > 0)) refreshQueue(sid);
  }, POLL_MS);
}
