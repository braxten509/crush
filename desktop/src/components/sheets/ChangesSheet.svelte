<script>
  // The Changes sheet (DLG-294 to DLG-311, desktop-design 2.8 and 4.7): every
  // file a tool run changed, with its diff. It slides over the window's right
  // part; the uncovered strip is dimmed. The heading says "Changes", the run
  // ("Ran 4 tools · 3 files · +12 −4"), and "· Loading…" while the saved
  // diffs load (reviews.go openReview). A file list on the left (kind
  // square, path, counts or kind) jumps to a file; each file's header
  // (chevron, kind badge, path, counts) folds it and stays pinned while its
  // diff scrolls; diffs over 600 lines start folded. Keys as the terminal's
  // (changes.go:81-89); Esc, Alt+Esc, Q, ✕ or a left click outside close it.
  // An expired or missing diff closes it with the terminal's expired notice;
  // other failures are reported; an answer after closing is ignored
  // (reviews.go applyReview).
  //   sessionId, messageId  the chat and the run's first tool message
  //   toolCallIds           the run's calls, in order
  //   title                 what made the changes ("Ran 4 tools")
  import { onDestroy, onMount, tick } from "svelte";
  import { drawer, scrim } from "../../lib/motion.js";
  import { closeDialog, frontDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { bindKey } from "../../lib/keys.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { messages } from "../../lib/stores/messages.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";
  import { size } from "../../lib/tools/nodes.js";
  import { addProbeReader } from "../../lib/tools/probe.js";
  import { kindName } from "../../lib/tools/review.js";
  import { withoutTestPaths } from "../../lib/testpaths.js";
  import {
    callsFromChat,
    EXPIRED_TEXT,
    FOOTER_KEYS,
    fileHead,
    isExpired,
    jumpTarget,
    kindTone,
    listStat,
    loadSheetFiles,
    SHEET_KEYS,
    shortPath,
    sourceLine,
    startsFolded,
    truncateLeft,
  } from "../../lib/tools/sheet.js";
  import Icon from "../Icon.svelte";
  import Rule from "../Rule.svelte";
  import DiffView from "../tools/DiffView.svelte";

  let { sessionId, messageId, toolCallIds = [], title = "" } = $props();

  // A body narrower than this many characters, or under one line, draws
  // nothing (changes.go Draw).
  const MIN_BODY_CELLS = 20;

  let files = $state(null); // null while loading
  let folded = $state([]);
  let panel = $state(null);
  let scroller = $state(null);
  let footerEl = $state(null);
  let sections = $state([]);
  let listWidth = $state(0);
  let bodyWidth = $state(0);
  let bodyHeight = $state(0);
  let cell = $state(0);
  let footerWidth = $state(0);
  let hintWidths = $state([]);
  let scrollTop = $state(0);
  let closed = false;

  const cwd = $derived(conn.project?.cwd ?? "");
  const source = $derived(files === null ? { text: `${title} · Loading…`, adds: null, dels: null } : sourceLine(title, files));
  // One line of the diff font is about three of its character widths.
  const tiny = $derived(cell > 0 && (bodyWidth / cell < MIN_BODY_CELLS || bodyHeight < cell * 3));
  // changes.go: a hint fits when it leaves 4 characters of the footer free.
  const hint = $derived.by(() => {
    if (!(cell > 0) || hintWidths.length !== FOOTER_KEYS.length) return FOOTER_KEYS[2];
    for (let i = 0; i < 2; i++) if (hintWidths[i] <= footerWidth - 4 * cell) return FOOTER_KEYS[i];
    return FOOTER_KEYS[2];
  });
  // The file list's paths, cut from the left to the room the row leaves.
  const listCells = $derived(cell > 0 ? Math.floor(listWidth / cell) : 40);

  function statText(file) {
    const stat = listStat(file);
    return stat.label ?? `+${stat.adds} −${stat.dels}`;
  }

  function listPath(file) {
    return truncateLeft(shortPath(file.path, cwd), Math.max(2, listCells - statText(file).length - 4));
  }

  function close() {
    closeDialog("changes");
  }

  // -- Loading (reviews.go openReview / applyReview) --

  onMount(async () => {
    const chat = messages.bySession[sessionId];
    const inputs = callsFromChat(chat, toolCallIds);
    try {
      const loaded = await loadSheetFiles(sessionId, inputs, (sid, mid) => api.review(sid, mid));
      if (closed) return;
      const shown = withoutTestPaths(loaded);
      folded = shown.map(startsFolded);
      files = shown;
    } catch (error) {
      if (closed) return;
      close();
      if (isExpired(error)) notify("info", EXPIRED_TEXT);
      else notify("error", String(error?.message ?? error));
    }
  });

  // -- Scrolling and keys (DLG-301 to DLG-305) --

  function lineHeight() {
    const row = scroller?.querySelector(".row");
    return row ? row.getBoundingClientRect().height : cell * 2;
  }

  function scrollBy(delta) {
    if (!scroller) return;
    scroller.scrollTop = Math.max(0, Math.min(scroller.scrollHeight - scroller.clientHeight, scroller.scrollTop + delta));
  }

  function heads() {
    if (!scroller) return [];
    const top = scroller.getBoundingClientRect().top - scroller.scrollTop;
    return sections.filter(Boolean).map((el) => Math.round(el.getBoundingClientRect().top - top));
  }

  function jump(dir) {
    if (!scroller) return;
    const target = jumpTarget(heads(), Math.round(scroller.scrollTop), dir);
    if (target !== null) scroller.scrollTop = target;
  }

  function jumpTo(i) {
    const el = sections[i];
    if (!scroller || !el) return;
    scroller.scrollTop = heads()[i] ?? 0;
  }

  function fold(i) {
    const top = heads()[i] ?? 0;
    folded[i] = !folded[i];
    // A pinned header that folds keeps its file in view.
    if (scroller && scroller.scrollTop > top) tick().then(() => (scroller.scrollTop = top));
  }

  const ACTIONS = {
    close,
    up: () => scrollBy(-lineHeight()),
    down: () => scrollBy(lineHeight()),
    pageUp: () => scrollBy(-Math.max(lineHeight(), (scroller?.clientHeight ?? 0) - lineHeight())),
    pageDown: () => scrollBy(Math.max(lineHeight(), (scroller?.clientHeight ?? 0) - lineHeight())),
    top: () => scroller && (scroller.scrollTop = 0),
    bottom: () => scroller && (scroller.scrollTop = scroller.scrollHeight),
    next: () => jump(1),
    prev: () => jump(-1),
  };

  const unbind = Object.entries(SHEET_KEYS).map(([combo, action]) =>
    bindKey("overlay", combo, () => {
      if (frontDialog() !== "changes") return false;
      ACTIONS[action]();
      return true;
    }),
  );

  // DLG-308: only a left click outside closes.
  function outside(event) {
    if (event.button === 0) close();
  }

  let returnFocus = null;
  onMount(() => {
    returnFocus = document.activeElement;
    panel?.focus({ preventScroll: true });
  });

  const stopProbe = addProbeReader(
    "sheet",
    Symbol("changes"),
    () => ({
      title: "Changes",
      source: source.text,
      totals: source.adds === null ? null : { adds: source.adds, dels: source.dels },
      loading: files === null,
      files: (files ?? []).map((f, i) => ({ path: f.path, list_path: listPath(f), kind: kindName(f.kind), stat: statText(f), folded: Boolean(folded[i]), lines: f.lines.length })),
      footer: hint,
      tiny,
      scroll_top: scrollTop,
      session_id: sessionId,
      message_id: messageId,
      tool_call_ids: toolCallIds,
    }),
    () => {
      const rect = (el) => {
        const r = el?.getBoundingClientRect();
        return r ? { x: r.x, y: r.y, w: r.width, h: r.height } : null;
      };
      const top = scroller?.getBoundingClientRect().top ?? 0;
      const headEls = scroller ? [...scroller.querySelectorAll(".file-head")] : [];
      const pinned = headEls.find((el) => Math.abs(el.getBoundingClientRect().top - top) < 1 && el.getBoundingClientRect().bottom > top);
      return {
        panel: rect(panel),
        scroller: rect(scroller),
        scroll_top: scroller?.scrollTop ?? 0,
        scroll_height: scroller?.scrollHeight ?? 0,
        client_height: scroller?.clientHeight ?? 0,
        line: lineHeight(),
        heads: heads(),
        head_rects: headEls.map(rect),
        pinned: pinned ? (pinned.dataset.path ?? "") : "",
        list_rows: scroller ? [...panel.querySelectorAll(".list-row")].map(rect) : [],
      };
    },
  );

  onDestroy(() => {
    closed = true;
    for (const off of unbind) off();
    stopProbe();
    if (returnFocus?.isConnected) returnFocus.focus?.({ preventScroll: true });
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="scrim" transition:scrim|global={{ to: 0.6 }} onclick={outside} oncontextmenu={(event) => event.preventDefault()}></div>
<div class="panel" role="dialog" aria-modal="true" aria-label="Changes" tabindex="-1" bind:this={panel} transition:drawer|global>
  <span class="ruler" aria-hidden="true" {@attach size((rect) => (cell = rect.width / 10))}>0000000000</span>
  <header class="heading">
    <div class="titles">
      <h2 class="title">Changes</h2>
      <p class="source">
        {source.text}{#if source.adds !== null}<span class="sep">{" · "}</span><span class="adds">+{source.adds}</span>
          <span class="dels">−{source.dels}</span>{/if}
      </p>
    </div>
    <button class="close" type="button" aria-label="Close" onclick={close}><Icon name="close" /></button>
  </header>
  <Rule marks="both" />
  <div
    class="body"
    {@attach size((rect) => {
      bodyWidth = rect.width;
      bodyHeight = rect.height;
    })}
  >
    {#if files !== null && !tiny}
      <nav class="files" aria-label="Changed files" {@attach size((rect) => (listWidth = rect.width))}>
        {#each files as file, i (file.path)}
          <button class="list-row" type="button" onclick={() => jumpTo(i)}>
            <span class="square {kindTone(file.kind)}" aria-hidden="true"></span>
            <span class="path">{listPath(file)}</span>
            {#if listStat(file).label}
              <span class="stat label">{listStat(file).label}</span>
            {:else}
              <span class="stat"><span class="adds">+{listStat(file).adds}</span> <span class="dels">−{listStat(file).dels}</span></span>
            {/if}
          </button>
        {/each}
      </nav>
      <div class="diffs" bind:this={scroller} onscroll={() => (scrollTop = scroller.scrollTop)}>
        {#each files as file, i (file.path)}
          {@const head = fileHead(file)}
          <section class="file" bind:this={sections[i]}>
            <button class="file-head" type="button" data-path={file.path} aria-expanded={!folded[i]} onclick={() => fold(i)}>
              <span class="chevron" class:open={!folded[i]}><Icon name="disclosure" size={10} /></span>
              <span class="badge {head.tone}">{head.badge}</span>
              <span class="file-path">{shortPath(file.path, cwd)}</span>
              {#if head.counts}<span class="counts"><span class="adds">+{head.counts.adds}</span> <span class="dels">−{head.counts.dels}</span></span>{/if}
            </button>
            {#if !folded[i]}
              <DiffView files={[file]} mode="unified" maxLines={0} lineNumbers={true} wrap={true} />
            {/if}
          </section>
        {/each}
      </div>
    {/if}
  </div>
  <footer class="footer" bind:this={footerEl} {@attach size((rect) => (footerWidth = rect.width))}>
    <span class="hint">{hint}</span>
    <span class="measure" aria-hidden="true">
      {#each FOOTER_KEYS as keys, i (i)}<span {@attach size((rect) => (hintWidths[i] = rect.width))}>{keys}</span>{/each}
    </span>
  </footer>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    background: var(--scrim);
    opacity: 0.6;
  }
  .panel {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    width: min(calc(1180 * var(--px)), calc(100% - 160 * var(--px)));
    background: var(--paper);
    border-left: 1px solid var(--rule-frame);
    outline: none;
  }
  .ruler {
    position: absolute;
    visibility: hidden;
    pointer-events: none;
    font: var(--t-mono);
    white-space: pre;
  }
  .heading {
    flex: none;
    display: flex;
    align-items: center;
    height: calc(72 * var(--px));
    padding: 0 calc(26 * var(--px));
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  .title {
    margin: 0;
    font: var(--t-project);
  }
  .source {
    margin: 0;
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .close {
    display: grid;
    place-items: center;
    width: calc(36 * var(--px));
    height: calc(36 * var(--px));
    color: var(--slate);
    cursor: pointer;
  }
  .close:hover {
    color: var(--ink);
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .files {
    flex: none;
    width: calc(260 * var(--px));
    overflow-y: auto;
    padding: calc(8 * var(--px)) 0;
    border-right: 1px solid var(--rule-faint);
    scrollbar-width: none;
  }
  .list-row {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    box-sizing: border-box;
    width: 100%;
    min-height: calc(32 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(26 * var(--px));
    text-align: left;
    cursor: pointer;
  }
  .list-row:hover {
    background: var(--band);
  }
  .square {
    flex: none;
    width: calc(8 * var(--px));
    height: calc(8 * var(--px));
    background: var(--figure);
  }
  .square.add {
    background: var(--diff-add-ink);
  }
  .square.del {
    background: var(--error-ink);
  }
  .path,
  .file-path {
    flex: 1;
    min-width: 0;
    font: var(--t-mono);
    white-space: pre;
    overflow: hidden;
  }
  .stat,
  .counts {
    flex: none;
    font: var(--t-mono);
    white-space: nowrap;
  }
  .stat.label {
    font: var(--t-small);
    color: var(--slate);
  }
  .adds {
    color: var(--diff-add-ink);
  }
  .dels {
    color: var(--error-ink);
  }
  .diffs {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .file + .file {
    margin-top: calc(16 * var(--px));
  }
  /* The current file's header stays at the top while its diff scrolls
     (DLG-306). */
  .file-head {
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    box-sizing: border-box;
    width: 100%;
    min-height: calc(40 * var(--px));
    padding: 0 calc(16 * var(--px)) 0 calc(12 * var(--px));
    background: var(--paper-raised);
    border-bottom: 1px solid var(--rule-faint);
    text-align: left;
    cursor: pointer;
  }
  .chevron {
    display: flex;
    color: var(--slate);
  }
  .chevron :global(.icon) {
    transition: transform var(--motion-quick) var(--ease-paper);
  }
  .chevron.open :global(.icon) {
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .chevron :global(.icon) {
      transition: none;
    }
  }
  .badge {
    flex: none;
    padding: 0 calc(6 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    color: var(--slate);
    background: var(--band);
  }
  .badge.add {
    color: var(--diff-add-ink);
    background: var(--diff-add-bg);
  }
  .badge.del {
    color: var(--error-ink);
    background: var(--diff-del-bg);
  }
  .footer {
    position: relative;
    flex: none;
    display: flex;
    align-items: center;
    height: calc(44 * var(--px));
    padding: 0 calc(26 * var(--px));
    border-top: 1px solid var(--rule-faint);
    font: var(--t-small);
    color: var(--slate);
    overflow: hidden;
  }
  .hint {
    white-space: nowrap;
  }
  .measure {
    position: absolute;
    visibility: hidden;
    pointer-events: none;
    white-space: nowrap;
  }
  .measure span {
    position: absolute;
  }
</style>
