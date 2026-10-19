// Is this window busy (desktop-design-5-6 2.0 rule 2, 6.2; scaffold S5,
// complete): the shown chat replies, or a `!` command of it runs in this
// window. Read it in $derived or $effect; it follows the stores.
import { composer } from "./stores/composer.svelte.js";
import { busy, sessions } from "./stores/sessions.svelte.js";

export function windowBusy() {
  const sid = sessions.currentId;
  if (!sid) return false;
  if (busy(sid)) return true;
  return Object.values(composer.shells).some((shell) => shell.sessionId === sid && shell.running);
}
