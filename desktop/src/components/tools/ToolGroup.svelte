<script>
  // A folded tool run: one row that says how many tools ran and what they did.
  // Scaffold S3 moved this markup and CSS out of Conversation.svelte unchanged;
  // piece P3.6 draws the group {item} (a ToolGroupItem, lib/types.js).
  import Icon from "../Icon.svelte";

  let { item } = $props();
</script>

<button class="tool-row" type="button" aria-expanded="false">
  <Icon name="disclosure" />
  <span class="line"><span class="count">Ran {item.count} tools</span><span class="dot" aria-hidden="true"></span><span class="summary">{item.summary}</span></span>
</button>

<style>
  .tool-row {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    width: 100%;
    height: calc(44 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(21 * var(--px));
    border: 1px solid var(--rule-control);
    font: var(--t-body-small);
  }
  /* One line of text, so the dot can sit on the letters' middle and a long
     summary ends in an ellipsis. */
  .tool-row .line {
    flex: 1;
    min-width: 0;
    margin-left: calc(15 * var(--px));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    text-align: left;
  }
  .tool-row .count {
    letter-spacing: -0.01em;
  }
  /* vertical-align: middle centres the dot half an x-height above the
     baseline, read from the font itself, in every renderer and at every zoom. */
  .tool-row .dot {
    display: inline-block;
    vertical-align: middle;
    width: calc(4 * var(--px));
    height: calc(4 * var(--px));
    margin: 0 calc(10 * var(--px)) 0 calc(9 * var(--px));
    border-radius: 50%;
    background: var(--ink);
  }
  .tool-row .summary {
    color: var(--slate);
  }
</style>
