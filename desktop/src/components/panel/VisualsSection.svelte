<script module>
  // Shown while the chat has no figure yet (4.3 names the demo's empty text,
  // core.js:157, which this piece's sources do not include; these words say
  // the same thing in the panel's voice).
  export const VISUALS_EMPTY = "Tables, diagrams, math and pictures from this chat show here.";
</script>

<script>
  // VISUALS (4.3, CHAT-014): the shown chat's figures in chat order
  // (visuals.svelte.js), each a FigureView: no box, its heading, its body, and
  // the caret rule between figures as the approved picture draws them. A
  // chip's click (revealFigure) scrolls the panel body to the figure once it
  // is there, and the figure carries a 2 px ink bar at its left for 1.2 s.
  import { untrack } from "svelte";
  import { figuresFor, reveal, visuals } from "../../lib/stores/visuals.svelte.js";
  import FigureView from "../render/FigureView.svelte";
  import Rule from "../Rule.svelte";

  let { sessionId, scrollTo } = $props();

  const figures = $derived(figuresFor(sessionId));
  let root = $state();
  // The reveal seq last carried out: a panel shown again does not replay the
  // reveal made before it was hidden.
  let shown = reveal.seq;

  $effect(() => {
    const seq = reveal.seq;
    const id = reveal.id;
    figures; // a figure that arrives later still gets its reveal
    if (!seq || seq === shown || !root) return;
    const target = [...root.querySelectorAll("[data-figure-id]")].find((el) => el.dataset.figureId === id);
    if (!target) return;
    shown = seq;
    untrack(() => scrollTo?.(target));
  });
</script>

<section class="visuals" aria-labelledby="visuals-label" bind:this={root}>
  <h2 class="label" id="visuals-label">Visuals</h2>
  {#if figures.length === 0}
    <p class="empty">{VISUALS_EMPTY}</p>
  {:else}
    {#each figures as figure, i (figure.id)}
      {#if i > 0}<Rule caret={26} class="figure-rule" />{/if}
      <div class="figure" class:first={i === 0} class:marked={visuals.marked === figure.id} data-figure-id={figure.id}>
        <FigureView {figure} />
      </div>
    {/each}
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
  .figure {
    position: relative;
    min-width: 0;
    margin-top: calc(19 * var(--px));
  }
  .figure.first {
    margin-top: calc(29 * var(--px));
  }
  .visuals :global(.rule.figure-rule) {
    margin: calc(37 * var(--px)) 0 0 calc(-1 * var(--px));
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
