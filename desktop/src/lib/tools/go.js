// Go-compatible helpers for the tool views. The terminal decodes a tool's
// input with encoding/json into typed structs, re-encodes maps with
// json.Marshal, prints values with fmt's %v and title-cases names with
// x/text/cases, and the desktop's headers and copy text read exactly as its
// do (desktop-design CHAT-046 to CHAT-077).
import { cells, charCells } from "../diff.js";

// -- Decoding into a typed shape (encoding/json Unmarshal into a struct) --
//
// A shape names each field's type: "string", "int", "bool", "float", "any",
// ["array", element], a nested shape object, or a function (value, state)
// that decodes the value itself. Like Go: a key matches its field exactly or
// else ignoring case, unknown keys are skipped, null leaves the zero value, and
// a value of the wrong type leaves the field at zero and fails the decode while
// the other fields still decode (what a "lenient" tool draws, CHAT-051).
// JavaScript numbers do not keep their text, so 10.0 counts as an integer
// here while Go refuses it for an int field.

const ZEROS = { string: "", int: 0, bool: false, float: 0, any: null };

export function zeroOf(type) {
  if (typeof type === "string") return ZEROS[type];
  if (Array.isArray(type)) return [];
  if (typeof type === "function") return type.zero ?? null;
  const out = {};
  for (const [name, t] of Object.entries(type)) out[name] = zeroOf(t);
  return out;
}

function decodeValue(value, type, state) {
  if (value === null) return zeroOf(type);
  if (typeof type === "function") return type(value, state);
  switch (type) {
    case "any":
      return value;
    case "string":
      if (typeof value === "string") return value;
      break;
    case "bool":
      if (typeof value === "boolean") return value;
      break;
    case "int":
      if (typeof value === "number" && Number.isInteger(value)) return value;
      break;
    case "float":
      if (typeof value === "number") return value;
      break;
    default:
      if (Array.isArray(type)) {
        if (Array.isArray(value)) return value.map((v) => decodeValue(v, type[1], state));
        break;
      }
      if (typeof value === "object" && !Array.isArray(value)) return decodeFields(value, type, state);
  }
  state.ok = false;
  return zeroOf(type);
}

function decodeFields(object, shape, state) {
  const out = zeroOf(shape);
  const names = Object.keys(shape);
  for (const [key, value] of Object.entries(object)) {
    const lower = key.toLowerCase();
    const name = Object.hasOwn(shape, key) ? key : names.find((n) => n.toLowerCase() === lower);
    if (name === undefined || value === null) continue;
    out[name] = decodeValue(value, shape[name], state);
  }
  return out;
}

// decodeAs(value, type) → {value, ok}: an already parsed JSON value decoded
// into the type (for decoders written as functions).
export function decodeAs(value, type) {
  const state = { ok: true };
  const decoded = decodeValue(value, type, state);
  return { value: decoded, ok: state.ok };
}

// decode(input, shape) → {value, ok}: ok is false when the text is not JSON
// or a value has the wrong type (value then holds whatever did decode).
export function decode(input, shape) {
  let parsed;
  try {
    parsed = JSON.parse(input);
  } catch {
    return { value: zeroOf(shape), ok: false };
  }
  const state = { ok: true };
  const value = decodeValue(parsed, shape, state);
  return { value, ok: state.ok };
}

// parseJSON(text) → the value, or undefined when the text is not JSON.
export function parseJSON(text) {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

// A JSON object decoded into map[string]any: {value, ok}; null decodes to an
// empty map, anything but an object fails.
export function decodeMap(input) {
  const value = parseJSON(input);
  if (value === null) return { value: {}, ok: true };
  if (value === undefined || typeof value !== "object" || Array.isArray(value)) return { value: {}, ok: false };
  return { value, ok: true };
}

// -- Encoding (json.Marshal of decoded values) --

// Go orders map keys by their UTF-8 bytes, which is code point order.
export function compareGoStrings(a, b) {
  const ia = a[Symbol.iterator]();
  const ib = b[Symbol.iterator]();
  for (;;) {
    const x = ia.next();
    const y = ib.next();
    if (x.done || y.done) return x.done ? (y.done ? 0 : -1) : 1;
    const cx = x.value.codePointAt(0);
    const cy = y.value.codePointAt(0);
    if (cx !== cy) return cx - cy;
  }
}

// Go decodes a lone surrogate escape to U+FFFD.
const LONE_SURROGATE = /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/g;

// A JSON string as encoding/json writes it: HTML-safe (<, > and & escaped)
// and with U+2028 and U+2029 escaped.
export function goQuote(text) {
  return JSON.stringify(String(text).replace(LONE_SURROGATE, "\ufffd")).replace(
    /[<>&\u2028\u2029]/g,
    (c) => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"),
  );
}

// json.Marshal of a value decoded into any: maps with sorted keys, numbers as
// float64 (JavaScript's shortest form is Go's for every float64 but -0).
export function goMarshal(value) {
  if (value === null || value === undefined) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") return Object.is(value, -0) ? "-0" : String(value);
  if (typeof value === "string") return goQuote(value);
  if (Array.isArray(value)) return "[" + value.map(goMarshal).join(",") + "]";
  const keys = Object.keys(value).sort(compareGoStrings);
  return "{" + keys.map((k) => goQuote(k) + ":" + goMarshal(value[k])).join(",") + "}";
}

// -- fmt's %v --

// %v of a float64: %g with the shortest digits, exponent form below 1e-4 or
// from 1e6 up, with at least two exponent digits ("1e+06").
export function goFloat(n) {
  if (Number.isNaN(n)) return "NaN";
  if (n === Infinity) return "+Inf";
  if (n === -Infinity) return "-Inf";
  if (n === 0) return Object.is(n, -0) ? "-0" : "0";
  const [mantissa, exponent] = n.toExponential().split("e");
  const exp = Number(exponent);
  if (exp < -4 || exp >= 6) return `${mantissa}e${exp < 0 ? "-" : "+"}${String(Math.abs(exp)).padStart(2, "0")}`;
  return String(n);
}

// fmt.Sprintf("%v", v) of a value decoded into any.
export function goSprintV(value) {
  if (value === null || value === undefined) return "<nil>";
  if (typeof value === "string") return value;
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") return goFloat(value);
  if (Array.isArray(value)) return "[" + value.map(goSprintV).join(" ") + "]";
  const keys = Object.keys(value).sort(compareGoStrings);
  return "map[" + keys.map((k) => k + ":" + goSprintV(value[k])).join(" ") + "]";
}

// time.Duration(seconds * time.Second).String() for whole seconds.
export function goDuration(seconds) {
  const sign = seconds < 0 ? "-" : "";
  const total = Math.abs(seconds);
  if (total === 0) return "0s";
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${sign}${h}h${m}m${s}s`;
  if (m > 0) return `${sign}${m}m${s}s`;
  return `${sign}${s}s`;
}

// -- Names --

// Characters that keep a word going in x/text's title caser: letters, digits,
// marks, connectors (underscore) and the in-word marks such as ' . :. Two
// in-word marks in a row end the word.
const WORD = /[\p{L}\p{N}\p{M}\p{Pc}]/u;
const MID = /['.:·’‘‛‧︓﹕：＇．,;﹐﹔，；]/u;

function titleChar(ch) {
  const up = ch.toUpperCase();
  return up.length > ch.length ? up[0] + up.slice(1).toLowerCase() : up;
}

// stringext.Capitalize: cases.Title(language.English): each word's first
// letter upper case, its other letters lower case ("mcp github" → "Mcp Github").
export function goTitle(text) {
  let out = "";
  let inWord = false;
  let lastMid = false;
  for (const ch of String(text)) {
    const cased = ch.toLowerCase() !== ch.toUpperCase();
    const mid = MID.test(ch);
    if (cased) {
      out += inWord ? ch.toLowerCase() : titleChar(ch);
      inWord = true;
    } else {
      out += ch;
      if (mid ? lastMid : !WORD.test(ch)) inWord = false;
    }
    lastMid = mid;
  }
  return out;
}

// humanizedToolName (tool_result_content.go:17-21): "mcp_github_get" → "Mcp Github Get".
export function humanize(name) {
  return goTitle(String(name).replaceAll("_", " ").replaceAll("-", " "));
}

// fsext.PrettyPath shortens the home folder to "~". The window is not told the
// home folder (app_info has none), so paths show as the engine sent them.
export function prettyPath(path) {
  return String(path ?? "");
}

// The base name of a slash path (filepath.Base): "" → ".", "/" → "/".
export function baseName(path) {
  let p = String(path ?? "");
  if (p === "") return ".";
  while (p.length > 1 && p.endsWith("/")) p = p.slice(0, -1);
  if (p === "/") return "/";
  const i = p.lastIndexOf("/");
  return i >= 0 ? p.slice(i + 1) : p;
}

// The extension of a slash path, with its dot (filepath.Ext).
export function extName(path) {
  const p = String(path ?? "");
  for (let i = p.length - 1; i >= 0 && p[i] !== "/"; i--) if (p[i] === ".") return p.slice(i);
  return "";
}

// -- Text --

// stringext.NormalizeSpace: CRLF to LF, tabs to four spaces, and the leading
// and trailing newlines trimmed.
export function normalizeSpace(text) {
  return String(text ?? "")
    .replaceAll("\r\n", "\n")
    .replaceAll("\t", "    ")
    .replace(/^\n+|\n+$/g, "");
}

// strings.Fields joined with single spaces.
export function collapseSpace(text) {
  return String(text ?? "")
    .split(/[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+/)
    .filter(Boolean)
    .join(" ");
}

// ansi.Truncate(text, width, "…") on plain text: whole characters while they
// fit, the tail counted in the width.
export function truncate(text, width, tail = "…") {
  const s = String(text ?? "");
  if (cells(s) <= width) return s;
  const room = width - cells(tail);
  if (room < 0) return "";
  let out = "";
  let used = 0;
  for (const ch of s) {
    const w = charCells(ch);
    if (used + w > room) break;
    out += ch;
    used += w;
  }
  return out + tail;
}

// The longest start of text within limit UTF-8 bytes, cut between characters
// (Go cuts the bytes themselves; a cut through a character would show as "�").
export function utf8Prefix(text, limit) {
  let out = "";
  let used = 0;
  for (const ch of String(text)) {
    const size = utf8Length(ch);
    if (used + size > limit) break;
    out += ch;
    used += size;
  }
  return out;
}

export function utf8Length(text) {
  let n = 0;
  for (const ch of String(text)) {
    const code = ch.codePointAt(0);
    n += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
  }
  return n;
}

// formatSize (tool_output.go:147-160): "12.3 KB", with fmt's %.1f rounding
// (half to even on the exact value).
export function formatSize(bytes) {
  const kb = 1024;
  const mb = kb * 1024;
  if (bytes >= mb) return `${fixed1(bytes, mb)} MB`;
  if (bytes >= kb) return `${fixed1(bytes, kb)} KB`;
  return `${bytes} B`;
}

function fixed1(num, den) {
  const scaled = num * 10;
  let q = Math.floor(scaled / den);
  const r = scaled - q * den;
  if (r * 2 > den || (r * 2 === den && q % 2 === 1)) q += 1;
  return `${Math.floor(q / 10)}.${q % 10}`;
}

// The size of base64 data as the terminal estimates it: len(data) * 3 / 4.
export function base64Size(data) {
  return Math.floor((String(data ?? "").length * 3) / 4);
}
