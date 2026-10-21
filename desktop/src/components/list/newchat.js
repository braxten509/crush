// LIST-033: the New chat button, Ctrl+N and the "New Session" action. The
// landing page shows (sessions.newChat: no record until the first send; a
// busy chat answers with the terminal's warning), then the composer takes
// the focus, as the terminal's new session focuses its editor.
import { closeMain } from "../../lib/mainview.js";
import { focusField } from "../../lib/stores/composer.svelte.js";
import { newChat } from "../../lib/stores/sessions.svelte.js";

// A terminal or helper shown over the chat steps aside first.
export async function startNewChat() {
  closeMain();
  if (await newChat()) focusField();
}
