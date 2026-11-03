<script>
  // A locked chat (owner, 2026-11-03): its messages live only in the Plasma
  // Vault, so the chat shows this page in place of its timeline and
  // composer. Unlock asks KDE for the vault password (KDE's own box; the
  // password never passes through Crush) and the chat comes back. Under it,
  // a quiet link turns the chat into a normal chat for good, after the same
  // password (owner, 2026-11-03, sketch stop.png).
  //
  // States: locking ("Locking this chat…"), locked, unlocking ("Waiting for
  // the vault…"), relock (a crash left a plain copy outside; it stays hidden
  // until it is back in the vault, and Unlock puts it back first).
  import Icon from "./Icon.svelte";
  import { unlockChat } from "../lib/stores/sessions.svelte.js";

  let { session } = $props();

  const state = $derived(session?.lock_state ?? "");
  const waiting = $derived(state === "locking" || state === "unlocking");
  let asked = $state(false);

  async function unlock(stop = false) {
    if (!session?.id || waiting) return;
    asked = true;
    try {
      await unlockChat(session.id, { stop });
    } finally {
      asked = false;
    }
  }
</script>

<section class="locked" aria-label="Locked chat" data-locked-page={state}>
  <div class="body">
    <Icon name="lock" size={28} />
    <p class="headline">{state === "locking" ? "Locking this chat…" : "This chat is locked."}</p>
    {#if state === "relock"}
      <p class="note warn">It is not back in the vault yet; unlocking puts it back first.</p>
    {/if}
    {#if state !== "locking"}
      <button type="button" class="unlock" disabled={waiting || asked} onclick={() => unlock(false)} data-unlock>
        <Icon name="lock-open" />
        <span>{state === "unlocking" ? "Waiting for the vault…" : "Unlock"}</span>
      </button>
      <p class="note">KDE asks for the vault password</p>
      <button type="button" class="stop" disabled={waiting || asked} onclick={() => unlock(true)} data-stop-locking>Stop locking this chat</button>
    {/if}
    <!-- The first lock opens KDE's vault wizard (PLAN §3). -->
    <details class="setup">
      <summary>First lock? Make the vault in KDE's window</summary>
      <ol>
        <li>Name: <b>Crush locked chats</b></li>
        <li>Backend: <b>gocryptfs</b></li>
        <li>Mount point: <b>~/.local/share/crush/locked-chats</b></li>
        <li>Leave “Offline” off and don't limit it to activities</li>
      </ol>
    </details>
  </div>
</section>

<style>
  /* Over the timelines' box (Conversation .middle), on the paper. */
  .locked {
    position: absolute;
    inset: 0;
    z-index: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--paper);
    color: var(--ink);
  }
  .body {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: calc(12 * var(--px));
    max-width: calc(560 * var(--px));
    padding: 0 calc(23 * var(--px));
    text-align: center;
  }
  .headline {
    margin: 0;
    font: var(--t-heading);
  }
  .note {
    margin: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .note.warn {
    color: var(--amber-ink);
  }
  .unlock {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    box-sizing: border-box;
    height: calc(36 * var(--px));
    margin-top: calc(8 * var(--px));
    padding: 0 calc(16 * var(--px));
    border: 1px solid var(--ink);
    background: var(--band);
    font: var(--t-button);
    color: var(--ink);
  }
  .unlock:disabled,
  .stop:disabled {
    opacity: 0.5;
  }
  /* Quiet: text only, under the note. */
  .stop {
    padding: 0;
    border: 0;
    background: none;
    font: var(--t-small);
    color: var(--slate);
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }
  .stop:hover:not(:disabled) {
    color: var(--ink);
  }
  .setup {
    margin-top: calc(24 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    text-align: left;
  }
  .setup summary {
    cursor: pointer;
  }
  .setup ol {
    margin: calc(8 * var(--px)) 0 0;
    padding-left: calc(24 * var(--px));
  }
</style>
