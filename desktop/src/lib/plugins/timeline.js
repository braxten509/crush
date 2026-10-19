// The timeline's right-click menu entries (desktop-design 4.2, 1.10): Copy
// (order 20), Quote in reply (30) and Copy whole message (40); phase 4 adds
// Branch from here (10). Each runs on the context the menu was opened with
// (Timeline.svelte): {sessionId, key, item, message, range, text, items},
// where range and text are the timeline's text selection then (null and "").
import { COPIED_SELECTION, itemCopy, messageItemFor, quoteLines, withQuote, writeClipboard } from "../copy.js";
import { registerMenuItem } from "../menu.js";
import { composer, focusField, setText } from "../stores/composer.svelte.js";

const selected = (ctx) => Boolean(ctx?.range) && Boolean(ctx.text);

registerMenuItem({
  id: "copy",
  label: "Copy",
  order: 20,
  enabled: selected,
  run: (ctx) => {
    if (selected(ctx)) writeClipboard(ctx.text, COPIED_SELECTION);
  },
});

registerMenuItem({
  id: "quote",
  label: "Quote in reply",
  order: 30,
  enabled: selected,
  run: (ctx) => {
    const quote = quoteLines(ctx.text);
    if (!quote) return;
    setText(withQuote(composer.drafts[ctx.sessionId ?? ""]?.text ?? "", quote));
    focusField();
  },
});

// The same text as c/y on the message (CHAT-012, CHAT-030).
function wholeMessage(ctx) {
  const target = messageItemFor(ctx?.item, ctx?.items ?? []);
  return target ? itemCopy(target, ctx.message) : null;
}

registerMenuItem({
  id: "copy-message",
  label: "Copy whole message",
  order: 40,
  enabled: (ctx) => wholeMessage(ctx) !== null,
  run: (ctx) => {
    const copy = wholeMessage(ctx);
    if (copy) writeClipboard(copy.text, copy.notice);
  },
});
