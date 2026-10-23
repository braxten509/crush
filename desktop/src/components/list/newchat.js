// LIST-033: the New chat button, Ctrl+N and the "New Session" action. The
// empty chat is saved immediately, then the composer takes the focus.
import { closeMain } from "../../lib/mainview.js";
import { focusField } from "../../lib/stores/composer.svelte.js";
import { newChat } from "../../lib/stores/sessions.svelte.js";

// A terminal or helper shown over the chat steps aside first.
export async function startNewChat() {
  closeMain();
  if (await newChat()) focusField();
}
