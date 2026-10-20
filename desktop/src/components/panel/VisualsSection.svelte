<script module>
  // Shown while the chat has no figure yet (4.3 names the demo's empty text,
  // core.js:157, which this piece's sources do not include; these words say
  // the same thing in the panel's voice).
  export const VISUALS_EMPTY = "Tables, diagrams, math and pictures from this chat show here.";
</script>

<script>
  // VISUALS (4.3, CHAT-014): the selected figure of the shown chat
  // (visuals.svelte.js: the last chip clicked, else the newest; owner,
  // 2026-10-19), a FigureView: no box, its heading, its body. A chip's click
  // (revealFigure) selects it, scrolls the panel body to it once it is there,
  // and the figure carries a 2 px ink bar at its left for 1.2 s.
  import { untrack } from "svelte";
  import { reveal, selectedFigure, visuals } from "../../lib/stores/visuals.svelte.js";
  import FigureView from "../render/FigureView.svelte";

  let { sessionId, scrollTo } = $props();

  const figure = $derived(selectedFigure(sessionId));
  let root = $state();
  // The reveal seq last carried out: a panel shown again does not replay the
  // reveal made before it was hidden.
  let shown = reveal.seq;

  $effect(() => {
    const seq = reveal.seq;
    const id = reveal.id;
    figure; // a figure that arrives later still gets its reveal
    if (!seq || seq === shown || !root) return;
    const target = [...root.querySelectorAll("[data-figure-id]")].find((el) => el.dataset.figureId === id);
    if (!target) return;
    shown = seq;
    untrack(() => scrollTo?.(target));
  });
</script>

<section class="visuals" aria-labelledby="visuals-label" bind:this={root}>
  <h2 class="label" id="visuals-label">Visuals</h2>
  {#if !figure}
    <p class="empty">{VISUALS_EMPTY}</p>
  {:else}
    {#key figure.id}
      <div class="figure first" class:marked={visuals.marked === figure.id} data-figure-id={figure.id}>
        <FigureView {figure} />
      </div>
    {/key}
  {/if}
</section>

<style>
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .empty {
    margin-top: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  /* The picture's spacing: the first heading 29 below the label, 37 from a
     figure to the caret rule, 19 from the rule to the next heading. */
  /* A newly selected figure unrolls in its place (the paper motion). */
  .figure {
    animation: paper-unfold var(--motion) var(--ease-paper);
    position: relative;
    min-width: 0;
    margin-top: calc(19 * var(--px));
  }
  .figure.first {
    margin-top: calc(29 * var(--px));
  }
  /* The reveal mark: an ink bar in the gutter, beside the figure (4.3). */
  .figure.marked::before {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    left: calc(-14 * var(--px));
    width: calc(2 * var(--px));
    background: var(--ink);
  }
</style>
