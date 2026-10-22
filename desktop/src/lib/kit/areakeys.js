// The terminal's keys for multiline editors (desktop-design-5-6 2.1, DLG-018,
// DLG-019): the keys of bubbles v2.2.1's textarea that a browser <textarea>
// lacks, with the terminal's own rules (textarea/textarea.go:89-127,
// 869-1069, 1130-1141, 1276-1431). The browser keeps ↑/↓, ←/→, PgUp/PgDn,
// Home/End, Ctrl+Home/End, Shift+arrows, Ctrl+Shift+←/→, Backspace, Delete,
// Ctrl+Backspace/Delete, Enter and Ctrl+V.
//
//   Ctrl+P / Ctrl+N           the line above / below (Area.svelte moves the
//                             caret as ↑/↓ do, by drawn lines)
//   Alt+< / Alt+>             the start / end of the text
//   Alt+Shift+←, Alt+Shift+B  select a word back; Alt+Shift+→, Alt+Shift+F forward
//   Ctrl+G, Ctrl+A            select all (Ctrl+A: owner, 2026-10-21); Ctrl+Shift+C
//                             copies the selection
//   Alt+C / Alt+L / Alt+U     capitalize / lowercase / uppercase the next word
//   Ctrl+T                    swap the characters around the caret
//
// and the one-line field's keys (lib/kit/fieldkeys.js) with the editor's
// rules: they work on the caret's line, a delete at a line's edge joins the
// lines, and words run across lines (textarea.go:1000-1046).
import { deleteWordStart, isSpace } from "./fieldkeys.js";

// The editor's text as lines of characters with the selection as {row, col}
// points: {lines, anchor, caret, start, end}.
export function areaState(value, selectionStart, selectionEnd, direction = "forward") {
  const lines = value.split("\n").map((line) => [...line]);
  const point = (offset) => {
    let rest = [...value.slice(0, offset)].length;
    for (let row = 0; row < lines.length; row++) {
      if (rest <= lines[row].length) return { row, col: rest };
      rest -= lines[row].length + 1;
    }
    const row = lines.length - 1;
    return { row, col: lines[row].length };
  };
  const start = point(selectionStart ?? value.length);
  const end = point(selectionEnd ?? value.length);
  const back = direction === "backward";
  return { lines, start, end, caret: back ? start : end, anchor: back ? end : start };
}

const same = (a, b) => a.row === b.row && a.col === b.col;
const before = (a, b) => a.row < b.row || (a.row === b.row && a.col < b.col);

// What one key does to the editor, or null when the key is not one of these.
// → {chars, start, end, backward}: the new text and selection in characters
// (lib/kit/fieldkeys.js's applyEdit puts it on the page), or {copy: text}.
export function areaEdit(combo, state) {
  const { lines, start, end, caret, anchor } = state;
  const selected = !same(start, end);
  const at = (point) => ({ row: point.row, col: point.col });
  const done = (text, from, to = from, backward = false) => flat(text, from, to, backward);
  const moveTo = (point) => done(lines, point);
  const dropSelection = () => (selected ? removeRange(lines, start, end) : null);

  switch (combo) {
    case "Ctrl+F":
      return moveTo(characterRight(lines, at(caret)));
    case "Ctrl+B":
      return moveTo(characterLeft(lines, at(caret), false));
    case "Alt+F":
    case "Alt+ArrowRight":
      return moveTo(wordRight(lines, at(caret)));
    case "Alt+B":
    case "Alt+ArrowLeft":
      return moveTo(wordLeft(lines, at(caret)));
    case "Ctrl+A":
      return done(lines, { row: 0, col: 0 }, { row: lines.length - 1, col: lines[lines.length - 1].length });
    case "Ctrl+E":
      return moveTo({ row: caret.row, col: lines[caret.row].length });
    case "Alt+<":
    case "Alt+Shift+<":
      return moveTo({ row: 0, col: 0 });
    case "Alt+>":
    case "Alt+Shift+>":
      return moveTo({ row: lines.length - 1, col: lines[lines.length - 1].length });
    case "Alt+Shift+ArrowRight":
    case "Alt+Shift+F": {
      const head = wordRight(lines, at(caret));
      return done(lines, before(head, anchor) ? head : anchor, before(head, anchor) ? anchor : head, before(head, anchor));
    }
    case "Alt+Shift+ArrowLeft":
    case "Alt+Shift+B": {
      const head = wordLeft(lines, at(caret));
      return done(lines, before(head, anchor) ? head : anchor, before(head, anchor) ? anchor : head, before(head, anchor));
    }
    case "Ctrl+H": {
      const cut = dropSelection();
      if (cut) return cut;
      if (caret.col > 0) return removeRange(lines, { row: caret.row, col: caret.col - 1 }, caret);
      return caret.row > 0 ? removeRange(lines, endOf(lines, caret.row - 1), caret) : moveTo(caret);
    }
    case "Ctrl+D": {
      const cut = dropSelection();
      if (cut) return cut;
      if (caret.col < lines[caret.row].length) return removeRange(lines, caret, { row: caret.row, col: caret.col + 1 });
      return caret.row < lines.length - 1 ? removeRange(lines, caret, { row: caret.row + 1, col: 0 }) : moveTo(caret);
    }
    case "Ctrl+W":
    case "Alt+Backspace": {
      const cut = dropSelection();
      if (cut) return cut;
      if (caret.col <= 0) return caret.row > 0 ? removeRange(lines, endOf(lines, caret.row - 1), caret) : moveTo(caret);
      return removeRange(lines, { row: caret.row, col: deleteWordStart(lines[caret.row], caret.col) }, caret);
    }
    case "Alt+D":
    case "Alt+Delete": {
      const cut = dropSelection();
      if (cut) return cut;
      const line = lines[caret.row];
      if (caret.col >= line.length) return caret.row < lines.length - 1 ? removeRange(lines, caret, { row: caret.row + 1, col: 0 }) : moveTo(caret);
      let col = caret.col;
      while (col < line.length && isSpace(line[col])) col++;
      while (col < line.length && !isSpace(line[col])) col++;
      return removeRange(lines, caret, { row: caret.row, col });
    }
    case "Ctrl+U": {
      const cut = dropSelection();
      if (cut) return cut;
      if (caret.col <= 0) return caret.row > 0 ? removeRange(lines, endOf(lines, caret.row - 1), caret) : moveTo(caret);
      return removeRange(lines, { row: caret.row, col: 0 }, caret);
    }
    case "Ctrl+K": {
      const cut = dropSelection();
      if (cut) return cut;
      if (caret.col >= lines[caret.row].length) return caret.row < lines.length - 1 ? removeRange(lines, caret, { row: caret.row + 1, col: 0 }) : moveTo(caret);
      return removeRange(lines, caret, endOf(lines, caret.row));
    }
    case "Alt+U":
      return changeWord(lines, at(caret), (ch) => oneToOne(ch, ch.toUpperCase()));
    case "Alt+L":
      return changeWord(lines, at(caret), (ch) => oneToOne(ch, ch.toLowerCase()));
    case "Alt+C":
      return changeWord(lines, at(caret), (ch, i) => (i === 0 ? oneToOne(ch, ch.toUpperCase()) : ch));
    case "Ctrl+T":
      return transpose(lines, at(caret));
    case "Ctrl+Shift+C":
      return selected ? { copy: textOf(lines, start, end) } : { copy: null };
    default:
      return null;
  }
}

// Go's simple case mapping keeps one character for one: a mapping that
// would grow ("ß" → "SS") leaves the character as it is.
function oneToOne(ch, mapped) {
  return [...mapped].length === 1 ? mapped : ch;
}

function endOf(lines, row) {
  return { row, col: lines[row].length };
}

// textarea.go:970-979.
function characterRight(lines, p) {
  if (p.col < lines[p.row].length) return { row: p.row, col: p.col + 1 };
  if (p.row < lines.length - 1) return { row: p.row + 1, col: 0 };
  return p;
}

// textarea.go:984-995; insideLine stops on the last character of the line above.
function characterLeft(lines, p, insideLine) {
  let { row, col } = p;
  if (col === 0 && row !== 0) {
    row--;
    col = lines[row].length;
    if (!insideLine) return { row, col };
  }
  if (col > 0) col--;
  return { row, col };
}

// textarea.go:1000-1018: back to a character that is not a space, then to
// the start of its word.
function wordLeft(lines, p) {
  let point = p;
  for (;;) {
    const next = characterLeft(lines, point, true);
    if (same(next, point)) return point;
    point = next;
    if (point.col < lines[point.row].length && !isSpace(lines[point.row][point.col])) break;
  }
  let { row, col } = point;
  while (col > 0 && !isSpace(lines[row][col - 1])) col--;
  return { row, col };
}

// textarea.go:1027-1046: on over spaces and line ends, then to the word's end.
function wordRight(lines, p) {
  return walkWord(lines, p).end;
}

function walkWord(lines, p) {
  let point = p;
  const last = lines.length - 1;
  while (point.col >= lines[point.row].length || isSpace(lines[point.row][point.col])) {
    if (point.row === last && point.col === lines[last].length) break;
    point = characterRight(lines, point);
  }
  const row = point.row;
  let col = point.col;
  const from = col;
  while (col < lines[row].length && !isSpace(lines[row][col])) col++;
  return { row, from, end: { row, col } };
}

// textarea.go:1049-1069: the next word changed, the caret after it.
function changeWord(lines, p, change) {
  const { row, from, end } = walkWord(lines, p);
  const next = lines.map((line) => [...line]);
  for (let col = from; col < end.col; col++) next[row][col] = change(next[row][col], col - from);
  return flat(next, end);
}

// textarea.go:886-897.
function transpose(lines, p) {
  const line = lines[p.row];
  if (p.col === 0 || line.length < 2) return flat(lines, p);
  let col = p.col >= line.length ? p.col - 1 : p.col;
  const next = lines.map((l) => [...l]);
  [next[p.row][col - 1], next[p.row][col]] = [next[p.row][col], next[p.row][col - 1]];
  if (col < line.length) col++;
  return flat(next, { row: p.row, col });
}

function removeRange(lines, from, to) {
  const next = lines.slice(0, from.row);
  next.push([...lines[from.row].slice(0, from.col), ...lines[to.row].slice(to.col)]);
  next.push(...lines.slice(to.row + 1));
  return flat(next, from);
}

function textOf(lines, from, to) {
  if (from.row === to.row) return lines[from.row].slice(from.col, to.col).join("");
  const parts = [lines[from.row].slice(from.col).join("")];
  for (let row = from.row + 1; row < to.row; row++) parts.push(lines[row].join(""));
  parts.push(lines[to.row].slice(0, to.col).join(""));
  return parts.join("\n");
}

// Lines and points back to one run of characters with character offsets.
function flat(lines, from, to = from, backward = false) {
  const chars = [];
  const offsets = [];
  lines.forEach((line, row) => {
    if (row > 0) chars.push("\n");
    offsets.push(chars.length);
    chars.push(...line);
  });
  const offset = (point) => offsets[point.row] + point.col;
  return { chars, start: offset(from), end: offset(to), backward };
}
