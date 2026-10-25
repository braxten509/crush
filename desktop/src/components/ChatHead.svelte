<script>
  // The middle column's head (desktop-design 2.1). Drawn only on the init and
  // onboarding screens now: a chat's title, folder and branch are in the
  // window's top bar (system/TitleBar.svelte; owner, 2026-10-20). What it
  // would show on a chat is still reported (shown.head) for the checks.
  //
  // As it was: on a chat, its title and
  // the path line, the project's folder and Git branch in the built path
  // style; on a branch, "Branched from <parent title>" with the parent's
  // title as a link that opens it, reopening it first when it is closed (4.8,
  // 3.3; demo core.js:101). While a chat opens: that chat's title. On the
  // init and onboarding screens: the CRUSH AI wordmark with
  // "Charm™ · v<version>" (START-003, START-004), in the landing block's
  // column. Nothing while the window is still deciding what to show, so no
  // wordmark flashes before a -s chat.
  //
  // The branch links over a message's words are drawn by the renderer as
  // span.md-deco-branch-link (lib/render/sourcemap.js); their look (4.8) is
  // set here, the one branching component every chat shows.
  import { coderModel } from "../lib/models/store.svelte.js";
  import { conn } from "../lib/stores/connection.svelte.js";
  import { parentOf } from "../lib/stores/links.svelte.js";
  import { sessions } from "../lib/stores/sessions.svelte.js";
  import { ws } from "../lib/stores/workspace.svelte.js";
  import Wordmark from "./start/Wordmark.svelte";
  import { screen, start } from "./start/start.svelte.js";
  import { baseName, pathLine } from "./start/startmodel.js";
  import { shown } from "./start/shown.js";

  let { session } = $props();

  const view = $derived(screen());
  // The landing page is blank, wordmark included (owner, 2026-10-19).
  const brand = $derived(view === "init" || view === "onboarding");
  const hyper = $derived(coderModel().selected?.provider === "hyper");
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
      : { title, path: parent ? `Branched from ${parent.title}` : path, parent: parent?.id ?? "" };
    return () => {
      shown.head = null;
    };
  });

</script>

{#if brand}
  <header class="head brand">
    <div class="brand-column">
      <Wordmark {hyper} version={start.version} />
    </div>
  </header>
{/if}

<style>
  .head {
    padding: calc(17 * var(--px)) calc(30 * var(--px)) 0;
    height: calc(121 * var(--px));
    box-sizing: border-box;
    flex: none;
    min-width: 0;
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
