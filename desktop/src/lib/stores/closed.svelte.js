// Closed chats (desktop-design 3.3), kept per engine data folder in the
// desktop settings file. Scaffold S3: the final exports with empty data;
// piece P4.1 fills them. The ids are a SvelteSet: $state does not track a
// plain Set's add and delete.
import { SvelteSet } from "svelte/reactivity";

export const closed = $state({ ids: new SvelteSet() }); // from settings.closed_chats[engine data_dir]

// closed_set(project, id, true)
export async function close(sessionId) {}

// closed_set(project, id, false)
export async function reopen(sessionId) {}

export function isClosed(sessionId) {
  return false;
}
