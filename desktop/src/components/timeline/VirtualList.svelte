<script>
  // The timeline's virtual list (desktop-design 1.6 items 2, 3 and 8;
  // CHAT-087, CHAT-092 to CHAT-095, CHAT-099, CHAT-105). Only the items in
  // the view and one view above and below are on the page, plus the pinned
  // ones (the two items holding a text selection's ends); spacers stand for
  // the rest. Heights are measured by one ResizeObserver and kept by key,
  // version and column width (lib/vlist.js); items never measured use the
  // estimate of their kind. The first item in view and how far into it the
  // view starts stay still when anything above changes height; while
  // following, the bottom stays in view instead. Each rendered row is kept in
  // an LRU of 300 by key, its DOM taken off the page when it leaves the
  // window and put back when it returns, so scrolling back never draws a
  // reply again; an item whose version changed meanwhile is updated in place.
  //
  // The scrollbar is the app's plain one (app.css: a thin grey rectangle, no
  // arrows), as everywhere in the app (owner, 2026-10-26; CHAT-095): "never"
  // hides it; "always" shows it; "default" shows its thumb while scrolling
  // and 2 s after (and while it is dragged). A hidden thumb is only
  // transparent (app.css), so the bar still takes presses.
  // Placed by Timeline.
  import { flushSync, mount, unmount, untrack } from "svelte";
  import { barThumb, dragTop } from "../../lib/bardrag.js";
  import { userActed } from "../../lib/barfade.js";
  import {
    anchorAt,
    atBottom,
    nearBottom,
    clampTop,
    createHeights,
    estimate,
    gapBefore,
    inView,
    itemTop,
    layout,
    maxTop,
    mountedIndices,
    scrollToShow,
    segments,
    topForAnchor,
    visibleRange,
    windowRange,
  } from "../../lib/vlist.js";
  import Row from "./Row.svelte";

  const CACHE_SIZE = 300;
  const BAR_SHOW_MS = 2000;

  let {
    items = [],
    selectedKey = null,
    focusKey = null,
    pinnedKeys = [],
    follow = true,
    scrollbar = "default",
    onfollow = null, // (on) a scroll by the user turned following on or off
    onscrolled = null, // ({up}) after a scroll by the user (wheel, bar, keys of the page)
    label = "Messages",
  } = $props();

  let scroller;
  let canvas;
  let viewport = $state(0);
  let width = $state(0);
  let zoom = $state(1);
  let top = $state(0);
  let measured = $state(0); // bumped when a measured height changes
  let lastTop = 0;
  const heights = createHeights();

  const keys = $derived(items.map((item) => item.key));
  const indexOf = $derived(new Map(keys.map((key, i) => [key, i])));
  const lay = $derived.by(() => {
    measured;
    const z = zoom;
    const w = width;
    const list = items;
    return layout(
      list,
      (i) => heights.get(list[i], w) ?? estimate(list[i]) * z,
      (i) => gapBefore(list[i - 1], list[i]) * z,
      { top: 24 * z, bottom: 24 * z },
    );
  });
  // While following, the view is the bottom by definition (the first frame of
  // a long chat draws its end, CHAT-105).
  const viewTop = $derived(follow ? maxTop(lay, viewport) : top);
  const range = $derived(windowRange(lay, viewTop, viewport));
  const pinned = $derived(pinnedKeys.map((key) => indexOf.get(key)).filter((i) => i !== undefined));
  const mounted = $derived(mountedIndices(range, pinned));
  const segs = $derived(segments(lay, mounted));

  // -- Rendered rows (1.6 item 3) --

  const cache = new Map(); // key → entry
  let clock = 0;
  let attached = 0;

  function makeEntry(item) {
    let current = $state.raw(item);
    let isSelected = $state(false);
    let isFocused = $state(false);
    const entry = {
      key: item.key,
      container: null,
      component: null,
      attached: false,
      used: 0,
      waiting: null, // a newer item that came while off the page
      props: {
        get item() {
          return current;
        },
        get selected() {
          return isSelected;
        },
        get focused() {
          return isFocused;
        },
      },
      set(next, selected, focused) {
        if (current !== next) current = next;
        if (isSelected !== selected) isSelected = selected;
        if (isFocused !== focused) isFocused = focused;
      },
    };
    return entry;
  }

  function drop(entry) {
    if (entry.component) unmount(entry.component);
    entry.component = null;
    cache.delete(entry.key);
  }

  function trim() {
    if (cache.size <= CACHE_SIZE) return;
    const idle = [...cache.values()].filter((entry) => !entry.attached).sort((a, b) => a.used - b.used);
    for (const entry of idle) {
      if (cache.size <= CACHE_SIZE) break;
      drop(entry);
    }
  }

  // The action on each item's slot: puts the item's row (made now, or kept
  // from before) into the slot and measures it.
  function host(node, key) {
    const entry = untrack(() => {
      const item = items[indexOf.get(key)];
      let found = cache.get(key);
      if (!found) {
        found = makeEntry(item);
        found.container = document.createElement("div");
        found.container.className = "row";
        cache.set(key, found);
      }
      found.set(found.waiting ?? item ?? found.props.item, key === selectedKey, key === focusKey);
      found.waiting = null;
      return found;
    });
    node.append(entry.container);
    if (!entry.component) entry.component = mount(Row, { target: entry.container, props: entry.props });
    entry.attached = true;
    entry.used = ++clock;
    attached += 1;
    resizer.observe(node);
    return {
      destroy() {
        resizer.unobserve(node);
        entry.container.remove();
        entry.attached = false;
        entry.used = ++clock;
        attached -= 1;
        trim();
      },
    };
  }

  // New items, selection and focus reach the rows on the page; rows off the
  // page get theirs when they come back; rows of items that left the chat go.
  $effect(() => {
    const byKey = new Map(items.map((item) => [item.key, item]));
    const sel = selectedKey;
    const foc = focusKey;
    untrack(() => {
      for (const entry of [...cache.values()]) {
        const item = byKey.get(entry.key);
        if (!item) {
          if (!entry.attached) drop(entry);
          continue;
        }
        if (entry.attached) entry.set(item, entry.key === sel, entry.key === foc);
        else if (entry.props.item !== item) entry.waiting = item;
      }
      heights.keep(new Set(byKey.keys()));
    });
  });

  // -- Measuring and keeping the view still --

  function readZoom() {
    const size = parseFloat(getComputedStyle(document.documentElement).fontSize);
    return Number.isFinite(size) && size > 0 ? size / 16 : 1;
  }

  const resizer = new ResizeObserver((entries) => {
    let changed = false;
    for (const entry of entries) {
      const node = entry.target;
      if (node === scroller || node === canvas) {
        changed = readSizes() || changed;
        continue;
      }
      if (!node.isConnected) continue;
      const i = indexOf.get(node.dataset.key);
      if (i === undefined) continue;
      if (heights.set(items[i], width, entry.contentRect.height)) changed = true;
    }
    if (changed) {
      measured += 1;
      flushSync();
    }
    evenEnd();
    placeGrip();
  });

  // The scroll range ends on a whole pixel (owner, 2026-10-20: "scrolling
  // further down pushes it up/down 1-2 pixels"): Chromium keeps a fractional
  // end offset, then snaps a wheel scroll at the bottom to the whole pixel
  // above it. A little more room under the last item rounds it up.
  let even = $state(0);
  function evenEnd() {
    if (!scroller || !canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const end = (canvas.getBoundingClientRect().height - even - scroller.getBoundingClientRect().height) * dpr;
    const next = end > 0 ? (Math.ceil(end - 0.001) - end) / dpr : 0;
    if (Math.abs(next - even) > 0.001) even = next;
  }

  function readSizes() {
    if (!scroller || !canvas) return false;
    const z = readZoom();
    const v = scroller.clientHeight;
    const style = getComputedStyle(canvas);
    const w = canvas.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
    const changed = z !== zoom || v !== viewport || Math.abs(w - width) >= 0.5;
    zoom = z;
    viewport = v;
    width = Math.round(w * 100) / 100;
    return changed;
  }

  // What was on the page last, to find the anchor before the next change.
  let shown = { lay: null, keys: [] };
  let anchor = null;
  let reveal = null; // a key to keep in view until the measures settle

  $effect.pre(() => {
    lay;
    untrack(() => {
      anchor = !follow && !reveal && shown.lay && scroller ? anchorAt(shown.lay, shown.keys, top) : null;
    });
  });

  $effect(() => {
    const l = lay;
    const k = keys;
    const f = follow;
    untrack(() => {
      shown = { lay: l, keys: k };
      if (!scroller) return;
      if (f) toBottom();
      else if (reveal) {
        const i = indexOf.get(reveal);
        if (i === undefined) reveal = null;
        else setTop(scrollToShow(l, i, scroller.scrollTop, scroller.clientHeight));
      } else if (anchor) {
        const t = topForAnchor(l, k, anchor);
        if (t !== null && Math.abs(t - scroller.scrollTop) >= 0.5) setTop(clampTop(l, scroller.clientHeight, t));
      }
      anchor = null;
      placeGrip();
    });
  });

  // The grip follows the Settings choice for the bar.
  $effect(() => {
    scrollbar;
    untrack(placeGrip);
  });

  function setTop(t) {
    scroller.scrollTop = t;
    lastTop = scroller.scrollTop;
    top = lastTop;
  }

  function toBottom() {
    setTop(scroller.scrollHeight);
  }

  // Runs once: its cleanup (the component going away) drops every kept row,
  // so it must track nothing; the sizes it reads change all the time.
  $effect(() => {
    untrack(readSizes);
    resizer.observe(scroller);
    resizer.observe(canvas);
    return () => {
      resizer.disconnect();
      clearTimeout(barTimer);
      for (const entry of [...cache.values()]) drop(entry);
    };
  });

  // -- Scrolling by the user --

  let barShown = $state(false);
  let dragging = $state(false);
  let barTimer = null;

  function flashBar() {
    if (scrollbar !== "default") return;
    barShown = true;
    clearTimeout(barTimer);
    barTimer = setTimeout(hideBar, BAR_SHOW_MS);
  }

  function hideBar() {
    if (dragging) {
      barTimer = setTimeout(hideBar, BAR_SHOW_MS);
      return;
    }
    barShown = false;
  }

  function onScroll() {
    placeGrip();
    const t = scroller.scrollTop;
    if (Math.abs(t - lastTop) < 0.5) return; // our own
    const up = t < lastTop;
    lastTop = t;
    top = t;
    reveal = null;
    const bottom = atBottom(t, scroller.clientHeight, scroller.scrollHeight);
    // Still at the bottom while following: the page moved the view (the
    // typing box shrank after a send), not the user, so the thumb stays off.
    if (follow && bottom) return;
    // Only a scroll the user made shows the thumb, not the view kept still
    // while the window or a row above changed size (lib/barfade.js).
    if (userActed() || dragging) flashBar();
    // Within about two lines of the bottom still counts as the bottom, so a
    // tiny nudge keeps following (owner, 2026-10-22).
    const near = nearBottom(t, scroller.clientHeight, scroller.scrollHeight, zoom);
    if (up && !near) {
      if (follow) onfollow?.(false);
    } else if (near && !follow) onfollow?.(true);
    onscrolled?.({ up });
  }

  // A press on the bar's column, or its track (CHAT-095): it
  // lasts until the button is released anywhere.
  function onMouseDown(event) {
    if (event.button !== 0 || event.target !== scroller || event.offsetX < scroller.clientWidth) return;
    dragging = true;
    flashBar();
    window.addEventListener(
      "mouseup",
      () => {
        dragging = false;
        flashBar();
      },
      { once: true, capture: true },
    );
  }

  // The grip: an invisible piece over the bar's thumb, so a press on the
  // thumb is dragged here, not by the browser (lib/bardrag.js). The browser's
  // bar keeps the mouse to itself while its thumb is dragged and moves by the
  // mouse's steps, so as the chat measured older messages a drag to the top
  // stopped a quarter of the way down (2026-10-26). Here each move puts the
  // view where the mouse is, on the chat as long as it is now. The track stays
  // the browser's.
  let grip = $state(null); // {left, top, width, height, travel} in the parent's px
  let gripDrag = null;

  function placeGrip() {
    if (!scroller) return;
    const thickness = scroller.offsetWidth - scroller.clientWidth;
    const client = scroller.clientHeight;
    const total = scroller.scrollHeight;
    if (scrollbar === "never" || thickness <= 0 || total <= client) {
      grip = null;
      return;
    }
    const thumb = barThumb({ height: client, client, total, top: scroller.scrollTop });
    grip = { left: scroller.offsetLeft + scroller.clientWidth, top: scroller.offsetTop + thumb.start, width: thickness, height: thumb.length, travel: thumb.travel };
  }

  function gripDown(event) {
    if (event.button !== 0 || !grip) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    gripDrag = { y0: event.clientY, frac: scroller.scrollTop / Math.max(1, scroller.scrollHeight - scroller.clientHeight), travel: grip.travel };
    dragging = true;
    flashBar();
  }

  function gripMove(event) {
    if (!gripDrag) return;
    const range = scroller.scrollHeight - scroller.clientHeight;
    scroller.scrollTop = dragTop({ frac: gripDrag.frac, dy: event.clientY - gripDrag.y0, travel: gripDrag.travel, range });
  }

  function gripUp() {
    if (!gripDrag) return;
    gripDrag = null;
    dragging = false;
    flashBar();
  }

  // A wheel turned over the grip still scrolls the chat.
  function gripWheel(event) {
    scroller.scrollBy({ top: event.deltaMode === 1 ? event.deltaY * 40 : event.deltaY });
  }

  // -- What Timeline asks --

  export function element() {
    return scroller;
  }

  export function scrollOffset() {
    return scroller ? scroller.scrollTop : 0;
  }

  // The offset the list last set or saw, without reading the page (the
  // probe's light fields, 1.9).
  export function lastOffset() {
    return top;
  }

  // Whether the thumb shows now (CHAT-094): "always", or with "default"
  // while scrolling and 2 s after, or while it is dragged.
  export function barShowing() {
    return scrollbar === "always" || (scrollbar === "default" && (barShown || dragging));
  }

  export function viewportHeight() {
    return scroller ? scroller.clientHeight : 0;
  }

  // Scrolls by dy real pixels (keys); returns whether the view should follow,
  // using the same allowance as a wheel scroll.
  export function scrollByPx(dy) {
    reveal = null;
    setTop(scroller.scrollTop + dy);
    flashBar();
    return nearBottom(scroller.scrollTop, scroller.clientHeight, scroller.scrollHeight, zoom);
  }

  export function scrollToTop() {
    reveal = null;
    setTop(0);
    flashBar();
  }

  export function scrollToBottom() {
    reveal = null;
    toBottom();
  }

  // Scrolls the least that shows the item, and keeps it shown while the items
  // around it are measured.
  export function scrollToKey(key) {
    const i = indexOf.get(key);
    if (i === undefined || !scroller) return false;
    reveal = key;
    setTop(scrollToShow(lay, i, scroller.scrollTop, scroller.clientHeight));
    flushSync();
    setTop(scrollToShow(lay, i, scroller.scrollTop, scroller.clientHeight));
    flashBar();
    return true;
  }

  // Puts the item's top at the view's top (a revealed message, 4.3).
  export function scrollKeyToTop(key) {
    const i = indexOf.get(key);
    if (i === undefined || !scroller) return false;
    reveal = null;
    setTop(clampTop(lay, scroller.clientHeight, itemTop(lay, i)));
    flushSync();
    setTop(clampTop(lay, scroller.clientHeight, itemTop(lay, i)));
    return true;
  }

  export function atEnd() {
    return scroller ? atBottom(scroller.scrollTop, scroller.clientHeight, scroller.scrollHeight) : true;
  }

  export function keyInView(key) {
    const i = indexOf.get(key);
    return i !== undefined && scroller ? inView(lay, i, scroller.scrollTop, scroller.clientHeight) : false;
  }

  // The keys of the items in view, first to last.
  export function keysInView() {
    if (!scroller) return [];
    const r = visibleRange(lay, scroller.scrollTop, scroller.clientHeight);
    return r ? keys.slice(r.first, r.last + 1) : [];
  }

  // The item's frame while it is on the page, else null.
  export function frameOf(key) {
    const entry = cache.get(key);
    return entry?.attached ? (entry.container.firstElementChild ?? null) : null;
  }

  // The item's frame while it is drawn, on the page or kept off it, else null.
  export function drawnFrameOf(key) {
    return cache.get(key)?.container.firstElementChild ?? null;
  }

  export function mountedCount() {
    return attached;
  }

  // How many items the window holds without the pinned ones.
  export function windowCount() {
    return range ? range.last - range.first + 1 : 0;
  }
</script>

<!-- The press handler only watches the native scrollbar's column (CHAT-095). -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  class="scroller"
  class:bar-never={scrollbar === "never"}
  data-bar-fade="own"
  class:bar-always={scrollbar === "always"}
  class:bar-shown={scrollbar === "default" && (barShown || dragging)}
  bind:this={scroller}
  onscroll={onScroll}
  onmousedown={onMouseDown}
  role="feed"
  aria-label={label}
  aria-busy="false"
>
  <div class="canvas" style:--even="{even}px" bind:this={canvas}>
    {#each segs as seg (seg.spacer ? seg.id : keys[seg.index])}
      {#if seg.spacer}
        <div class="spacer" style:height="{seg.height}px" aria-hidden="true"></div>
      {:else}
        <div class="slot" data-key={keys[seg.index]} style:padding-top="{lay.gaps[seg.index]}px" use:host={keys[seg.index]}></div>
      {/if}
    {/each}
  </div>
</div>
{#if grip}
  <div
    class="grip"
    style:left="{grip.left}px"
    style:top="{grip.top}px"
    style:width="{grip.width}px"
    style:height="{grip.height}px"
    aria-hidden="true"
    data-probe="chat-grip"
    onpointerdown={gripDown}
    onpointermove={gripMove}
    onpointerup={gripUp}
    onpointercancel={gripUp}
    onwheel={gripWheel}
  ></div>
{/if}

<style>
  /* Over the thumb, drawing nothing; placed in the scroller's parent. */
  .grip {
    position: absolute;
    z-index: 1;
  }
  .scroller {
    flex: 1;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* --bar-shown makes WebKitGTK redraw the bar (app.css). */
  .scroller.bar-shown {
    --bar-shown: 1;
  }
  .scroller.bar-never {
    scrollbar-width: none;
  }
  .scroller.bar-never::-webkit-scrollbar {
    display: none;
  }
  /* The picture's padding (Conversation.svelte:133 as built): the text starts
     calc(66 * var(--px)) from the column's left (CHAT-001). */
  .canvas {
    padding: calc(24 * var(--px)) calc(23 * var(--px)) calc(24 * var(--px) + var(--even, 0px)) calc(66 * var(--px));
  }
  .slot {
    display: flow-root;
  }
</style>
