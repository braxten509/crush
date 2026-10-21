<script>
  // The one mode mark at the field's left (TYPE-002, SIDE-025; 4.9): plan is
  // "PLAN" in small caps, bang a mono "!"; none in normal or YOLO mode (owner,
  // 2026-10-19: always in YOLO, Shift+Tab changes it, so no mark for it).
  // REMOTE is no longer here: it has its line under the typing box (owner,
  // 2026-10-21). The box is the first line's height, so the mark sits
  // centred on that line.
  import Icon from "../Icon.svelte";

  let { mode } = $props();
</script>

{#if mode === "plan" || mode === "bang"}
  <span class="marks">
    {#if mode === "plan"}
      <span class="mark plan" data-mark="plan">Plan</span>
    {:else}
      <span class="mark bang" data-mark="bang" aria-label="Shell command">!</span>
    {/if}
  </span>
{/if}

<style>
  .marks {
    flex: none;
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    height: calc(26 * var(--px));
    margin: calc(16 * var(--px)) 0 0 calc(14 * var(--px));
  }
  .mark {
    display: flex;
    align-items: center;
    color: var(--ink);
  }
  .plan {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .bang {
    font: var(--t-key);
  }
</style>
