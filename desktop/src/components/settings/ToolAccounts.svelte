<script>
  // Settings > Accounts, the Claude Code and Codex part (owner, 2026-10-21):
  // each tool's own login is account 1; more accounts each get a folder of
  // their own that shares account 1's settings, skills, tools and saved
  // chats. "Add" makes one and opens a terminal tab that signs it in. Every
  // account, the tools' own logins too, is renamed in place; one not renamed
  // shows the email it signed in with (owner, 2026-10-21). Removing asks once
  // more on the row itself. Lines part the tools, not a tool's own rows.
  import { onMount } from "svelte";
  import { addAccount, fallback, loadFallback, removeAccount, renameAccount, signInAccount } from "../../lib/fallback/store.svelte.js";
  import { CLAUDE, CODEX } from "../../lib/fallback/blocks.js";

  const TOOLS = [
    { kind: CLAUDE, name: "Claude Code" },
    { kind: CODEX, name: "Codex" },
  ];

  let renaming = $state(""); // "kind/id" of the row being renamed
  let draft = $state("");
  let removing = $state(""); // the account asked about

  onMount(() => {
    if (!fallback.loaded) loadFallback();
  });

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
</script>

{#each TOOLS as tool (tool.kind)}
  {#each fallback.accounts.filter((a) => a.kind === tool.kind) as a (a.id)}
    <div class="row account" data-account={`${a.kind}/${a.id}`}>
      <span class="name">
        {#if renaming === `${a.kind}/${a.id}`}
          <!-- svelte-ignore a11y_autofocus -->
          <input class="rename" type="text" bind:value={draft} aria-label="Account name" autofocus onkeydown={(e) => finishRename(a, e)} onblur={() => finishRename(a)} />
        {:else}
          {a.name}<span class="tool">{" · "}{tool.name}{a.id ? "" : ", its own login"}</span>
        {/if}
      </span>
      {#if removing === a.id && a.id}
        <span class="value">Remove {a.name}? Its sign-in is deleted.</span>
        <button type="button" class="act" onclick={() => ((removing = ""), removeAccount(a.id))}>Yes, remove</button>
        <button type="button" class="act" onclick={() => (removing = "")}>No</button>
      {:else}
        <span class="value">{a.signed_in ? "Signed in" : "Not signed in"}</span>
        {#if a.id}
          <button type="button" class="act" onclick={() => signInAccount(a.id)}>{a.signed_in ? "Sign in again" : "Sign in"}</button>
          <button type="button" class="act" onclick={() => (removing = a.id)}>Remove</button>
        {/if}
        <!-- Last on every row, so it keeps one place (my-ui-design). -->
        <button type="button" class="act" onclick={() => startRename(a)}>Rename</button>
      {/if}
    </div>
  {/each}
  <div class="row">
    <span class="name quiet-name">Another {tool.name} account</span>
    <button type="button" class="act" onclick={() => addAccount(tool.kind)}>Add…</button>
  </div>
{/each}

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
