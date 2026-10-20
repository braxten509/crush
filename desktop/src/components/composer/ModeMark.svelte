<script>
  // The one mode mark at the field's left (TYPE-002, SIDE-025; 4.9): plan is
  // "PLAN" in small caps, bang a mono "!"; none in normal or YOLO mode (owner,
  // 2026-10-19: always in YOLO, Shift+Tab changes it, so no mark for it). Phase 6's badges (the Remote badge) follow it. The box is
  // the first line's height, so the mark sits centred on that line.
  import Icon from "../Icon.svelte";

  let { mode, badges = [] } = $props();
</script>

{#if mode === "plan" || mode === "bang" || badges.length > 0}
  <span class="marks">
    {#if mode === "plan"}
      <span class="mark plan" data-mark="plan">Plan</span>
    {:else if mode === "bang"}
      <span class="mark bang" data-mark="bang" aria-label="Shell command">!</span>
    {/if}
    {#each badges as badge (badge.id)}
      <span class="badge" data-badge={badge.id}>{badge.text}</span>
    {/each}
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
  .plan,
  .badge {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .badge {
    color: var(--slate);
  }
  .bang {
    font: var(--t-author);
  }
</style>
