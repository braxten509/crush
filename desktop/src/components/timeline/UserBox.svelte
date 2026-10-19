<script>
  // Your text in the picture's box (CHAT-002, CHAT-004, CHAT-006, CHAT-007):
  // 1px --rule-faint, the full timeline width, wrapping (never a sideways
  // scroll). The text is drawn as typed: text nodes only, white-space:
  // pre-wrap, no Markdown, with its map back to the stored text
  // (render/usertext.js), so copy and phase 4's branch cut read it as they
  // read replies. The chips of the attachments follow under the text; a web
  // picture's chip is a link the timeline's guard opens after a click.
  // Placed by UserItem and PendingItem.
  import { untrack } from "svelte";
  import Chip from "../render/Chip.svelte";
  import { decorationHandler, decorationsFor } from "../../lib/decorations.js";
  import { safeHref } from "../../lib/render/links.js";
  import { sourceMapOf } from "../../lib/render/sourcemap.js";
  import { userTextInto } from "../../lib/render/usertext.js";

  let { text, chips = [], messageId = "" } = $props();

  let body;

  $effect(() => {
    const stored = text;
    const decorations = messageId ? decorationsFor(messageId) : [];
    untrack(() => userTextInto(body, stored, { decorations }));
  });

  // A click on a decoration (a branch link) goes to its kind's handler; a
  // click that ends a selection does not.
  function onclick(event) {
    const span = event.target.closest?.(".md-deco");
    if (!span || !body?.contains(span)) return;
    if (body.ownerDocument.getSelection?.()?.isCollapsed === false) return;
    const decoration = sourceMapOf(body)?.decorations[Number(span.dataset.deco)];
    if (decoration) decorationHandler(decoration.kind)?.(decoration, event);
  }

  // A picture the page cannot show (a web address): its chip opens the browser.
  const webLink = (chip) => (chip.kind === "picture" && !chip.viewable && /^https?:/i.test(chip.src ?? "") ? safeHref(chip.src) : null);
</script>

<div class="box">
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="text" bind:this={body} {onclick}></div>
  {#if chips.length > 0}
    <div class="chips">
      {#each chips as chip, i (i)}
        {@const href = webLink(chip)}
        {#if href}
          <a class="web" {href} draggable="false" rel="noreferrer" title={href}><Chip {chip} removable={false} /></a>
        {:else}
          <Chip {chip} removable={false} />
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .box {
    border: 1px solid var(--rule-faint);
    padding: calc(17 * var(--px)) calc(12 * var(--px)) calc(12 * var(--px));
    min-width: 0;
  }
  .text {
    font: var(--t-reading);
    color: var(--ink);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .text::selection,
  .text :global(::selection) {
    background: var(--selection);
  }
  .text:empty {
    display: none;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: calc(8 * var(--px));
  }
  .text:not(:empty) + .chips {
    margin-top: calc(12 * var(--px));
  }
  .web {
    display: inline-flex;
    max-width: 100%;
    color: inherit;
    text-decoration: none;
  }
</style>
