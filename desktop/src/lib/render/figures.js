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
//                           address is refused, are not visuals.
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
      const path = /^file:/i.test(src) ? decodeURIComponent(url.pathname) : src;
      if (/^file:/i.test(src) && url.host && url.host !== "localhost") return null;
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

// The visual a token is, or null.
function visualOf(token) {
  if (token.type === "table") return { kind: "table", title: tableTitle(token), source: token.raw };
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
  const walk = (list, top, inLink) => {
    if (!list) return;
    for (let i = 0; i < list.length; i++) {
      const token = list[i];
      const t = top ?? i;
      const visual = inLink ? null : visualOf(token);
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
