// A table figure (desktop-design 4.3): the Markdown table of a reply drawn as
// a real table for the right panel, its cells' marks and links drawn by the
// same builder as the chat (links are anchors the figure guards).
//
//   drawTable(source, {doc})   <div class="md figure-table"> holding the
//                              <table>, or null when source holds no table
import { Builder } from "./build.js";
import { lexMarkdown } from "./lex.js";
import { SourceMap } from "./sourcemap.js";

export function drawTable(source, { doc = document } = {}) {
  const text = String(source ?? "");
  const token = lexMarkdown(text).find((t) => t.type === "table");
  if (!token) return null;
  const holder = doc.createElement("div");
  holder.className = "md figure-table";
  const builder = new Builder({ doc, map: new SourceMap(holder, text), toStored: (i) => i });
  const base = Math.max(0, text.indexOf(token.raw));
  holder.append(builder.table(token, (i) => base + i));
  return holder;
}
