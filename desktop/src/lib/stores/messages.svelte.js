// Each loaded chat's messages (desktop-design 1.5.3). Scaffold S3: the pinned
// exports with empty bodies; piece P3.3 writes them.
import { SvelteMap } from "svelte/reactivity";

// One per loaded chat; a class instance, so no store proxy wraps it.
export class ChatMessages {
  status = $state("idle"); // "idle" | "loading" | "ready" | "error"
  error = $state(null);
  ids = $state.raw([]); // message ids in chat order; replaced as a whole, never pushed into
  byId = new SvelteMap(); // mid → Message exactly as sent; replaced by key, never changed in place
}

export const messages = $state({ bySession: {} }); // sid → ChatMessages

// GET messages; buffers events meanwhile; replays (LIST-036).
export async function load(sid) {}

export function applyMessage(event, m) {}
