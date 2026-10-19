// Prompt history and bang mode as pure rules (TYPE-017 to TYPE-020; a port
// of internal/ui/model/history.go:16-96,183-282 and keypress.go:381-449).
// The composer store holds one PromptHistory and the field's value; these
// functions say what the field shows next.

// agent.TaskNotificationTag (internal/agent/tasks.go:41).
const TASK_RESULT = "<crush-task-result>";

const partsOf = (m, type) => (m?.parts ?? []).filter((p) => p?.type === type);

// loadPromptHistory (history.go:22-61): the user messages as the engine lists
// them (newest first, at most 200), without the prompts Crush wrote itself
// (hidden continuations and task results). A message gives its text when it
// has one, then "!command" for each shell command it ran.
export function historyEntries(list) {
  const texts = [];
  for (const m of Array.isArray(list) ? list : []) {
    const first = partsOf(m, "text")[0]?.data ?? {};
    const text = first.text ?? "";
    if (first.hidden === true || text.trim().startsWith(TASK_RESULT)) continue;
    if (text !== "") texts.push(text);
    for (const part of partsOf(m, "shell_command")) texts.push(`!${part.data?.command ?? ""}`);
  }
  return texts;
}

// Where the caret is, in the terminal's terms (composer.go:51-66): the start is
// position 0 of the first line, the end the last character of the last line.
export function atStart(value, caret) {
  return caret === 0;
}
export function atEnd(value, caret) {
  return caret >= value.length;
}
export function onFirstLine(value, caret) {
  return !value.slice(0, caret).includes("\n");
}
export function onLastLine(value, caret) {
  return !value.slice(caret).includes("\n");
}

// The history's place: index -1 is the draft being written (promptHistory in
// composer.go:28-32).
export class PromptHistory {
  messages = [];
  index = -1;
  draft = "";

  load(messages) {
    this.messages = Array.isArray(messages) ? messages : [];
  }

  // historyReset: after a send.
  reset() {
    this.index = -1;
    this.draft = "";
  }

  // updateHistoryDraft: any edit makes the text the draft again.
  edited(value) {
    this.draft = value;
    this.index = -1;
  }

  // historyPrev: the next older entry, or null at the oldest. The draft is
  // kept when leaving it.
  prev(value) {
    if (this.messages.length === 0) return null;
    if (this.index === -1) this.draft = value;
    const next = this.index + 1;
    if (next >= this.messages.length) return null;
    this.index = next;
    return this.messages[next];
  }

  // historyNext: the next newer entry, the draft after the newest, or null
  // when already on the draft.
  next() {
    if (this.index < 0) return null;
    const next = this.index - 1;
    if (next < 0) {
      this.index = -1;
      return this.draft;
    }
    this.index = next;
    return this.messages[next];
  }

  // handleHistoryEscape: back to the draft while browsing, else null.
  escape() {
    if (this.index < 0) return null;
    this.index = -1;
    return this.draft;
  }

  browsing() {
    return this.index >= 0;
  }
}

// syncBangModeFromTextarea (history.go:241-259): a text starting with "!"
// turns bang mode on and loses the "!"; any other text turns it off.
// Returns {bang, value, wasEmpty} where wasEmpty is null when it is unchanged.
export function bangFromValue(value, bang) {
  if (value.startsWith("!")) return { bang: true, value: value.slice(1), wasEmpty: bang ? null : false };
  return { bang: false, value, wasEmpty: bang ? false : null };
}

const trimLeft = (s) => s.replace(/^\s+/u, "");

// syncBangModeAfterTyping (keypress.go:428-449): "!" typed at the start,
// spaces before it allowed, turns bang mode on and loses the spaces and the
// "!"; caret moves with the text. In bang mode the field turning empty marks
// it empty (a Backspace then leaves bang mode). before is the text before the
// key. Returns {bang, wasEmpty, value, caret}.
export function bangAfterTyping({ before, value, caret, bang, wasEmpty }) {
  const now = trimLeft(value);
  if (!bang && now.startsWith("!") && !trimLeft(before).startsWith("!")) {
    const stripped = now.slice(1);
    return { bang: true, wasEmpty: before.trim() === "", value: stripped, caret: Math.max(0, caret - (value.length - stripped.length)) };
  }
  if (bang && value === "" && before !== "") return { bang, wasEmpty: true, value, caret };
  if (bang && value !== "") return { bang, wasEmpty: false, value, caret };
  return { bang, wasEmpty, value, caret };
}

// checkBangModeAfterPaste (paste.go:36-53): a paste that leaves the text
// starting with "!" (spaces allowed) turns bang mode on, marked empty.
export function bangAfterPaste({ value, caret, bang }) {
  if (bang) return null;
  const trimmed = trimLeft(value);
  if (!trimmed.startsWith("!")) return null;
  const stripped = trimmed.slice(1);
  return { bang: true, wasEmpty: true, value: stripped, caret: Math.max(0, caret - (value.length - stripped.length)) };
}

// restoreRecalledDrafts (history.go:155-181): the drafts go after the field's
// text, a blank line between non-empty parts.
export function joinDrafts(text, drafts) {
  let out = text;
  for (const draft of drafts) {
    const prompt = draft?.text ?? "";
    if (out !== "" && prompt !== "") out += "\n\n";
    out += prompt;
  }
  return out;
}
