// Visuals (CHAT-014; desktop-design 1.5.6 "Figure ids", 4.3). Tables, display
// math, Mermaid fences and pictures leave the prose: the chat draws a chip
// where each was, the right panel draws the figure. visualBlocks is the one
// function that decides which blocks are visuals and numbers them, so a chip
// (markdown.js) and its figure (extractFigures, read by the panel) always
// share the id "<message id>:<n>".
//
//   visualBlocks(tokens)    [{token, top, index, kind, title, source, src?, host?}]
//                           in source order; top is the top-level block that holds it.
//                           Pictures inside links or tables, and pictures whose
//                           address is refused, are not visuals. A table's
//                           source carries the reply's link definitions its
//                           reference links use.
//   extractFigures(sessionId, messageId, text, {finished})
//                           Figure[] (lib/types.js), pure; for a message still
//                           streaming (finished false) only blocks before the
//                           stable boundary count, so a figure appears when its
//                           block closes and its index never changes
//   pictureOf(target)       {kind: "data"|"blob"|"web"|"file", src, host?, name} or null
//   KIND_LABEL              the small-caps word of each kind
import { stableBoundary } from "./boundary.js";
import { lexMarkdown, prepare } from "./lex.js";
import { mathTitle } from "./math.js";
import { diagramTitle } from "./mermaid.js";
import { decodeEntities } from "./sanitize.js";

export const KIND_LABEL = { diagram: "DIAGRAM", math: "MATH", table: "TABLE", picture: "PICTURE" };

// A fence's language word.
export function fenceLang(token) {
  return String(token.lang ?? "")
    .trim()
    .split(/\s+/)[0]
    .toLowerCase();
}

export function pictureOf(target) {
  const src = decodeEntities(String(target ?? "")).trim();
  if (!src) return null;
  let url;
  try {
    url = new URL(src, "file:///");
  } catch {
    return null;
  }
  const name = (path) => {
    const last = path.split(/[\\/]/).filter(Boolean).pop() ?? "";
    try {
      return decodeURIComponent(last);
    } catch {
      return last;
    }
  };
  switch (url.protocol) {
    case "data:":
      return /^data:image\//i.test(src) ? { kind: "data", src, name: "" } : null;
    case "blob:":
      return { kind: "blob", src, name: "" };
    case "http:":
    case "https:":
      return { kind: "web", src, host: url.host, name: name(url.pathname) };
    case "file:": {
      if (/^file:/i.test(src) && url.host && url.host !== "localhost") return null;
      let path = src;
      if (/^file:/i.test(src)) {
        // A broken %-escape ("%ZZ", or "%FF", which is not UTF-8) names no
        // file: the picture is refused and shows as its words.
        try {
          path = decodeURIComponent(url.pathname);
        } catch {
          return null;
        }
      }
      return { kind: "file", src: path, name: name(path) };
    }
    default:
      return null;
  }
}

// The words of inline tokens without their marks (titles).
export function plainText(tokens) {
  let out = "";
  for (const t of tokens ?? []) {
    if (t.tokens) out += plainText(t.tokens);
    else if (t.type === "text" || t.type === "escape" || t.type === "codespan") out += t.type === "text" ? decodeEntities(t.text) : t.text;
    else if (t.type === "image") out += t.text ?? "";
    else if (t.type === "mathInline") out += t.text;
    else if (t.type === "br") out += " ";
  }
  return out;
}

function tableTitle(token) {
  return token.header
    .slice(0, 3)
    .map((cell) => plainText(cell.tokens).replace(/\s+/g, " ").trim())
    .join(" · ");
}

// A link label as marked matches it: whitespace runs as one space, the ends
// trimmed, the case folded.
const labelKey = (label) => label.replace(/\s+/g, " ").trim().toLowerCase().toUpperCase().toLowerCase();

// The reply's link definitions ("[ref]: https://…") by label, wherever they
// stand (quotes and list items too); marked keeps the first of each label.
function definitionsOf(tokens, out = new Map()) {
  for (const token of tokens ?? []) {
    if (token.type === "def") {
      if (!out.has(token.tag)) out.set(token.tag, token.raw.trim());
    } else if (token.type === "list") for (const item of token.items) definitionsOf(item.tokens, out);
    else if (token.tokens) definitionsOf(token.tokens, out);
  }
  return out;
}

// The labels inline tokens' reference links and pictures resolved through:
// "[text][label]", "[label][]" and "[label]".
function labelsOf(tokens, out) {
  for (const t of tokens ?? []) {
    if ((t.type === "link" || t.type === "image") && /^!?\[/.test(t.raw) && t.raw.endsWith("]")) {
      const full = /\]\[((?:\\.|[^[\]\\])+)\]$/.exec(t.raw)?.[1];
      out.add(labelKey(full ?? t.raw.replace(/^!?\[/, "").replace(/\](?:\[\])?$/, "")));
    }
    if (t.tokens) labelsOf(t.tokens, out);
  }
  return out;
}

// A table figure's source: its Markdown, then (after a blank line) the reply's
// definitions its reference links use. The panel draws the table from this
// source alone, so "[manual][ref]" needs "[ref]: <address>" along to be a link.
function tableSource(token, definitions) {
  const labels = new Set();
  for (const cell of [...token.header, ...token.rows.flat()]) labelsOf(cell.tokens, labels);
  const used = [...labels].map((label) => definitions().get(label)).filter(Boolean);
  return used.length ? `${token.raw.replace(/\n+$/, "")}\n\n${used.join("\n")}` : token.raw;
}

// The visual a token is, or null. definitions() gives the reply's link
// definitions (read once, when a table first needs them).
function visualOf(token, definitions) {
  if (token.type === "table") return { kind: "table", title: tableTitle(token), source: tableSource(token, definitions) };
  if (token.type === "mathBlock") return { kind: "math", title: mathTitle(token.text), source: token.text };
  if (token.type === "code") {
    const lang = fenceLang(token);
    if (lang === "mermaid") return { kind: "diagram", title: diagramTitle(token.text), source: token.text };
    if (lang === "math") return { kind: "math", title: mathTitle(token.text), source: token.text };
    return null;
  }
  if (token.type === "image") {
    const picture = pictureOf(token.href);
    if (!picture) return null;
    const alt = decodeEntities(token.text ?? "").trim();
    const visual = { kind: "picture", title: alt || picture.name || picture.host || "", source: picture.src, src: picture.src };
    if (picture.host) visual.host = picture.host;
    return visual;
  }
  return null;
}

export function visualBlocks(tokens) {
  const out = [];
  let definitions = null;
  const definitionsOnce = () => (definitions ??= definitionsOf(tokens));
  const walk = (list, top, inLink) => {
    if (!list) return;
    for (let i = 0; i < list.length; i++) {
      const token = list[i];
      const t = top ?? i;
      const visual = inLink ? null : visualOf(token, definitionsOnce);
      if (visual) out.push({ token, top: t, index: out.length, ...visual });
      else if (token.type === "list") for (const item of token.items) walk(item.tokens, t, inLink);
      else if (token.tokens) walk(token.tokens, t, inLink || token.type === "link");
    }
  };
  walk(tokens, null, false);
  return out;
}

export function figureOf(sessionId, messageId, visual) {
  const figure = {
    id: `${messageId}:${visual.index}`,
    sessionId,
    messageId,
    index: visual.index,
    kind: visual.kind,
    title: visual.title,
    source: visual.source,
  };
  if (visual.src !== undefined) figure.src = visual.src;
  if (visual.host !== undefined) figure.host = visual.host;
  return figure;
}

export function extractFigures(sessionId, messageId, text, { finished = true } = {}) {
  const { text: lexed } = prepare(text);
  const tokens = lexMarkdown(lexed);
  const boundary = finished ? lexed.length : stableBoundary(lexed, { lex: lexMarkdown });
  const ends = [];
  let at = 0;
  for (const token of tokens) ends.push((at += token.raw.length));
  return visualBlocks(tokens)
    .filter((visual) => ends[visual.top] <= boundary)
    .map((visual) => figureOf(sessionId, messageId, visual));
}
