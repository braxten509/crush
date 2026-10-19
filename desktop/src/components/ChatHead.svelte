<script>
  // The middle column's head (desktop-design 2.1). On a chat: its title and
  // the path line, the project's folder and Git branch in the built path
  // style (phase 4 adds "BRANCHED FROM <parent>" for branches, 4.8). While a
  // chat opens: that chat's title. On the start screens (landing, init,
  // onboarding): the CRUSH AI wordmark with "Charm™ · v<version>" (START-003,
  // START-004), in the landing block's column. Nothing while the window is
  // still deciding what to show, so no wordmark flashes before a -s chat.
  import { conn } from "../lib/stores/connection.svelte.js";
  import { sessions } from "../lib/stores/sessions.svelte.js";
  import { ws } from "../lib/stores/workspace.svelte.js";
  import Wordmark from "./start/Wordmark.svelte";
  import { screen, start } from "./start/start.svelte.js";
  import { baseName, pathLine } from "./start/startmodel.js";
  import { shown } from "./start/shown.js";

  let { session } = $props();

  const view = $derived(screen());
  const brand = $derived(view === "landing" || view === "init" || view === "onboarding");
  const hyper = $derived(ws.config?.models?.large?.provider === "hyper");
  const title = $derived(view === "opening" ? (sessions.opening?.title ?? "") : view === "chat" ? (session?.title ?? "") : "");
  const path = $derived(view === "opening" || view === "chat" ? pathLine(baseName(conn.project.path), ws.branch) : "");

  $effect(() => {
    shown.head = brand ? { wordmark: hyper ? "HYPERCRUSH AI" : "CRUSH AI", charm: start.version ? `Charm™ · v${start.version}` : "Charm™" } : { title, path };
    return () => {
      shown.head = null;
    };
  });
</script>

<header class="head" class:brand>
  {#if brand}
    <div class="brand-column">
      <Wordmark {hyper} version={start.version} />
    </div>
  {:else}
    <h2 class="title">{title}</h2>
    {#if path}<p class="path" aria-label="Path" title={conn.project.path}>{path}</p>{/if}
  {/if}
</header>

<style>
  .head {
    padding: calc(17 * var(--px)) calc(30 * var(--px)) 0;
    height: calc(121 * var(--px));
    box-sizing: border-box;
    flex: none;
    min-width: 0;
  }
  .title {
    min-height: calc(50 * var(--px));
    font: var(--t-title);
    letter-spacing: -0.02em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    margin-top: calc(11 * var(--px));
    font: var(--t-path);
    letter-spacing: var(--track-path);
    text-transform: uppercase;
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* The wordmark shares the landing block's left edge (Landing.svelte: at
     most 720 wide and centred, at least 60 from the column's edges). */
  .head.brand {
    padding-left: 0;
    padding-right: 0;
  }
  .brand-column {
    box-sizing: border-box;
    width: 100%;
    max-width: calc(840 * var(--px));
    margin: 0 auto;
    padding: 0 calc(60 * var(--px));
    overflow: hidden;
  }
</style>
