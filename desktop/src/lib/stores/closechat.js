// Closing a chat (owner, 2026-11-13): the close stops everything the chat
// runs (openchats.closeListed). A chat still working, waiting for an answer
// or running helpers or background jobs asks first, in the chat close box
// (components/ChatCloseBox.svelte); an idle chat closes at once.
import { hasDialog, openDialog } from "../dialogs.js";
import { phase3Kind } from "../leftlist/states.js";
import { closeListed } from "./openchats.svelte.js";

const BOX = "chat_close";

// kind: what the chat's row shows it doing ("working", "waiting",
// "background"), which a chat of another folder knows from its own engine;
// else this window's own reading of the chat.
export function askCloseChat(id, kind = "") {
  if (!id) return;
  const busy = kind || phase3Kind(id);
  if (busy) {
    // A box already open answers first: a busy chat never closes unasked.
    if (hasDialog(BOX) || openDialog(BOX, { id, kind: busy })) return;
  }
  closeListed(id).catch((error) => console.error("closing a chat failed", error));
}
