<script>
  // The helper view (desktop-design-5-6 2.16, look 5.16; DLG-228, LIVE-005 to
  // LIVE-007, LIVE-021): a helper's own chat, read only, in the middle
  // column's place (S5's showMain) while the parent chat stays mounted
  // underneath; a port of internal/ui/model/subagent_view.go. It shows
  // "Opening conversation…" until the first read, then P3.5's Timeline with
  // readOnly. The child chat enters the stores through getSession,
  // upsertSession and load (subagent_view.go:75-91), and is read again at
  // most every 100 ms, never twice at once, while message or session events
  // for it or helper events for this helper arrive (:93-102, :163-177). A
  // failed read closes the view with the error (:125-128). Under the
  // transcript, in the typing box's place: the read-only band "<name> ·
  // <status>[ · <model>] · read only" (the model only while the right panel
  // shows, :276-283), the footer with the helper's model stats
  // (composer_footer.go:209-243), the tray line without its key hint
  // (:321-322) and "← Return to parent" with "esc" (:267, :324-328). Esc, the
  // button, a form taking the typing box's place or the shown chat changing
  // close it (:159-162). The keys are handleSubagentKey's (:337-372); pastes
  // are ignored (:192-193) and only Ctrl+C or y copy, never the release of a
  // drag (:221-224); the wheel scrolls this view, the PLAN section 12 fix.
  import { onMount, untrack } from "svelte";
  import Icon from "../Icon.svelte";
  import NoticeLine from "../NoticeLine.svelte";
  import Rule from "../Rule.svelte";
  import { baseName, pathLine } from "../start/startmodel.js";
  import Timeline from "../timeline/Timeline.svelte";
  import { CLI_KINDS, compactionLimit } from "../../lib/compaction.js";
  import { api } from "../../lib/engine/api.js";
  import { onEvent } from "../../lib/engine/events.js";
  import { footerFields, footerItems, footerText, limitWindows } from "../../lib/footer.js";
  import { bindKey } from "../../lib/keys.js";
  import { helperById, helpersLabel, probes, runningHelpers } from "../../lib/live/helpers.svelte.js";
  import { jobsLabel, trayJobs } from "../../lib/live/jobs.svelte.js";
  import { closeMain, mainView } from "../../lib/mainview.js";
  import { timelineRange } from "../../lib/selection.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { limits } from "../../lib/stores/limits.svelte.js";
  import { load, messages, unload } from "../../lib/stores/messages.svelte.js";
  import { sessions, upsertSession } from "../../lib/stores/sessions.svelte.js";
  import { notify, setFocus, ui } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { ticker } from "../../lib/ticker.svelte.js";

  let { helper } = $props();

  // The helper as its events keep it (2.16 State); the view is about this one.
  const id = untrack(() => helper.id);
  const childId = untrack(() => helper.child_session_id);
  const parentId = untrack(() => helper.session_id);
  const record = $derived(helperById(id) ?? helper);

  let root = $state(null);
  let transcript = $state(null);
  let band = $state(null);
  let back = $state(null);
  let timeline = $state(null);

  let opened = $state(false); // the first read landed
  let openingDrawn = false; // "Opening conversation…" was on the page (for the probe)
  let child = $state.raw(null); // the helper chat's session record (its token counts)
  let buttonFocused = $state(false);

  // -- Reading the helper's chat --

  let closed = false;
  let loading = false;
  let dirty = false;
  let queued = false;
  let refreshTimer = null;

  async function read() {
    loading = true;
    dirty = false;
    try {
      const session = await api.getSession(childId);
      if (closed) return;
      upsertSession(session, { fromEvent: false });
      child = session;
      await load(childId);
      if (closed) return;
      opened = true;
    } catch (error) {
      if (!closed) {
        close();
        notify("error", error?.message ?? String(error));
      }
      return;
    } finally {
      loading = false;
    }
    if (dirty) queueRefresh();
  }

  // Coalesces streaming events into one read per 100 ms, never two at once.
  function queueRefresh() {
    dirty = true;
    if (closed || loading || queued) return;
    queued = true;
    refreshTimer = setTimeout(() => {
      queued = false;
      if (!closed) read();
    }, 100);
  }

  function close() {
    const view = mainView.current;
    if (view?.kind === "helper" && view.props?.helper?.id === id) closeMain();
  }

  // A form in the typing box's place, or another chat shown, ends the view.
  $effect(() => {
    const shown = sessions.currentId;
    const slot = ui.slot;
    untrack(() => {
      if (shown !== parentId || slot) close();
    });
  });

  // -- What the band, footer and tray line say --

  const status = $derived(record.status || "running");
  const model = $derived(!ui.panelHidden && record.model ? record.model : "");
  const bandText = $derived(`${record.name ?? ""} · ${status}${model ? ` · ${model}` : ""} · read only`);
  const path = $derived(pathLine(baseName(conn.project.path), ws.branch));

  // subagentFooter: the helper's model, found among the providers its CLI
  // names, with its effort and fast tier, against the helper chat's counts.
  const footer = $derived.by(() => {
    const h = record;
    const providers = Array.isArray(ws.providers) ? ws.providers : [];
    const cfg = { model: h.model ?? "", reasoning_effort: h.effort ?? "", provider: "" };
    if (h.fast) cfg.service_tier = "fast";
    let found = { name: h.model ?? "" };
    let kind = "";
    for (const p of providers) {
      const cliName = String(p?.type ?? "")
        .replace(/-cli$/u, "")
        .replace(/-code$/u, "");
      if (h.cli !== p?.id && h.cli !== cliName) continue;
      found = (p.models ?? []).find((m) => m?.id === h.model) ?? found;
      cfg.provider = p.id;
      kind = p.type ?? "";
      break;
    }
    const agent = { model: found, model_cfg: cfg };
    const limit = compactionLimit({ agent, session: child, providers, options: ws.config?.options });
    const windows = CLI_KINDS.has(kind) ? (limitWindows(limits.byKind[kind], h.model ?? "", Date.now()) ?? []) : [];
    return footerText(footerFields({ items: footerItems(ws.config), agent, session: child, limit, windows }));
  });

  const helperCount = $derived(runningHelpers().length);
  const jobCount = $derived(trayJobs().length);
  const trayText = $derived(
    [helperCount > 0 ? helpersLabel(helperCount) : "", jobCount > 0 ? jobsLabel(jobCount) : ""].filter(Boolean).join("  "),
  );

  // -- Keys (handleSubagentKey) --

  function scroller() {
    return transcript?.querySelector(".scroller") ?? null;
  }

  function atBottom() {
    const s = scroller();
    return !s || s.scrollTop + s.clientHeight >= s.scrollHeight - 2;
  }

  function focusButton(on) {
    if (on) back?.focus({ preventScroll: true });
    else root?.focus({ preventScroll: true });
    buttonFocused = on;
  }

  // Enter or Space with the button not focused folds the item whose toggle
  // has the focus: the read-only timeline selects no item (S5's readOnly).
  function toggleFocused() {
    const el = document.activeElement;
    if (el && transcript?.contains(el) && el.matches("[aria-expanded]")) el.click();
  }

  // y copies what Ctrl+C copies: the view's Timeline copies its selection on
  // that key, with "Selected text copied to clipboard". Without a selection,
  // nothing.
  function copyKey() {
    const section = transcript?.querySelector("section.timeline");
    if (!section || !timelineRange(section)) return;
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "c", code: "KeyC", ctrlKey: true, bubbles: true, cancelable: true }));
  }

  const KEYS = [
    [["Escape"], close],
    [["Tab", "Shift+Tab", "ArrowLeft", "ArrowRight"], () => focusButton(!buttonFocused)],
    [
      ["Enter", "Space"],
      () => {
        if (buttonFocused) close();
        else toggleFocused();
      },
    ],
    [
      ["ArrowUp", "k"],
      () => {
        focusButton(false);
        timeline?.scrollLines(-1);
      },
    ],
    [
      ["ArrowDown", "j"],
      () => {
        if (atBottom()) focusButton(true);
        else timeline?.scrollLines(1);
      },
    ],
    [["PageUp", "Ctrl+U"], () => timeline?.scrollPages(-0.5)],
    [["PageDown", "Ctrl+D"], () => timeline?.scrollPages(0.5)],
    [["Home"], () => timeline?.scrollLines(-1_000_000)],
    [["End"], () => timeline?.scrollEnd()],
    [["y"], copyKey],
    // Pastes are ignored (LIVE-021).
    [["Ctrl+V", "Shift+Insert", "Ctrl+Shift+V"], () => {}],
  ];

  function mine() {
    const view = mainView.current;
    return view?.kind === "helper" && view.props?.helper?.id === id;
  }

  // -- Mouse --

  // Releasing a drag selection must not copy (LIVE-021), and the Timeline
  // copies 400 ms after any primary press it saw ends. A primary press on the
  // text is kept from it here, before it gets there; the text selection is the
  // page's own and goes on, and item toggles act on the click. Presses on
  // links, pictures, buttons and the scrollbar go through, as their own press
  // handlers tell a click from a drag (render/links.js, FigureView, Chip,
  // VirtualList).
  function keepPresses(node) {
    const press = (event) => {
      if (event.button !== 0) return;
      const target = event.target?.nodeType === 1 ? event.target : event.target?.parentElement;
      if (!target || target.closest("a, img, button, input, textarea, select, [role='button']")) return;
      if (event.offsetX >= target.clientWidth && target.scrollHeight > target.clientHeight) return;
      event.stopPropagation();
    };
    node.addEventListener("mousedown", press, true);
    return {
      destroy() {
        node.removeEventListener("mousedown", press, true);
      },
    };
  }

  function drawn() {
    openingDrawn = true;
  }

  function takeFocus() {
    if (ui.focus !== "main") setFocus("main");
  }

  // -- Probe --

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  function wordsInView(limit = 300) {
    const s = scroller();
    if (!s) return [];
    const view = s.getBoundingClientRect();
    const out = [];
    const range = document.createRange();
    const walker = document.createTreeWalker(s, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node && out.length < limit; node = walker.nextNode()) {
      for (const word of node.data.matchAll(/\S+/gu)) {
        range.setStart(node, word.index);
        range.setEnd(node, word.index + word[0].length);
        const r = range.getBoundingClientRect();
        if (!r.width || !r.height || r.bottom <= view.top || r.top >= view.bottom) continue;
        out.push({ text: word[0], rect: { x: r.x, y: r.y, w: r.width, h: r.height } });
        if (out.length >= limit) break;
      }
    }
    return out;
  }

  onMount(() => {
    root?.focus({ preventScroll: true });
    const off = [
      onEvent("message", (_kind, m) => {
        if (m?.session_id === childId) queueRefresh();
      }),
      onEvent("session", (_kind, s) => {
        if (s?.id === childId) queueRefresh();
      }),
      onEvent("helper", (_kind, h) => {
        if (h?.id === id) queueRefresh();
      }),
    ];
    for (const [combos, fn] of KEYS) {
      for (const combo of combos) {
        off.push(
          bindKey("main", combo, () => {
            if (!mine()) return false;
            fn();
            return true;
          }),
        );
      }
    }
    const probe = {
      light: () => {
        const s = scroller();
        return {
          helper_id: id,
          child_session_id: childId,
          state: opened ? "shown" : "opening",
          opening_drawn: openingDrawn,
          title: record.name ?? "",
          path,
          band: bandText,
          status,
          footer,
          tray: trayText,
          button: "← Return to parent",
          button_focused: buttonFocused,
          focused: ui.focus === "main",
          messages: (messages.bySession[childId]?.ids ?? []).length,
          scroll: s ? { top: Math.round(s.scrollTop), height: s.scrollHeight, client: s.clientHeight, bar: s.offsetWidth - s.clientWidth } : null,
        };
      },
      heavy: () => {
        const section = transcript?.querySelector("section.timeline");
        const range = section ? timelineRange(section) : null;
        return {
          view: rectOf(root),
          title: rectOf(root?.querySelector(".title")),
          band: rectOf(band),
          button: rectOf(back),
          transcript: rectOf(scroller()),
          words: wordsInView(),
          links: [...(scroller()?.querySelectorAll("a[href]") ?? [])].map((a) => ({ text: a.textContent, href: a.getAttribute("href"), rect: rectOf(a) })),
          toggles: [...(scroller()?.querySelectorAll("[aria-expanded]") ?? [])].map((el) => ({
            text: el.textContent.trim().slice(0, 80),
            expanded: el.getAttribute("aria-expanded") === "true",
            focused: el === document.activeElement,
            rect: rectOf(el),
          })),
          selection: range ? range.toString() : null,
        };
      },
    };
    probes.view = probe;
    read();
    return () => {
      closed = true;
      clearTimeout(refreshTimer);
      for (const fn of off) fn();
      if (probes.view === probe) probes.view = null;
      unload(childId);
    };
  });
</script>

<!-- The view takes the keys through the "main" scope; the root holds the focus. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<main
  class="helper-view"
  bind:this={root}
  tabindex="-1"
  aria-label="{record.name ?? ''}, read only"
  data-helper-view={id}
  onpaste={(event) => event.preventDefault()}
  onpointerdown={takeFocus}
>
  <header class="head">
    <h2 class="title">{record.name ?? ""}</h2>
    {#if path}<p class="path" title={conn.project.path}>{path}</p>{/if}
  </header>

  <Rule marks="both" class="under-head" />

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="transcript" bind:this={transcript} use:keepPresses onpointerdown={() => (buttonFocused = false)}>
    {#if opened}
      <Timeline sessionId={childId} readOnly bind:this={timeline} />
    {:else}
      <p class="opening" use:drawn><Icon name="working" size={18} /><span>Opening conversation…</span></p>
    {/if}
  </div>

  <NoticeLine />

  <Rule marks="both" class="above-composer" />

  <div class="dock">
    <div class="band" bind:this={band} data-helper-band>
      <span class="name">{record.name ?? ""}</span>
      <span class="sep">·</span>
      <span class="status {status}">
        {#if status === "done"}<Icon name="check" size={18} />{:else if status === "running"}<Icon name="working" size={18} />{/if}
        <span>{status}</span>
      </span>
      {#if model}<span class="sep">·</span><span class="rest">{model}</span>{/if}
      <span class="sep">·</span>
      <span class="rest">read only</span>
    </div>
    {#if footer}<p class="line footer">{footer}</p>{/if}
    {#if helperCount > 0 || jobCount > 0}
      <p class="line tray">
        {#if helperCount > 0}
          <span class="entry"><span class="mark busy" style:transform="rotate({90 * ticker.second}deg)"><Icon name="half-circle" size={18} /></span>{helpersLabel(helperCount)}</span>
        {/if}
        {#if jobCount > 0}
          <span class="entry"><span class="mark"><Icon name="gear" size={18} /></span>{jobsLabel(jobCount)}</span>
        {/if}
      </p>
    {/if}
    <div class="back-row">
      <button
        type="button"
        class="back"
        bind:this={back}
        onclick={close}
        onfocus={() => (buttonFocused = true)}
        onblur={() => (buttonFocused = false)}>← Return to parent</button
      >
      <span class="esc">esc</span>
    </div>
  </div>
</main>

<style>
  .helper-view {
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    outline: none;
  }
  .helper-view :global(.rule.under-head),
  .helper-view :global(.rule.above-composer) {
    flex: none;
    z-index: 1;
  }

  /* The chat's head, as ChatHead draws it, so nothing moves when the view opens. */
  .head {
    flex: none;
    box-sizing: border-box;
    min-width: 0;
    height: calc(121 * var(--px));
    padding: calc(17 * var(--px)) calc(30 * var(--px)) 0;
  }
  .title {
    min-height: calc(50 * var(--px));
    margin: 0;
    font: var(--t-title);
    letter-spacing: -0.02em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    margin: calc(11 * var(--px)) 0 0;
    font: var(--t-path);
    letter-spacing: var(--track-path);
    text-transform: uppercase;
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .transcript {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  /* Loading (look 5.0): the working mark, then the text, 10 apart. */
  .opening {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    margin: 0;
    padding: calc(20 * var(--px)) calc(30 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }

  /* The typing box's place: the composer's own padding (Composer.svelte). */
  .dock {
    flex: none;
    box-sizing: border-box;
    padding: calc(15 * var(--px)) calc(20 * var(--px)) calc(25 * var(--px));
  }
  /* The read-only band: 44 high, text from 14 in (look 5.16); never focused. */
  .band {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-width: 0;
    height: calc(44 * var(--px));
    padding: 0 calc(14 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
  }
  .band .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--ink);
  }
  .status {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: calc(6 * var(--px));
    color: var(--ink);
  }
  .status.failed {
    color: var(--error-ink);
  }
  .status.stopped {
    color: var(--slate);
  }
  .sep,
  .rest {
    flex: none;
  }

  /* The footer and the tray line, 8 below the band, at its text start. */
  .line {
    margin: calc(8 * var(--px)) 0 0;
    padding-left: calc(14 * var(--px) + 1px);
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .line + .line {
    margin-top: 0;
  }
  .tray {
    display: flex;
    gap: calc(24 * var(--px));
    color: var(--ink);
  }
  .entry {
    display: inline-flex;
    align-items: center;
    gap: calc(8 * var(--px));
  }
  .mark {
    display: inline-grid;
    place-items: center;
    color: var(--slate);
  }
  .mark.busy {
    color: var(--busy);
  }

  /* "← Return to parent", then "esc", 12 below, at the band's text start. */
  .back-row {
    display: flex;
    align-items: baseline;
    gap: calc(12 * var(--px));
    margin-top: calc(12 * var(--px));
    padding-left: calc(14 * var(--px) + 1px);
  }
  .back {
    padding: 0;
    font: var(--t-button);
    color: var(--ink);
  }
  .back:hover {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .esc {
    font: var(--t-agent-mono);
    color: var(--slate);
  }
</style>
