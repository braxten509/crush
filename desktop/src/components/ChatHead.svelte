<script>
  // The middle column's head (desktop-design 2.1). On a chat: its title and
  // the path line, the project's folder and Git branch in the built path
  // style; on a branch, "BRANCHED FROM <parent title>" with the parent's
  // title as a link that opens it, reopening it first when it is closed (4.8,
  // 3.3; demo core.js:101). While a chat opens: that chat's title. On the
  // start screens (landing, init, onboarding): the CRUSH AI wordmark with
  // "Charm™ · v<version>" (START-003, START-004), in the landing block's
  // column. Nothing while the window is still deciding what to show, so no
  // wordmark flashes before a -s chat.
  //
  // The branch links over a message's words are drawn by the renderer as
  // span.md-deco-branch-link (lib/render/sourcemap.js); their look (4.8) is
  // set here, the one branching component every chat shows.
  import { isClosed, reopen } from "../lib/stores/closed.svelte.js";
  import { conn } from "../lib/stores/connection.svelte.js";
  import { parentOf } from "../lib/stores/links.svelte.js";
  import { openChat, sessions } from "../lib/stores/sessions.svelte.js";
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
  // A branch whose parent is still listed names it; a deleted parent took its
  // link along (engine 5.11), and the branch reads as any other chat.
  const parent = $derived.by(() => {
    if (view !== "chat" || !session?.id) return null;
    const id = parentOf(session.id);
    return id && sessions.byId[id] ? { id, title: sessions.byId[id].title || "New Session" } : null;
  });
  const path = $derived(view === "opening" || view === "chat" ? pathLine(baseName(conn.project.path), ws.branch) : "");

  $effect(() => {
    shown.head = brand
      ? { wordmark: hyper ? "HYPERCRUSH AI" : "CRUSH AI", charm: start.version ? `Charm™ · v${start.version}` : "Charm™" }
      : { title, path: parent ? `BRANCHED FROM ${parent.title}` : path, parent: parent?.id ?? "" };
    return () => {
      shown.head = null;
    };
  });

  async function openParent() {
    const id = parent?.id;
    if (!id) return;
    if (isClosed(id) && !(await reopen(id))) return;
    await openChat(id);
  }
</script>

<header class="head" class:brand>
  {#if brand}
    <div class="brand-column">
      <Wordmark {hyper} version={start.version} />
    </div>
  {:else}
    <h2 class="title">{title}</h2>
    {#if parent}
      <p class="path" aria-label="Path">
        Branched from <button class="parent" type="button" title="{isClosed(parent.id) ? 'Reopen' : 'Open'} {parent.title}" onclick={openParent}>{parent.title}</button>
      </p>
    {:else if path}<p class="path" aria-label="Path" title={conn.project.path}>{path}</p>{/if}
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
  /* The parent's title: the path line's own letters in the link look. */
  .parent {
    padding: 0;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    color: var(--link);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .parent:hover {
    text-decoration-thickness: max(1px, calc(1.5 * var(--px)));
  }

  /* A branch link over a message's words (4.8; as built, Conversation.svelte:181-186). */
  :global(.md-deco-branch-link) {
    color: var(--link);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
    cursor: pointer;
  }
  :global(.md-deco-branch-link:hover) {
    text-decoration-thickness: max(1px, calc(1.5 * var(--px)));
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
