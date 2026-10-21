<script module>
  // Items new to a shown chat settle in (the paper motion, owner 2026-10-19).
  // Timeline marks their keys before they mount; a mark lasts 1.5 s, so an
  // item scrolled away and back later is drawn still.
  const freshUntil = new Map(); // key → performance.now() deadline
  export function markFresh(keys) {
    const until = performance.now() + 1500;
    for (const key of keys) freshUntil.set(key, until);
  }
  function takeFresh(key) {
    const until = freshUntil.get(key);
    if (until === undefined) return false;
    freshUntil.delete(key);
    return until >= performance.now();
  }
</script>

<script>
  // One timeline item's frame (desktop-design 1.5.6, 4.4; CHAT-003, CHAT-016,
  // CHAT-054): the vermilion key on the rail and the keyboard focus around the
  // item, which comes as `children`. A selected item draws no bar beside it
  // (owner, 2026-10-19: no line when a row is clicked). The frame is
  // the feed's article; the item that holds the timeline's one tab stop
  // (`focused`) has tabindex 0, the rest -1.
  // Placed by Timeline.
  let { item, selected, focused, children } = $props();
  // Only whole new pieces settle: a reply, a tool group or call, a queued
  // message or a shell. Status lines and banners that come and go while the
  // agent works stay still, so a busy chat never twitches.
  const SETTLES = new Set(["reply", "tools", "tool", "pending", "shell"]);
  const fresh = takeFresh(item.key) && SETTLES.has(item.kind);

  // The key sits on the middle of the item's first box (owner, 2026-10-19):
  // a tool group's row, the user's box, or a reply's first line of text.
  // Measured, so it stays centred whatever the box's height or the zoom;
  // the fixed tops below hold until the first measure.
  let frame = $state();
  let keyTop = $state(null);
  $effect(() => {
    if (!item.letter || !frame) return;
    const place = () => {
      const key = frame.querySelector(":scope > .key");
      const box = frame.querySelector(".tool-row, .user .box, .reply .body");
      if (!key || !box) return;
      let { top, height } = box.getBoundingClientRect();
      if (box.matches(".reply .body:not(.plan)")) {
        const line = parseFloat(getComputedStyle(box.querySelector("p, li, h1, h2, h3, h4, pre") ?? box).lineHeight);
        if (line > 0 && line < height) height = line;
      }
      const next = top - frame.getBoundingClientRect().top + (height - key.offsetHeight) / 2;
      if (keyTop === null || Math.abs(next - keyTop) > 0.25) keyTop = next;
    };
    place();
    const sizes = new ResizeObserver(place);
    sizes.observe(frame);
    return () => sizes.disconnect();
  });
</script>

<!-- The feed pattern makes each article focusable (WAI-ARIA APG, Feed). -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  bind:this={frame}
  class="frame"
  class:fresh
  role="article"
  data-key={item.key}
  data-kind={item.kind}
  data-selected={selected ? "true" : undefined}
  aria-current={selected ? "true" : undefined}
  tabindex={focused ? 0 : -1}
>
  {#if item.letter}<span class="key" style:top={keyTop === null ? null : `${keyTop}px`} aria-label="Key {item.letter}">{item.letter}</span>{/if}
  {@render children()}
</div>

<style>
  .frame {
    position: relative;
  }
  /* No ring round a message (owner, 2026-10-20: "remove the orange
     selector"): the timeline's keys still move between items. */
  .frame:focus-visible {
    outline: none;
  }
  .frame.fresh {
    animation: paper-settle var(--motion) var(--ease-paper);
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
