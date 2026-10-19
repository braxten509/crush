<script>
  // A filtered list (desktop-design-5-6 2.1, DLG-010 to DLG-012; look 5.1).
  // A query keeps the rows that fuzzy-match it, best first, with the matched
  // letters underlined (lib/kit/fuzzy.js, the terminal's sahilm/fuzzy;
  // internal/ui/list/filterable.go:91-119); an empty query shows every row in
  // its own order with no marks. A grouped list keeps its rows in their own
  // order under a query, under the headings of the groups that still have
  // rows (internal/ui/dialog/models/list.go:193-250). A new query selects the
  // first row and goes back to the top (commands.go:219-226). A long title
  // ends with "…". The info column on the right takes at most half the row and
  // is cut with "…" (kit/listitem.go:69-88); with an info budget the whole
  // column hides while its widest entry would take more than that share of
  // the row, and comes back when the row is wide enough
  // (kit/infocolumn.go:30-45). The scrollbar and its column appear only while
  // the rows overflow (kit/layout.go:59-76), and the edge with more rows fades.
  // In a kit sheet, ↑/↓ move the selection round the ends (the terminal's
  // lists all wrap, e.g. commands.go:178-193) and Enter calls onChoose; a
  // click selects and a second click within 400 ms chooses (2.0 rule 6).
  //   items, filterText(item), query, selected (bindable: the selected item's
  //   index in items, -1 with no rows), infoBudget? (a share such as 0.25, or
  //   a percent such as 25), groups? (item → its group's heading), row? (a
  //   snippet row(item, index, {matched, selected}); without it the row draws
  //   the title and the info), title?(item) (the title shown, filterText by
  //   default), info?(item), detail?(item) (a second line), onChoose?(item,
  //   index), empty? (the line shown with no rows), label? (accessible name)
  // Methods: move(delta, {wrap}), selectFirst(), selectLast(), chosen(),
  // visibleIndexes().
  import { tick, untrack } from "svelte";
  import { fuzzyFind } from "../../lib/kit/fuzzy.js";
  import { setPart } from "../../lib/kit/sheetkeys.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import Scrollbar from "./Scrollbar.svelte";

  const DOUBLE_CLICK_MS = 400;
  const INFO_GAP = 16; // picture px between a title and its info

  let {
    items = [],
    filterText = (item) => String(item),
    query = "",
    selected = $bindable(0),
    infoBudget = null,
    groups = null,
    row = null,
    title = null,
    info = null,
    detail = null,
    onChoose = null,
    empty = "",
    label = "",
  } = $props();

  let root = $state(null);
  let scroller = $state(null);
  let meter = $state(null);
  let bar = $state(null);
  let rowWidth = $state(0);
  let fontsLoaded = $state(0);
  let fadeTop = $state(false);
  let fadeBottom = $state(false);

  const barMode = $derived(ws.config?.options?.tui?.scrollbar ?? "default");
  const titleOf = (item) => String((title ? title(item) : filterText(item)) ?? "");
  const textOf = (item) => String(filterText(item) ?? "");

  // The rows that show, best first ({index, matched}).
  const visible = $derived.by(() => {
    const q = query ?? "";
    if (!q) return items.map((_, index) => ({ index, matched: [] }));
    const found = fuzzyFind(q, items.map(textOf));
    if (groups) found.sort((a, b) => a.index - b.index);
    return found.map(({ index, matched }) => ({ index, matched }));
  });

  // Sections under their headings, in the order the groups first appear.
  const sections = $derived.by(() => {
    if (!groups) return [{ key: "", label: "", rows: visible }];
    const byLabel = new Map();
    for (const entry of visible) {
      const heading = String(groups(items[entry.index]) ?? "");
      if (!byLabel.has(heading)) byLabel.set(heading, { key: heading, label: heading, rows: [] });
      byLabel.get(heading).rows.push(entry);
    }
    return [...byLabel.values()];
  });

  // The rows' item indexes in drawn order, and their matched letters.
  const order = $derived(sections.flatMap((section) => section.rows.map((entry) => entry.index)));
  const matches = $derived(new Map(visible.map((entry) => [entry.index, entry.matched])));

  // Marks only where the title is the text the query matched.
  const titleMatched = (index) => (textOf(items[index]) === titleOf(items[index]) ? (matches.get(index) ?? []) : []);

  // -- The info column's budget --

  const share = $derived(infoBudget === null || infoBudget === undefined ? null : infoBudget > 1 ? infoBudget / 100 : infoBudget);
  let pen = null;
  const widest = $derived.by(() => {
    void fontsLoaded;
    void rowWidth;
    if (!info || !meter) return 0;
    const style = getComputedStyle(meter);
    pen ??= document.createElement("canvas").getContext("2d");
    if (!pen) return 0;
    pen.font = `${style.fontStyle} ${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
    let most = 0;
    for (const index of order) {
      const text = String(info(items[index]) ?? "");
      if (text) most = Math.max(most, pen.measureText(text).width);
    }
    return most === 0 ? 0 : most + INFO_GAP * scale();
  });
  const infoHidden = $derived(share !== null && rowWidth > 0 && widest > rowWidth * share);

  function scale() {
    return parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1;
  }

  // -- Selection --

  let lastQuery = untrack(() => query);
  $effect(() => {
    const q = query;
    if (q === lastQuery) return;
    lastQuery = q;
    untrack(() => {
      selected = order[0] ?? -1;
      if (scroller) scroller.scrollTop = 0;
    });
  });

  // A selection that is not drawn goes to the first row.
  $effect(() => {
    const drawn = order;
    untrack(() => {
      if (!drawn.includes(selected)) selected = drawn[0] ?? -1;
    });
  });

  export function move(delta, { wrap = true } = {}) {
    if (order.length === 0) return false;
    const at = order.indexOf(selected);
    let next = at < 0 ? 0 : at + delta;
    if (wrap) next = ((next % order.length) + order.length) % order.length;
    else next = Math.min(Math.max(next, 0), order.length - 1);
    selected = order[next];
    reveal();
    return true;
  }

  export function selectFirst() {
    if (order.length === 0) return false;
    selected = order[0];
    reveal();
    return true;
  }

  export function selectLast() {
    if (order.length === 0) return false;
    selected = order[order.length - 1];
    reveal();
    return true;
  }

  export function chosen() {
    return order.includes(selected) ? items[selected] : null;
  }

  export function visibleIndexes() {
    return [...order];
  }

  // Scrolls the list (and nothing around it) the least that shows the row.
  async function reveal() {
    await tick();
    const el = scroller?.querySelector(`[data-index="${selected}"]`);
    if (!el) return;
    const r = el.getBoundingClientRect();
    const view = scroller.getBoundingClientRect();
    if (r.top < view.top) scroller.scrollTop -= view.top - r.top;
    else if (r.bottom > view.top + scroller.clientHeight) scroller.scrollTop += r.bottom - (view.top + scroller.clientHeight);
    updateFades();
  }

  let lastClick = { index: -1, at: 0 };
  function onRowClick(index) {
    const now = Date.now();
    const again = lastClick.index === index && now - lastClick.at <= DOUBLE_CLICK_MS;
    lastClick = { index, at: now };
    selected = index;
    if (again && onChoose) onChoose(items[index], index);
  }

  // ↑/↓ and Enter in a kit sheet, unless an editor or a button has the focus.
  function keys(event, combo) {
    const active = document.activeElement;
    if (active?.tagName === "TEXTAREA" || active?.isContentEditable) return false;
    if (combo === "ArrowUp") return move(-1);
    if (combo === "ArrowDown") return move(1);
    if (combo === "Enter" && onChoose && active?.tagName !== "BUTTON") {
      const item = chosen();
      if (item === null) return false;
      onChoose(item, selected);
      return true;
    }
    return false;
  }

  // -- Sizes and edges --

  function updateFades() {
    if (!scroller) return;
    fadeTop = scroller.scrollTop > 0.5;
    fadeBottom = scroller.scrollTop + scroller.clientHeight < scroller.scrollHeight - 0.5;
  }

  function measureRow() {
    if (!scroller) return;
    rowWidth = Math.max(0, scroller.clientWidth - 2 * 26 * scale());
    updateFades();
  }

  $effect(() => {
    if (!scroller) return;
    const observer = new ResizeObserver(measureRow);
    observer.observe(scroller);
    if (scroller.firstElementChild) observer.observe(scroller.firstElementChild);
    measureRow();
    const fonts = document.fonts;
    const loaded = () => fontsLoaded++;
    fonts?.addEventListener?.("loadingdone", loaded);
    fonts?.ready?.then(loaded);
    return () => {
      observer.disconnect();
      fonts?.removeEventListener?.("loadingdone", loaded);
    };
  });

  $effect(() => {
    void sections;
    void infoHidden;
    tick().then(measureRow);
  });

  // -- The probe --

  $effect(() => {
    if (!root) return;
    return setPart(root, {
      kind: "list",
      keys,
      report: () => ({
        kind: "list",
        label,
        query,
        selected,
        rows: order.map((index) => ({
          index,
          title: titleOf(items[index]),
          info: info ? String(info(items[index]) ?? "") : null,
          matched: matches.get(index) ?? [],
          group: groups ? String(groups(items[index]) ?? "") : null,
        })),
        groups: sections.map((section) => section.label).filter(Boolean),
        info_hidden: infoHidden,
        info_budget: share,
      }),
      heavy: () => heavyReport(),
    });
  });

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  function heavyReport() {
    const rows = [...(scroller?.querySelectorAll("[data-index]") ?? [])].map((el) => {
      const titleEl = el.querySelector(".title");
      const infoEl = el.querySelector(".info");
      const edge = getComputedStyle(el, "::after");
      return {
        index: Number(el.dataset.index),
        rect: rectOf(el),
        background: getComputedStyle(el).backgroundColor,
        edge: el.classList.contains("selected") ? { color: edge.backgroundColor, width: edge.width, left: edge.left } : null,
        title: titleEl ? { rect: rectOf(titleEl), cut: titleEl.scrollWidth > titleEl.clientWidth + 0.5, overflow: getComputedStyle(titleEl).textOverflow } : null,
        info: infoEl ? { rect: rectOf(infoEl), cut: infoEl.scrollWidth > infoEl.clientWidth + 0.5, color: getComputedStyle(infoEl).color } : null,
        marks: [...el.querySelectorAll("u")].map((u) => {
          const style = getComputedStyle(u);
          return { text: u.textContent, rect: rectOf(u), line: style.textDecorationLine, thickness: style.textDecorationThickness, color: style.textDecorationColor, offset: style.textUnderlineOffset };
        }),
      };
    });
    return {
      kind: "list",
      rect: rectOf(root),
      scroller: rectOf(scroller),
      client_width: scroller?.clientWidth ?? 0,
      offset_width: scroller?.offsetWidth ?? 0,
      scroll_height: scroller?.scrollHeight ?? 0,
      client_height: scroller?.clientHeight ?? 0,
      scroll_top: scroller?.scrollTop ?? 0,
      bar: { mode: barMode, showing: bar?.showing?.() ?? null },
      fades: { top: fadeTop, bottom: fadeBottom },
      row_width: rowWidth,
      info_widest: widest,
      groups: [...(scroller?.querySelectorAll("[data-group]") ?? [])].map((el) => ({ text: el.textContent, rect: rectOf(el), transform: getComputedStyle(el).textTransform })),
      empty: rectOf(root?.querySelector("[data-empty]")),
      rows,
    };
  }

  function runs(text, matched) {
    if (!matched.length) return [{ text, mark: false }];
    const marked = new Set(matched);
    const out = [];
    let at = 0;
    for (const ch of text) {
      const mark = marked.has(at);
      const last = out[out.length - 1];
      if (last && last.mark === mark) last.text += ch;
      else out.push({ text: ch, mark });
      at += ch.length;
    }
    return out;
  }
</script>

<div class="filter-list" data-kit-list bind:this={root}>
  <ul class="scroller" role="listbox" aria-label={label || "Items"} tabindex="-1" bind:this={scroller} onscroll={updateFades}>
    {#each sections as section (section.key)}
      {#if section.label}
        <li class="group" role="presentation" data-group>{section.label}</li>
      {/if}
      {#each section.rows as entry (entry.index)}
        {@const item = items[entry.index]}
        {@const isSelected = entry.index === selected}
        <li
          class="row"
          class:selected={isSelected}
          class:two-line={Boolean(detail) && !row}
          role="option"
          aria-selected={isSelected}
          tabindex="-1"
          data-index={entry.index}
          onmousedown={(event) => event.preventDefault()}
          onclick={() => onRowClick(entry.index)}
          onkeydown={() => {}}
        >
          {#if row}
            {@render row(item, entry.index, { matched: entry.matched, selected: isSelected })}
          {:else}
            <span class="first">
              <span class="title">{#each runs(titleOf(item), titleMatched(entry.index)) as run, i (i)}{#if run.mark}<u>{run.text}</u>{:else}{run.text}{/if}{/each}</span>
              {#if info && !infoHidden}
                {@const text = String(info(item) ?? "")}
                {#if text}<span class="info">{text}</span>{/if}
              {/if}
            </span>
            {#if detail}
              <span class="detail">{detail(item) ?? ""}</span>
            {/if}
          {/if}
        </li>
      {/each}
    {/each}
  </ul>
  {#if order.length === 0 && empty}
    <p class="empty" data-empty>{empty}</p>
  {/if}
  <span class="fade top" class:on={fadeTop} aria-hidden="true"></span>
  <span class="fade bottom" class:on={fadeBottom} aria-hidden="true"></span>
  <span class="meter info" aria-hidden="true" bind:this={meter}></span>
  <Scrollbar target={scroller} mode={barMode} bind:this={bar} />
</div>

<style>
  /* The rows run from frame edge to frame edge: the sheet's 26 of padding on
     each side is the rows' own, so titles line up with the title and field. */
  .filter-list {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    min-height: 0;
    margin: 0 calc(-26 * var(--px));
  }
  .scroller {
    flex: 1 1 auto;
    min-height: 0;
    margin: 0;
    padding: 0;
    overflow-x: hidden;
    overflow-y: auto;
    outline: none;
  }
  .row {
    position: relative;
    display: flex;
    align-items: center;
    box-sizing: border-box;
    height: calc(44 * var(--px));
    padding: 0 calc(26 * var(--px));
    cursor: default;
    outline: none;
  }
  .row.two-line {
    height: calc(64 * var(--px));
    flex-direction: column;
    align-items: stretch;
    justify-content: center;
  }
  .first {
    display: flex;
    flex: 1 1 auto;
    align-items: baseline;
    min-width: 0;
  }
  .two-line .first {
    flex: none;
  }
  .title {
    flex: 1 1 0;
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .info,
  .meter {
    font: var(--t-small);
    color: var(--slate);
    white-space: pre;
  }
  .info {
    flex: 0 1 auto;
    max-width: calc(50% - 16 * var(--px));
    margin-left: calc(16 * var(--px));
    overflow: hidden;
    text-overflow: ellipsis;
    text-align: right;
  }
  .detail {
    margin-top: calc(5 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .meter {
    position: absolute;
    visibility: hidden;
    pointer-events: none;
  }
  .row:hover {
    background: var(--control-hover);
  }
  /* The selected row: a band between faint lines, with the vermilion edge
     (the shown chat's row, ChatList, look 5.0). */
  .row.selected {
    background: var(--band);
  }
  .row.selected::before {
    content: "";
    position: absolute;
    inset: 0;
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    pointer-events: none;
  }
  .row.selected::after {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }
  .row.selected .info,
  .row.selected .detail,
  .row:hover .info,
  .row:hover .detail {
    color: var(--ink);
  }
  u {
    text-decoration-line: underline;
    text-decoration-thickness: 1px;
    text-decoration-color: var(--ink);
    text-underline-offset: calc(3 * var(--px));
  }
  .group {
    padding: calc(14 * var(--px)) calc(26 * var(--px)) calc(13 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .empty {
    margin: calc(20 * var(--px)) calc(26 * var(--px)) 0;
    font: var(--t-body-small);
    color: var(--slate);
  }
  .fade {
    position: absolute;
    left: 0;
    right: 0;
    height: calc(16 * var(--px));
    pointer-events: none;
    visibility: hidden;
  }
  .fade.on {
    visibility: visible;
  }
  .fade.top {
    top: 0;
    background: linear-gradient(to bottom, var(--paper), transparent);
  }
  .fade.bottom {
    bottom: 0;
    background: linear-gradient(to top, var(--paper), transparent);
  }
</style>
