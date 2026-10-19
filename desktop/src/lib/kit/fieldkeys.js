// The terminal's editing keys for one-line fields (desktop-design-5-6 2.1,
// DLG-013 to DLG-017): the keys of bubbles v2.2.1's textinput that a browser
// field lacks, with the terminal's own rules (textinput/textinput.go:68-87,
// 399-564, 597-640). The browser keeps the rest: ←/→, Home/End, Backspace,
// Delete, Ctrl+←/→, Ctrl+Backspace, Ctrl+Delete, Ctrl+V and Shift+Insert.
//
//   Ctrl+F / Ctrl+B            one character forward / back
//   Alt+F, Alt+→ / Alt+B, Alt+← one word forward / back
//   Ctrl+H / Ctrl+D            delete the character before / after the caret
//   Ctrl+W, Alt+Backspace      delete the word before the caret
//   Alt+D, Alt+Delete          delete the word after the caret
//   Ctrl+U / Ctrl+K            delete everything before / after the caret
//   Ctrl+A / Ctrl+E            to the line's start / end
//
// A word is a run of characters that are not white space (Go's
// unicode.IsSpace), and positions count characters, not UTF-16 units, as the
// terminal's runes do. A masked field never shows where its words break: word
// moves go to the ends and word deletes clear to the ends
// (textinput.go:421-424, 468-471, 509-512, 541-544). With a selection, a
// delete key removes the selection and a move collapses it, as the
// terminal's editor does (textarea/textarea.go:1276-1346).
//
// Edits go through the page's editing commands, so Ctrl+Z undoes them and
// the field's input event fires as for typing (applyEdit).

// Go's unicode.IsSpace: the Latin-1 spaces and the White_Space property.
const SPACES = new Set([0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000]);
for (let code = 0x2000; code <= 0x200a; code++) SPACES.add(code);

const REPLACEMENT = 0xfffd; // utf8.RuneError

export function isSpace(ch) {
  return ch !== undefined && SPACES.has(ch.codePointAt(0));
}

// The field's text and selection as characters: {chars, start, end, caret}
// (caret: the moving end, where the terminal's cursor is).
export function fieldState(value, selectionStart, selectionEnd, direction = "forward") {
  const head = (offset) => [...value.slice(0, offset)].length;
  const start = head(selectionStart ?? value.length);
  const end = head(selectionEnd ?? value.length);
  return { chars: [...value], start, end, caret: direction === "backward" ? start : end };
}

// What one key does to a field, or null when the key is not one of these.
// → {chars, start, end}: the new text and selection, in characters.
export function fieldEdit(combo, state, { masked = false } = {}) {
  const { chars, start, end, caret } = state;
  const selected = start !== end;
  const cut = (from, to, at = from) => ({ chars: [...chars.slice(0, from), ...chars.slice(to)], start: at, end: at });
  const move = (at) => ({ chars, start: at, end: at });
  switch (combo) {
    case "Ctrl+F":
      return move(selected ? end : Math.min(caret + 1, chars.length));
    case "Ctrl+B":
      return move(selected ? start : Math.max(caret - 1, 0));
    case "Alt+F":
    case "Alt+ArrowRight":
      return move(masked ? chars.length : wordForward(chars, selected ? end : caret));
    case "Alt+B":
    case "Alt+ArrowLeft":
      return move(masked ? 0 : wordBackward(chars, selected ? start : caret));
    case "Ctrl+A":
      return move(0);
    case "Ctrl+E":
      return move(chars.length);
    case "Ctrl+H":
      if (selected) return cut(start, end);
      return caret > 0 ? cut(caret - 1, caret) : move(caret);
    case "Ctrl+D":
      if (selected) return cut(start, end);
      return caret < chars.length ? cut(caret, caret + 1) : move(caret);
    case "Ctrl+W":
    case "Alt+Backspace":
      if (selected) return cut(start, end);
      if (masked) return cut(0, caret);
      return cut(deleteWordStart(chars, caret), caret);
    case "Alt+D":
    case "Alt+Delete":
      if (selected) return cut(start, end);
      if (masked) return cut(caret, chars.length);
      return cut(caret, deleteWordEnd(chars, caret), caret);
    case "Ctrl+U":
      return selected ? cut(start, end) : cut(0, caret);
    case "Ctrl+K":
      return selected ? cut(start, end) : cut(caret, chars.length);
    default:
      return null;
  }
}

// textinput.go:504-532: back over spaces, then over the word.
export function wordBackward(chars, pos) {
  let i = pos - 1;
  while (i >= 0 && isSpace(chars[i])) i--;
  while (i >= 0 && !isSpace(chars[i])) i--;
  return i + 1;
}

// textinput.go:536-564: on over spaces, then over the word.
export function wordForward(chars, pos) {
  let i = pos;
  while (i < chars.length && isSpace(chars[i])) i++;
  while (i < chars.length && !isSpace(chars[i])) i++;
  return i;
}

// Where Ctrl+W's cut starts (textinput.go:416-458), the terminal's steps
// as they are: the space before the word stays, except at the very start.
export function deleteWordStart(chars, pos) {
  if (pos === 0 || chars.length === 0) return pos;
  let at = pos - 1;
  while (isSpace(chars[at])) {
    if (at <= 0) break;
    at--;
  }
  while (at > 0) {
    if (!isSpace(chars[at])) {
      at--;
    } else {
      at++;
      break;
    }
  }
  return at;
}

// Where Alt+D's cut ends (textinput.go:463-500): one character, then any
// spaces, then the word.
export function deleteWordEnd(chars, pos) {
  if (pos >= chars.length) return pos;
  let at = pos + 1;
  while (at < chars.length && isSpace(chars[at])) at++;
  while (at < chars.length && !isSpace(chars[at])) at++;
  return at;
}

// A pasted text on one line: each line break and tab becomes a space, other
// control characters go (runeutil.Sanitizer with ReplaceTabs(" ") and
// ReplaceNewlines(" "), textinput.go:297-305).
export function oneLine(text) {
  let out = "";
  for (const ch of text) {
    const code = ch.codePointAt(0);
    if (ch === "\r" || ch === "\n" || ch === "\t") out += " ";
    else if (code === REPLACEMENT || code < 0x20 || (code >= 0x7f && code <= 0x9f)) continue;
    else out += ch;
  }
  return out;
}

// Puts an edit on the element (an <input> or <textarea>): the changed part is
// replaced through the page's editing commands (so it can be undone and the
// input event fires), then the selection is set.
export function applyEdit(element, edit) {
  const before = element.value;
  const after = edit.chars.join("");
  const offset = (index) => edit.chars.slice(0, index).join("").length;
  if (after !== before) {
    let from = 0;
    while (from < before.length && from < after.length && before[from] === after[from]) from++;
    if (from > 0 && isHighSurrogate(before.charCodeAt(from - 1))) from--;
    let tailBefore = before.length;
    let tailAfter = after.length;
    while (tailBefore > from && tailAfter > from && before[tailBefore - 1] === after[tailAfter - 1]) {
      tailBefore--;
      tailAfter--;
    }
    if (tailBefore < before.length && isLowSurrogate(before.charCodeAt(tailBefore))) {
      tailBefore++;
      tailAfter++;
    }
    replaceRange(element, from, tailBefore, after.slice(from, tailAfter));
  }
  element.setSelectionRange(offset(edit.start), offset(edit.end), edit.backward ? "backward" : "forward");
}

const isHighSurrogate = (code) => code >= 0xd800 && code <= 0xdbff;
const isLowSurrogate = (code) => code >= 0xdc00 && code <= 0xdfff;

function replaceRange(element, from, to, text) {
  if (from === to && text === "") return;
  const before = element.value;
  const expected = before.slice(0, from) + text + before.slice(to);
  element.setSelectionRange(from, to);
  try {
    if (text === "") element.ownerDocument.execCommand("delete");
    else element.ownerDocument.execCommand("insertText", false, text);
  } catch {
    // No editing commands: set below.
  }
  if (element.value === expected) return;
  // A page without editing commands (or one that refused): set the text.
  element.value = expected;
  element.dispatchEvent(new Event("input", { bubbles: true }));
}
