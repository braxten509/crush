<script>
  // About Crush Desktop (desktop-design-5-6 2.28, MISC-018, MISC-023): the
  // wordmark, the desktop's version, the engine's version with its commit and
  // platform, Crush's folders from `crush dirs` labelled Config, Data and
  // Project, the log file and the crash report folder. A folder opens in the
  // file manager on click.
  import { onMount } from "svelte";
  import Sheet, { section } from "../kit/Sheet.svelte";
  import Wordmark from "../start/Wordmark.svelte";
  import { api } from "../../lib/engine/api.js";
  import { app_info, crushDirs, open_link } from "../../lib/engine/commands.js";
  import { projectDir } from "../../lib/stores/composer.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "about";
  const LABELS = ["Config", "Data"];

  let desktop = $state("");
  let engine = $state(null);
  let folders = $state([]);
  let log = $state("");
  let crashes = $state("");
  let dirsError = $state("");

  const help = [{ key: "esc", text: "close" }];

  function open(path) {
    open_link(path, projectDir()).catch((error) => notify("error", error?.message ?? String(error)));
  }

  onMount(() => {
    app_info()
      .then((info) => (desktop = info?.version ?? ""))
      .catch(() => {});
    api
      .version()
      .then((v) => (engine = v))
      .catch(() => {});
    crushDirs()
      .then((dirs) => {
        folders = (dirs?.lines ?? []).map((path, i) => ({ label: LABELS[i] ?? "Project", path }));
        log = dirs?.log ?? "";
        crashes = dirs?.crashes ?? "";
      })
      .catch((error) => (dirsError = error?.message ?? String(error)));
  });
</script>

<Sheet id={ID} title="About Crush Desktop" width="standard" {help}>
  <div class="page">
    <div class="mark"><Wordmark /></div>
    <p class="line">Crush Desktop {desktop}</p>
    {#if engine}
      <p class="line">Crush {engine.version}</p>
      <p class="quiet">{[engine.commit, engine.platform].filter(Boolean).join(" · ")}</p>
    {/if}

    {@render section("Folders")}
    {#if dirsError}
      <p class="quiet">{dirsError}</p>
    {:else}
      {#each folders as folder, i (i)}
        <div class="row">
          <span class="name">{folder.label}</span>
          <button type="button" class="path" onclick={() => open(folder.path)}>{folder.path}</button>
        </div>
      {/each}
      {#if folders.length > 0}
        <p class="quiet">Configs merge from top to bottom</p>
      {/if}
      {#if log}
        <div class="row">
          <span class="name">Log file</span>
          <button type="button" class="path" onclick={() => open(log)}>{log}</button>
        </div>
      {/if}
      {#if crashes}
        <div class="row">
          <span class="name">Crash reports</span>
          <button type="button" class="path" onclick={() => open(crashes)}>{crashes}</button>
        </div>
      {/if}
    {/if}
  </div>
</Sheet>

<style>
  p {
    margin: 0;
  }
  .page {
    overflow-y: auto;
    min-height: 0;
  }
  .mark {
    margin-bottom: calc(16 * var(--px));
    transform: scale(0.5);
    transform-origin: left top;
    height: calc(48 * var(--px));
  }
  .line {
    font: var(--t-item);
    color: var(--ink);
  }
  .quiet {
    font: var(--t-small);
    color: var(--slate);
    margin-top: calc(4 * var(--px));
  }
  .row {
    display: flex;
    align-items: baseline;
    gap: calc(16 * var(--px));
    min-height: calc(32 * var(--px));
  }
  .name {
    flex: none;
    width: calc(120 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .path {
    min-width: 0;
    padding: 0;
    border: 0;
    background: none;
    font: var(--t-agent-mono);
    color: var(--link);
    text-align: left;
    overflow-wrap: anywhere;
    cursor: pointer;
  }
  .path:hover {
    text-decoration: underline;
  }
  .path:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
