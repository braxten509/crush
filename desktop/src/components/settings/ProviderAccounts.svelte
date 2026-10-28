<script>
  // Settings → Accounts (owner, 2026-10-24): a left bar of the providers the
  // model picker offers — the agent CLIs installed here, then every API
  // provider — with the chosen one's detail on the right, the same left-bar
  // look as Fallback order's "Starts on". Claude Code, Codex and Grok keep
  // account rows (ToolAccounts) and "Another account"; Antigravity, OpenCode
  // Go and Zen offer their own CLI's sign-in; an API provider holds an API
  // key, and another account is a provider entry of its own (engine
  // AddAccount). A row is just a short label and its actions (owner,
  // 2026-10-24: no subtext and no Signed in / API key set words).
  import { onMount } from "svelte";
  import { accountGroups, cliRowLabel } from "../../lib/accounts/providers.js";
  import { cliState, loadCLIState } from "../../lib/accounts/state.svelte.js";
  import { openDialog } from "../../lib/dialogs.js";
  import { api, SCOPE } from "../../lib/engine/api.js";
  import { sign_in_run } from "../../lib/engine/commands.js";
  import { fallback, loadFallback } from "../../lib/fallback/store.svelte.js";
  import { modelData, refreshModels } from "../../lib/models/store.svelte.js";
  import { projectDir } from "../../lib/stores/composer.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";
  import { openTerminal } from "../../lib/terminal/tabs.svelte.js";
  import ToolAccounts from "./ToolAccounts.svelte";

  let selected = $state(""); // the provider shown on the right
  let removing = $state(""); // the API provider whose key is asked about
  let busy = $state(""); // the CLI whose sign-in runs

  const groups = $derived(accountGroups(modelData.cfg, modelData.catalog));
  const rows = $derived(groups.flatMap((group) => group.rows));
  const current = $derived(rows.find((row) => row.id === selected) ?? null);

  // The first provider shows until another is picked; one that went away (a
  // copy removed from the config) gives way the same way.
  $effect(() => {
    if (!current && rows.length) selected = rows[0].id;
  });

  onMount(() => {
    if (!fallback.loaded) loadFallback();
    loadCLIState();
  });

  const errorText = (error) => error?.message ?? String(error);

  // An API provider's stored credential, as the model picker reads it.
  const stored = (id) => {
    const provider = modelData.cfg?.providers.get(id);
    return Boolean(provider && (provider.has_oauth || provider.has_api_key));
  };

  // The engine's word on the CLI's own login, else the stored credential.
  const cliSignedIn = (row) => cliState.providers[row.id] === true || stored(row.id);

  function openKey(row, providerId = row.id, providerName = row.name) {
    openDialog(
      "api_key_input",
      {
        providerId,
        providerName,
        onDone: (result) => {
          if (result === "done") refreshModels();
          loadCLIState();
        },
      },
      { grace: true },
    );
  }

  async function removeKey(row) {
    removing = "";
    try {
      await api.removeConfig(SCOPE.global, `providers.${row.id}.api_key`);
      await refreshModels();
      notify("info", `Removed the ${row.name} key.`);
    } catch (error) {
      notify("error", `Couldn't remove the key: ${errorText(error)}`);
    }
  }

  // Another account of an API provider: the engine makes a provider entry
  // that copies this one without its key, and the key sheet fills it in.
  async function addProviderAccount(row) {
    try {
      const login = await api.addAccount(row.id);
      const account = login?.account;
      if (!account?.id) throw new Error("no account came back");
      await refreshModels();
      openKey(row, account.id, account.name || account.id);
    } catch (error) {
      notify("error", `Couldn't add the account: ${errorText(error)}`);
    }
  }

  // A CLI that signs itself in: Grok finishes in the browser with no
  // terminal; Antigravity and OpenCode ask their questions in a tab, and the
  // state is read again when it closes.
  async function signInCLI(row) {
    if (busy) return;
    if (row.cli.terminal) {
      const tab = openTerminal({ cwd: projectDir(), argv: row.cli.login, title: `Sign in to ${row.name}` });
      tab.exited.then(() => loadCLIState());
      return;
    }
    busy = row.id;
    try {
      const ok = await sign_in_run(row.cli.login);
      await loadCLIState();
      if (ok) notify("info", `Signed in to ${row.name}.`);
      else notify("error", `Signing in to ${row.name} didn't finish.`);
    } catch (error) {
      notify("error", `Couldn't start the sign-in: ${errorText(error)}`);
    } finally {
      busy = "";
    }
  }
</script>

<div class="accounts" data-probe="provider-accounts">
  <nav class="providers" aria-label="Providers" data-probe="accounts-providers">
    {#each groups as group (group.label)}
      {#if group.rows.length}
        <span class="providers-head">{group.label}</span>
        {#each group.rows as row (row.id)}
          <button
            type="button"
            class="provider"
            class:on={row.id === selected}
            aria-current={row.id === selected ? "true" : undefined}
            data-provider={row.id}
            onclick={() => ((selected = row.id), (removing = ""))}>{row.name}</button
          >
        {/each}
      {/if}
    {/each}
  </nav>

  <div class="detail">
    {#if current}
      <h3 class="detail-name">{current.name}</h3>
      {#if current.cli}
        {#if current.cli.accounts}
          <ToolAccounts
            kind={current.id}
            name={current.name}
            ownLogin={current.id === "grok-cli" ? current.cli.login : null}
            ownSignedIn={current.id === "grok-cli" ? cliSignedIn(current) : null}
          />
        {:else}
          <div class="row">
            <span class="name">{cliRowLabel(current, cliSignedIn(current), cliState.emails[current.id])}</span>
            <button type="button" class="act" disabled={busy === current.id} onclick={() => signInCLI(current)}>{cliSignedIn(current) ? "Sign in again" : "Sign in…"}</button>
          </div>
        {/if}
      {:else}
        {#if removing === current.id}
          <div class="row">
            <span class="name">Remove the {current.name} key?</span>
            <span class="value">It is deleted from the global config.</span>
            <button type="button" class="act" onclick={() => removeKey(current)}>Yes, remove</button>
            <button type="button" class="act" onclick={() => (removing = "")}>No</button>
          </div>
        {:else}
          <div class="row">
            <span class="name">API key</span>
            <button type="button" class="act" onclick={() => openKey(current)}>{current.hasKey || current.hasOAuth ? "Change…" : "Add key…"}</button>
            {#if current.hasKey}<button type="button" class="act" onclick={() => (removing = current.id)}>Remove</button>{/if}
          </div>
        {/if}
        {#if current.configured}
          <div class="row">
            <span class="name quiet-name">Another {current.name} account</span>
            <button type="button" class="act" onclick={() => addProviderAccount(current)}>Add…</button>
          </div>
        {/if}
      {/if}
    {:else}
      <p class="quiet">No providers are installed or configured.</p>
    {/if}
  </div>
</div>

<style>
  /* The Fallback order's two panes: a left bar of names under their group
     headings, the chosen one's detail on the right. The bar shares Fallback
     order's width and never scrolls on its own; the page does (owner,
     2026-10-24). */
  .accounts {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
  }
  .providers {
    display: flex;
    flex-direction: column;
    /* The width Fallback order's left bar has (owner, 2026-10-24). */
    min-width: calc(210 * var(--px));
    padding: calc(14 * var(--px)) calc(12 * var(--px)) calc(16 * var(--px)) 0;
    border-right: 1px solid var(--rule-faint);
  }
  .providers-head {
    padding: calc(14 * var(--px)) calc(10 * var(--px)) calc(6 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .providers-head:first-child {
    padding-top: calc(4 * var(--px));
  }
  .provider {
    flex: none;
    padding: calc(8 * var(--px)) calc(10 * var(--px));
    border: 0;
    background: none;
    font: var(--t-item);
    color: var(--ink);
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }
  .provider:hover {
    background: var(--control-hover);
  }
  .provider.on {
    background: var(--band);
    box-shadow: inset 2px 0 var(--vermilion);
  }
  .provider:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(-1 * var(--px));
  }
  .detail {
    min-width: 0;
    padding-left: calc(20 * var(--px));
  }
  .detail-name {
    margin: calc(14 * var(--px)) 0 calc(6 * var(--px));
    font: var(--t-item);
    font-weight: 600;
    color: var(--ink);
  }
  .row {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    min-height: calc(56 * var(--px));
    padding: calc(6 * var(--px)) 0;
    border-bottom: 1px solid var(--rule-control);
  }
  .name {
    flex: 1;
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
  }
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
  .act:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .quiet {
    margin: calc(12 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
</style>
