// Unread chats (desktop-design-5-6 2.20, LIST-012's phase 6 part): a chat
// whose work ended while this window did not show it waits as "Done" until it
// is shown or works again, as the session host's statusChanged and show
// (internal/ui/sessionhost/host.go:381-401, :353). The set is per window.
import { SvelteSet } from "svelte/reactivity";

export const unread = new SvelteSet();

const wasWorking = new Map(); // sid → working or background at the last look

// One look at a chat: kind is phase 3's state ("waiting", "working",
// "background") or "" when it is ready; shown says this window shows it.
export function noteState(id, kind, shown) {
  const working = kind === "working" || kind === "background";
  if (wasWorking.get(id) === true && !working && kind === "" && !shown) unread.add(id);
  if (working || shown) unread.delete(id);
  wasWorking.set(id, working);
}

export function forget(id) {
  unread.delete(id);
  wasWorking.delete(id);
}
