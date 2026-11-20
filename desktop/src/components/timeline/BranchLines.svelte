<script>
  // The branches made from a message, one line each under it (owner's pick
  // B, 2026-11-19): "↳ “the words…” → Branch title". The words themselves
  // stay plain, so the same words can open any number of branches. A click
  // on the title opens that branch (a closed one is reopened first).
  // Placed by UserBox and ReplyItem.
  import { linkTitle, openBranch } from "../../lib/plugins/branch.js";
  import { linksFor } from "../../lib/stores/links.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";

  let { messageId = "" } = $props();

  const QUOTE_MAX = 40;
  const list = $derived(messageId ? linksFor(messageId).map((d) => d.data) : []);

  // The words on one line, cut to QUOTE_MAX characters with "…".
  function quote(text) {
    const line = String(text ?? "").replace(/\s+/g, " ").trim();
    const chars = Array.from(line);
    return chars.length > QUOTE_MAX ? `${chars.slice(0, QUOTE_MAX - 1).join("")}…` : line;
  }

  const open = (sid) => openBranch(sid).catch((error) => console.error("opening a branch failed", error));
</script>

{#if list.length > 0}
  <div class="branches" data-branch-lines>
    {#each list as link (link.id)}
      <div class="line" data-link-id={link.id}>
        <span class="mark" aria-hidden="true">↳</span>
        <span class="quote">“{quote(link.highlight_text)}”</span>
        <span class="mark" aria-hidden="true">→</span>
        <button type="button" class="open" title={linkTitle(link.session_id)} data-session-id={link.session_id} onclick={() => open(link.session_id)}>
          {sessions.byId[link.session_id]?.title || "branch"}
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .branches {
    margin-top: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--mark);
    user-select: none;
  }
  .line {
    display: flex;
    align-items: baseline;
    gap: calc(6 * var(--px));
    min-width: 0;
    white-space: nowrap;
  }
  .quote {
    font-style: italic;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    flex: 0 1 auto;
  }
  .mark {
    flex: none;
  }
  .open {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    color: var(--link);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
    cursor: pointer;
  }
  .open:hover {
    text-decoration-thickness: max(1px, calc(1.5 * var(--px)));
  }
</style>
