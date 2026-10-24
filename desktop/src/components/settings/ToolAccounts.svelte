<script>
  // Settings > Accounts, one tool's sign-ins (owner, 2026-10-21): each tool's
  // own login is account 1; more accounts each get a folder of their own that
  // shares account 1's settings, skills, tools and saved chats. "Add" makes
  // one and opens a terminal tab that signs it in. Every account, the tools'
  // own logins too, is renamed in place; one not renamed shows the email it
  // signed in with (owner, 2026-10-21). Removing asks once more on the row
  // itself.
  //   kind, name, ownLogin? (argv), ownSignedIn? (state read by the engine)
  import { onMount } from "svelte";
  import { addAccount, fallback, loadFallback, removeAccount, renameAccount, signInAccount } from "../../lib/fallback/store.svelte.js";
  import { sign_in_run } from "../../lib/engine/commands.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  let { kind, name, ownLogin = null, ownSignedIn = null } = $props();

  let renaming = $state(""); // "kind/id" of the row being renamed
  let draft = $state("");
  let removing = $state(""); // the account asked about
  let busy = $state(false);

  onMount(() => {
    if (!fallback.loaded) loadFallback();
  });

  const accounts = $derived(fallback.accounts.filter((a) => a.kind === kind));

  // Account 1's state comes from the CLI's own login when the engine read it.
  const signedIn = (a) => (a.id ? a.signed_in : (ownSignedIn ?? a.signed_in));

  function startRename(a) {
    renaming = `${a.kind}/${a.id}`;
    draft = a.name;
  }
  function finishRename(a, event) {
    if (event?.key && event.key !== "Enter" && event.key !== "Escape") return;
    if (event?.key === "Escape") {
      renaming = "";
      return;
    }
    renaming = "";
    // A tool's own login goes by the tool's name.
    if (draft.trim() !== a.name) renameAccount(a.id || a.kind, draft.trim());
  }

  // Account 1 signs in with the tool itself; the app runs that command with
  // no terminal, as it does for an added account (sign_in_run).
  async function signInOwn() {
    if (busy || !ownLogin?.length) return;
    busy = true;
    try {
      const ok = await sign_in_run(ownLogin);
      if (ok) notify("info", `Signed in to ${name}.`);
      else notify("error", `Signing in to ${name} didn't finish.`);
    } catch (error) {
      notify("error", `Couldn't start the sign-in: ${error?.message ?? String(error)}`);
    } finally {
      busy = false;
      await loadFallback();
    }
  }
</script>

{#each accounts as a (a.id)}
  <div class="row account" data-account={`${a.kind}/${a.id}`}>
    <span class="name">
      {#if renaming === `${a.kind}/${a.id}`}
        <!-- svelte-ignore a11y_autofocus -->
        <input class="rename" type="text" bind:value={draft} aria-label="Account name" autofocus onkeydown={(e) => finishRename(a, e)} onblur={() => finishRename(a)} />
      {:else}
        {a.name}<span class="tool">{" · "}{name}{a.id ? "" : ", its own login"}</span>
      {/if}
    </span>
    {#if removing === a.id && a.id}
      <span class="value">Remove {a.name}? Its sign-in is deleted.</span>
      <button type="button" class="act" onclick={() => ((removing = ""), removeAccount(a.id))}>Yes, remove</button>
      <button type="button" class="act" onclick={() => (removing = "")}>No</button>
    {:else}
      <span class="value">{signedIn(a) ? "Signed in" : "Not signed in"}</span>
      {#if a.id}
        <button type="button" class="act" onclick={() => signInAccount(a.id)}>{a.signed_in ? "Sign in again" : "Sign in"}</button>
        <button type="button" class="act" onclick={() => (removing = a.id)}>Remove</button>
      {:else if ownLogin}
        <button type="button" class="act" disabled={busy} onclick={signInOwn}>{signedIn(a) ? "Sign in again" : "Sign in"}</button>
      {/if}
      <!-- Last on every row, so it keeps one place (my-ui-design). -->
      <button type="button" class="act" onclick={() => startRename(a)}>Rename</button>
    {/if}
  </div>
{/each}
<div class="row">
  <span class="name quiet-name">Another {name} account</span>
  <button type="button" class="act" onclick={() => addAccount(kind)}>Add…</button>
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    min-height: calc(56 * var(--px));
    padding: calc(6 * var(--px)) 0;
    border-bottom: 1px solid var(--rule-control);
  }
  /* A tool's accounts and its "Another account" row stand together; the
     line comes after the group (owner, 2026-10-21). The clear line keeps
     every row the same height. */
  .account {
    border-bottom-color: transparent;
  }
  .name {
    flex: 1;
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
  }
  .tool,
  .quiet-name {
    color: var(--slate);
  }
  .value {
    font: var(--t-small);
    color: var(--slate);
  }
  .act {
    height: calc(36 * var(--px));
    padding: 0 calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .act:hover {
    background: var(--control-hover);
  }
  .act:disabled {
    color: var(--slate);
    cursor: default;
  }
  .rename {
    width: calc(220 * var(--px));
    height: calc(36 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    font: var(--t-field);
    color: var(--ink);
  }
  .act:focus-visible,
  .rename:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
