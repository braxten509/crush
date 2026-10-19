<script>
  // A visual in the chat where its block was (desktop-design 4.3): the kind in
  // small caps, the tool row's dot, then the title. Click or Enter shows the
  // figure in VISUALS (revealFigure); a chip still streaming has no figure yet
  // and does nothing. A web picture is a link chip with its host name: the
  // link guard of the surface around it (links.js) opens the browser only
  // after a real click. The look is markdown.css's .md-chip, shared with the
  // renderer's own chip. Placed by MarkdownView; FigureView for web pictures.
  import { KIND_LABEL } from "../../lib/render/figures.js";
  import { revealFigure } from "../../lib/stores/visuals.svelte.js";

  let { figureId = null, kind, title, href = null, host = null, inline = false } = $props();

  function reveal() {
    if (figureId) revealFigure(figureId);
  }
</script>

{#snippet line()}
  <span class="md-chip-line"
    ><span class="md-chip-kind">{KIND_LABEL[kind]}</span><span class="md-chip-dotbox"><span class="md-chip-dot"></span></span><span
      class="md-chip-title">{host ?? title}</span
    ></span
  >
{/snippet}

{#if href}
  <a class="md-chip" class:md-chip-inline={inline} {href} draggable="false" rel="noreferrer" data-kind={kind} data-figure={figureId ?? undefined}
    >{@render line()}</a
  >
{:else}
  <button type="button" class="md-chip" class:md-chip-inline={inline} data-kind={kind} data-figure={figureId ?? undefined} onclick={reveal}
    >{@render line()}</button
  >
{/if}
