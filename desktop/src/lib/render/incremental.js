// Incremental Markdown (CHAT-018; desktop-design 1.6 item 4). A streaming
// reply keeps a stable prefix: the blocks before the stable boundary
// (boundary.js) are drawn once and kept; each update lexes only the text after
// them, and a tail block whose source and place did not change keeps its
// nodes too. A retry that rewrites earlier text (the new text no longer starts
// with the kept prefix) drops everything and draws afresh. The result is
// always exactly what a full render of the same text draws.
//
//   createMarkdown(root, {messageId, sessionId, visual, release})
//     .update(stored, {finished, decorations})   draws stored into root
//     .destroy()                                 releases every visual
//   visual(info) → node    draws a visuals chip (MarkdownView mounts
//                          VisualChip or FigureView); null draws a plain chip
//   release(node)          a visual node left the page
//   stableBoundary         re-exported from boundary.js
import { Builder } from "./build.js";
import { stableBoundary } from "./boundary.js";
import { lexMarkdown, prepare } from "./lex.js";
import { SourceMap } from "./sourcemap.js";

export { stableBoundary };

function decorationsKey(decorations) {
  return decorations.map((d) => `${d.kind}:${d.start}:${d.end}`).join(",");
}

export function createMarkdown(root, { messageId = "", sessionId = "", visual = null, release = null } = {}) {
  const doc = root.ownerDocument;
  let map = null;
  let decoKey = null;
  // The stable blocks: [{nodes, visuals}], and where they end.
  let stable = [];
  let stableEnd = 0;
  let stablePrefix = "";
  let stableStoredEnd = 0;
  let stableVisuals = 0;
  // The last update's tail blocks by key.
  let tail = new Map();

  const releaseAll = (visuals) => {
    if (release) for (const node of visuals) release(node);
  };

  function reset(stored) {
    for (const block of stable) releaseAll(block.visuals);
    for (const block of tail.values()) releaseAll(block.visuals);
    stable = [];
    stableEnd = 0;
    stablePrefix = "";
    stableStoredEnd = 0;
    stableVisuals = 0;
    tail = new Map();
    map = new SourceMap(root, stored);
  }

  function update(stored, { finished = true, decorations = [] } = {}) {
    stored = String(stored ?? "");
    const { text, toStored } = prepare(stored);
    const key = decorationsKey(decorations);
    if (!map || key !== decoKey || !text.startsWith(stablePrefix) || toStored(stableEnd) !== stableStoredEnd) {
      reset(stored);
      decoKey = key;
    }
    map.source = stored;
    map.decorations = [...decorations].sort((a, b) => a.start - b.start);

    const boundary = finished ? text.length : stableBoundary(text, { lex: lexMarkdown });
    // The boundary only moves back when something later changes what came
    // before it (a link definition): nothing kept can stay.
    if (boundary < stableEnd) reset(stored);

    const tokens = lexMarkdown(text.slice(stableEnd));
    const builder = new Builder({ doc, map, toStored, messageId, sessionId, visual });
    const counts = builder.setVisuals(tokens, stableVisuals);
    // A reference link draws from a definition anywhere in the text, so a
    // tail block is only reused while the definitions stay the same.
    const links = tokens.links && Object.keys(tokens.links).length ? JSON.stringify(tokens.links) : "";

    const nextTail = new Map();
    const tailNodes = [];
    let at = stableEnd;
    let run = stableVisuals;
    let stillStable = true;
    tokens.forEach((token, i) => {
      const start = at;
      const end = (at += token.raw.length);
      const isStable = stillStable && end <= boundary;
      stillStable = isStable;
      const base = run;
      run += counts[i];
      const blockKey = `${start}\u0000${isStable ? 1 : 0}\u0000${base}\u0000${links}\u0000${token.raw}`;
      let block = tail.get(blockKey);
      if (block) tail.delete(blockKey);
      else {
        builder.stable = isStable;
        builder.created = [];
        const node = builder.block(token, start);
        block = { nodes: node ? [node] : [], visuals: builder.created };
      }
      if (isStable) {
        stable.push(block);
        stableEnd = end;
        stableVisuals = run;
      } else {
        nextTail.set(blockKey, block);
        tailNodes.push(...block.nodes);
      }
    });
    for (const block of tail.values()) releaseAll(block.visuals);
    tail = nextTail;
    stablePrefix = text.slice(0, stableEnd);
    stableStoredEnd = toStored(stableEnd);

    // Put the nodes in place, touching only what changed.
    const wanted = [];
    for (const block of stable) wanted.push(...block.nodes);
    wanted.push(...tailNodes);
    for (let i = 0; i < wanted.length; i++) {
      const current = root.childNodes[i] ?? null;
      if (current !== wanted[i]) root.insertBefore(wanted[i], current);
    }
    while (root.childNodes.length > wanted.length) root.lastChild.remove();
    return map;
  }

  function destroy() {
    for (const block of stable) releaseAll(block.visuals);
    for (const block of tail.values()) releaseAll(block.visuals);
    stable = [];
    tail = new Map();
    root.replaceChildren();
  }

  return {
    update,
    destroy,
    get map() {
      return map;
    },
  };
}
