// The one Markdown lexer setup and the text it reads. A reply's stored text
// (M.Content().Text) loses its plan markers (plan.js) and its \r\n and \r
// become \n before marked sees it; toStored maps every offset of the lexed
// text back to the stored text, so the source map and the figure ids speak in
// stored offsets.
//
//   prepare(stored)    {text, toStored(i)}: the lexed text and its map
//   lexMarkdown(text)  marked's tokens (GFM, math), never HTML
import { Marked } from "marked";
import { mathExtensions } from "./math.js";
import { stripPlanMarkersMapped } from "./plan.js";

const marked = new Marked({ gfm: true, breaks: false, pedantic: false });
marked.use({ extensions: mathExtensions });

export function lexMarkdown(text) {
  return marked.lexer(text);
}

export function prepare(stored) {
  const { text: stripped, toSource } = stripPlanMarkersMapped(String(stored ?? ""));
  if (!stripped.includes("\r")) return { text: stripped, toStored: toSource };
  // Each lexed character remembers where its run starts in the stripped text;
  // "\r\n" becomes one "\n" that stands for both.
  const out = [];
  const back = [];
  for (let i = 0; i < stripped.length; i++) {
    const ch = stripped[i];
    if (ch === "\r") {
      back.push(i);
      out.push("\n");
      if (stripped[i + 1] === "\n") i++;
    } else {
      back.push(i);
      out.push(ch);
    }
  }
  back.push(stripped.length);
  const text = out.join("");
  return { text, toStored: (i) => toSource(back[Math.max(0, Math.min(i, back.length - 1))]) };
}
