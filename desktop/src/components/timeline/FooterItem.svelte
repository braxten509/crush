<script>
  // The line under a turn's final reply (CHAT-035 to CHAT-037): the diamond
  // drawn as a line icon, then "Model via Provider in 12s" (or "Model →
  // Routed" on a Prism turn) and any Prism savings, in --t-small slate on one
  // line cut with "…" (messages.go:385-426).
  // Placed by Timeline.
  import Icon from "../Icon.svelte";

  let { item } = $props();
</script>

<p class="footer">
  <Icon name="diamond" size={16} />
  <span class="line">{item.text}{#if item.saved}{" "}{item.saved}{/if}</span>
</p>

<style>
  .footer {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-width: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .line {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
