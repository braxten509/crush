// Replies as rich Markdown through the safe renderer (CHAT-014, CHAT-017;
// desktop-design 1.7, 4.1). The pieces:
//   lex.js          the stored text → the lexed text (plan markers gone) and marked's tokens
//   build.js        tokens → nodes, every text node in the source map
//   incremental.js  the stable prefix of a streaming reply
//   figures.js      which blocks become visuals chips
//   safety.js       the probe's safety section, registered with the renderer
//
//   renderMarkdown(stored, {messageId, sessionId, finished, plan, decorations, visual})
//     a <div class="md"> holding the reply, its SourceMap registered
//     (sourceMapOf(root)); attach guardLinks(root) before it reaches the page
//     (1.10's markdown.render)
import { createMarkdown } from "./incremental.js";
import "./safety.js";

export { createMarkdown } from "./incremental.js";
export { stableBoundary } from "./boundary.js";

export function renderMarkdown(stored, { messageId = "", sessionId = "", finished = true, plan = "none", decorations = [], visual = null, doc = document } = {}) {
  const root = doc.createElement("div");
  root.className = plan && plan !== "none" ? "md md-plan" : "md";
  createMarkdown(root, { messageId, sessionId, visual }).update(stored, { finished, decorations });
  return root;
}
