<script>
  // One timeline item's frame (desktop-design 1.5.6, 4.4; CHAT-003, CHAT-016,
  // CHAT-054): the selection bar (calc(2 * var(--px)) of ink at
  // calc(56 * var(--px)) from the column's left, the item's full height; never
  // vermilion, which the rail uses), the vermilion key on the rail, and the
  // keyboard focus around the item, which comes as `children`. The frame is
  // the feed's article; the item that holds the timeline's one tab stop
  // (`focused`) has tabindex 0, the rest -1.
  // Placed by Timeline.
  let { item, selected, focused, children } = $props();
</script>

<!-- The feed pattern makes each article focusable (WAI-ARIA APG, Feed). -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  class="frame"
  role="article"
  data-key={item.key}
  data-kind={item.kind}
  data-selected={selected ? "true" : undefined}
  aria-current={selected ? "true" : undefined}
  tabindex={focused ? 0 : -1}
>
  {#if selected}<span class="bar" aria-hidden="true"></span>{/if}
  {#if item.letter}<span class="key" aria-label="Key {item.letter}">{item.letter}</span>{/if}
  {@render children()}
</div>

<style>
  .frame {
    position: relative;
  }
  /* The timeline's text starts calc(66 * var(--px)) from the column's left. */
  .bar {
    position: absolute;
    left: calc(-10 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(2 * var(--px));
    background: var(--ink);
    pointer-events: none;
  }

  /* A key on the rail marks where the chat branched or tools ran (4.4). It
     sits level with the item's first box: the row of a tool group, the box
     or the text under a byline. */
  .key {
    position: absolute;
    left: calc(-48 * var(--px));
    top: 0;
    box-sizing: border-box;
    width: calc(27 * var(--px));
    height: calc(29 * var(--px));
    border: calc(2 * var(--px)) solid var(--vermilion);
    background: var(--vermilion-wash);
    color: var(--vermilion-ink);
    font: var(--t-key);
    text-align: center;
    display: grid;
    place-items: center;
    user-select: none;
  }
  .frame[data-kind="tools"] > .key,
  .frame[data-kind="tool"] > .key {
    top: calc(1 * var(--px));
  }
  .frame[data-kind="user"] > .key {
    top: calc(29 * var(--px));
  }
  .frame[data-kind="reply"] > .key {
    top: calc(37 * var(--px));
  }
</style>
