<script>
  // The open list's "‹‹" (desktop-design-5-6 2.20, LIST-002; the terminal's
  // collapse label at the right end of the list's top row,
  // internal/ui/sessionhost/draw.go:299-307): it switches to the narrow strip,
  // as Alt+S does. Registered in the list's "header" place, it sits at the
  // right end of the wordmark's row. Mounting it starts the page-width watch
  // that forces the strip in a narrow window.
  import { onMount } from "svelte";
  import { toggleList, watchWidth } from "../../lib/leftlist/store.svelte.js";
  import Icon from "../Icon.svelte";

  let button = $state(null);

  onMount(() => watchWidth(button));
</script>

<button bind:this={button} class="collapse" type="button" aria-label="Collapse the list" title="Collapse the list (Alt+S)" onclick={toggleList}>
  <!-- Two down chevrons stacked, turned a quarter: "‹‹". -->
  <span class="pair">
    <Icon name="chevron" />
    <Icon name="chevron" />
  </span>
</button>

<style>
  /* The wordmark's row runs 27 to 67 at 12/12; the control is centred on it,
     its right edge 20 in from the column's, where the list's text ends. */
  .collapse {
    position: absolute;
    top: calc(31 * var(--px));
    right: calc(20 * var(--px));
    display: grid;
    place-items: center;
    width: calc(32 * var(--px));
    height: calc(32 * var(--px));
    padding: 0;
    color: var(--slate);
  }
  .collapse:hover {
    color: var(--ink);
  }
  .collapse:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .pair {
    display: flex;
    flex-direction: column;
    gap: calc(3 * var(--px));
    transform: rotate(90deg);
  }
</style>
