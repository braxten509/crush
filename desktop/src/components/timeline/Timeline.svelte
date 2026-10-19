<script module>
  // The probe's chat section (desktop-design 1.9): the mounted timeline fills
  // it; with no timeline it says no chat is shown.
  import { probeSection } from "../../lib/probe.js";

  let shownProbe = null; // {light(), heavy()} of the mounted timeline
  const NO_CHAT = { id: "", title: "", path: "", busy: false, follow: true, status: "", scroll_offset: 0, mounted: 0, items: [], messages: [] };
  probeSection(
    "chat",
    () => shownProbe?.light() ?? NO_CHAT,
    () => shownProbe?.heavy() ?? {},
  );
</script>

<script>
  // The timeline of the chat {sessionId} on its vermilion rail (desktop-design
  // 2.1, 2.2, 1.6, 1.8): the chat's messages, this window's prompts on their
  // way and its live `!` commands become items (lib/timeline.js), drawn by the
  // virtual list. The timeline owns the selected item (ui.selectedItem), the
  // page's text selection inside it, its keys (the timeline scope), the
  // right-click menu's opening and the copies (lib/copy.js).
  //
  // Ports: the chat keys of internal/ui/model/keypress.go:462-508 with
  // chat_scroll.go's moves; a wheel scroll's selection rule
  // (applyChatScroll, transcript.go:14-28); Tab from the chat
  // (keypress.go:516-518). Desktop rules: CHAT-087 to CHAT-101.
  //
  // The rail runs from under the head to the top edge of the composer block,
  // measured from the layout: the end follows `.rule.above-composer`'s bottom
  // edge (the composer block starts there), so a taller composer (chips
  // queued, a slot form) never lets the rail run into it.
  //
  // readOnly (a helper's chat in the main column, desktop-design-5-6 7.1):
  // no rail, no item selection or letters, no right-click menu (so no entry
  // that changes the chat), no timeline keys and no focus hand-off to the
  // typing box; it follows its own bottom, apart from the window's timeline.
  // The owner scrolls it with scrollLines(n), scrollPages(n) and scrollEnd().
  import { flushSync, onMount, untrack } from "svelte";
  import { runAction } from "../../lib/actions.js";
  import { COPIED_SELECTION, copySelection, itemCopy, writeClipboard } from "../../lib/copy.js";
  import { bindKey } from "../../lib/keys.js";
  import { linkGuard } from "../../lib/render/links.js";
  import {
    carrySelection,
    expandedOf,
    isSelectable,
    lastSelectable,
    nextSelectable,
    selectAcross,
    selectionEnds,
    timelineRange,
    toggleOf,
  } from "../../lib/selection.js";
  import { composer, focusField, interruptAndSend, stop } from "../../lib/stores/composer.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { linksFor } from "../../lib/stores/links.svelte.js";
  import { isFinished, messages, pendingFor } from "../../lib/stores/messages.svelte.js";
  import { prompts } from "../../lib/stores/prompts.svelte.js";
  import { busy, sessions } from "../../lib/stores/sessions.svelte.js";
  import { clearScrollTo, openMenu, selectItem, setFocus, setFollow, ui } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { thoughtLineEnabled } from "../../lib/thought.js";
  import { buildTimeline, createTimelineState } from "../../lib/timeline.js";
  import { focusTray } from "../../lib/tray.js";
  import VirtualList from "./VirtualList.svelte";

  let { sessionId, readOnly = false } = $props();

  // A single click on an item's toggle waits this long for a second click
  // (CHAT-101); a mouse selection is copied this long after it ends (CHAT-091).
  const DOUBLE_CLICK_MS = 400;
  const DRAG_SLOP = 4;
  const READING_LINE = 33; // --t-reading's line, picture px: ↑/k and ↓/j scroll one (CHAT-096)

  let root = $state(null); // the timeline's section
  let list = $state(null); // the VirtualList
  let pins = $state.raw([]); // the keys holding the text selection's ends (CHAT-087)
  let tick = $state(0); // bumped when a status word expires ("Responding", CHAT-019)
  let ownFollow = $state(true); // readOnly: this timeline's own follow, apart from ui.follow
  const follow = $derived(readOnly ? ownFollow : ui.follow);

  function setFollowed(on) {
    if (readOnly) ownFollow = on;
    else setFollow(on);
  }

  // -- Items --

  const chat = $derived(sessionId ? (messages.bySession[sessionId] ?? null) : null);
  // The opening screen takes the timeline's place while a chat opens (LIST-034).
  const shown = $derived(Boolean(sessionId) && !sessions.opening && chat?.status === "ready");

  let kept = { sessionId: null, state: null };
  function stateFor(sid) {
    if (kept.sessionId !== sid) kept = { sessionId: sid, state: createTimelineState() };
    return kept.state;
  }

  const pending = $derived(shown ? pendingFor(sessionId) : []);
  // This window's send marks the chat busy at once (send.go:114-121), so the
  // prompt gets its waiting status before the engine's first event.
  const isBusy = $derived(shown && (busy(sessionId) || pending.some((p) => p.state === "working")));
  const built = $derived.by(() => {
    tick;
    if (!shown) return { items: [], status: "", refreshAt: Infinity };
    return buildTimeline(
      {
        sessionId,
        ids: chat.ids,
        byId: chat.byId,
        pending,
        shells: Object.values(composer.shells),
        busy: isBusy,
        planMode: composer.mode === "plan",
        callMarks: prompts.callStatus,
        providers: ws.providers,
        thoughtLine: thoughtLineEnabled(ws.config),
        hasKey: (mid) => linksFor(mid).length > 0, // phase 4: items holding a branch link get a key
        now: Date.now(),
      },
      stateFor(sessionId),
    );
  });
  const items = $derived(built.items);
  // What the virtual list draws: readOnly items carry no key letters.
  const drawn = $derived(readOnly ? items.map((item) => (item.letter ? { ...item, letter: "" } : item)) : items);
  const indexOf = $derived(new Map(items.map((item, i) => [item.key, i])));
  const scrollbar = $derived(ws.config?.options?.tui?.scrollbar ?? "default");
  const focusOnly = $derived(ws.config?.options?.tui?.composer_focus_only === true);
  // The one item Tab reaches (a roving tab stop): the selected one, else the last.
  const tabKey = $derived.by(() => {
    if (ui.selectedItem && indexOf.has(ui.selectedItem)) return ui.selectedItem;
    const i = lastSelectable(items);
    return i >= 0 ? items[i].key : null;
  });

  // A status word that expires ("Responding" turns back into "Thinking" 5 s
  // after the last activity) rebuilds the items then.
  $effect(() => {
    const at = built.refreshAt;
    if (!Number.isFinite(at)) return;
    const timer = setTimeout(() => (tick += 1), Math.max(0, at - Date.now()) + 20);
    return () => clearTimeout(timer);
  });

  // Another chat: nothing selected, and its bottom in view (CHAT-105).
  $effect.pre(() => {
    sessionId;
    untrack(() => {
      setFollowed(true);
      if (!readOnly && ui.selectedItem) selectItem(null);
      pins = [];
    });
  });

  // A selected item that left the timeline: the item that took it over (a
  // group holding a tool), else none.
  $effect(() => {
    const list = items;
    const key = ui.selectedItem;
    if (readOnly || !key || !shown) return;
    untrack(() => {
      const next = carrySelection(key, list);
      if (next !== key) selectItem(next);
    });
  });

  // -- Focus (1.8, TYPE-022, SIDE-012) --

  let lastFocus = ui.focus;
  $effect(() => {
    const focus = ui.focus;
    const key = ui.selectedItem;
    const ready = shown && list !== null;
    untrack(() => {
      const before = lastFocus;
      lastFocus = focus;
      if (readOnly || focus !== "timeline" || !ready) return;
      const inside = root?.contains(document.activeElement);
      // Tab from the typing box selects the last item (keypress.go:262-267);
      // focus coming back from the panel keeps the selection.
      if (!key || (before === "composer" && !inside)) {
        const i = lastSelectable(items);
        if (i >= 0 && items[i].key !== key) {
          selectItem(items[i].key);
          return;
        }
      }
      // A press focuses the item it lands on by itself.
      if (key && !press) queueMicrotask(() => focusFrame(key));
    });
  });

  function focusFrame(key) {
    if (ui.focus !== "timeline" || ui.selectedItem !== key || !list) return;
    let frame = list.frameOf(key);
    if (!frame) {
      list.scrollToKey(key);
      frame = list.frameOf(key);
    }
    if (frame && !frame.contains(document.activeElement)) frame.focus({ preventScroll: true });
  }

  // With composer_focus_only (SET-003) a click selects and opens items, but
  // focus goes back to the typing box once the press (and its copy) is over.
  let refocusAfterPress = false;
  function onFocusIn() {
    if (readOnly) return;
    if (focusOnly) {
      if (press) refocusAfterPress = true;
      else focusField();
      return;
    }
    if (ui.focus !== "timeline") setFocus("timeline");
  }

  // -- Mouse: select, open, copy (CHAT-091, CHAT-101) --

  let press = null; // {x, y, dragged} of the primary press that started in the timeline
  let copyTimer = null;
  let held = null; // {toggle, timer}: a single click waiting to open or close an item
  let replaying = false;

  function keyAt(target) {
    const node = target?.nodeType === 1 ? target : target?.parentElement;
    const key = node?.closest?.("[data-key]")?.dataset.key ?? null;
    return key && indexOf.has(key) ? key : null;
  }

  function onMouseDown(event) {
    if (event.button === 0) press = { x: event.clientX, y: event.clientY, dragged: false };
    if (readOnly || (event.button !== 0 && event.button !== 2)) return;
    const key = keyAt(event.target);
    if (key && isSelectable(items[indexOf.get(key)]) && ui.selectedItem !== key) selectItem(key);
  }

  function onMouseMove(event) {
    if (!press || press.dragged || (event.buttons & 1) === 0) return;
    if (Math.hypot(event.clientX - press.x, event.clientY - press.y) > DRAG_SLOP) press.dragged = true;
  }

  function onMouseUp(event) {
    if (event.button !== 0 || !press) return;
    press = null;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copyTimer = null;
      const range = timelineRange(root);
      if (range) {
        const text = copySelection(range, items);
        if (text) writeClipboard(text, COPIED_SELECTION);
      }
      if (refocusAfterPress) {
        refocusAfterPress = false;
        focusField();
      }
    }, DOUBLE_CLICK_MS);
  }

  function selectionCollapsed() {
    const selection = document.getSelection();
    return !selection || selection.isCollapsed;
  }

  // An item's toggle (its first control with aria-expanded, drawn by the
  // item) opens or closes on a single click: the click waits 400 ms, and a
  // second click (a double click selecting a word) cancels it. A click made
  // by a key (detail 0) acts at once.
  function onClickCapture(event) {
    if (replaying) return;
    const toggle = event.target?.closest?.("[aria-expanded]");
    if (!toggle || !root.contains(toggle)) return;
    const control = event.target.closest("button, a, input, textarea, select, [role='button']");
    if (control && control !== toggle && toggle.contains(control)) return;
    if (event.detail === 0) return;
    event.stopPropagation();
    event.preventDefault();
    if (held) {
      clearTimeout(held.timer);
      held = null;
      return;
    }
    if (event.detail > 1 || press?.dragged || !selectionCollapsed()) return;
    held = {
      toggle,
      timer: setTimeout(() => {
        held = null;
        clickToggle(toggle);
      }, DOUBLE_CLICK_MS),
    };
  }

  function clickToggle(toggle) {
    replaying = true;
    try {
      toggle.click();
    } finally {
      replaying = false;
    }
  }

  // -- The text selection's two ends stay mounted (1.6 item 8, CHAT-087) --

  function onSelectionChange() {
    const { startKey, endKey } = selectionEnds(timelineRange(root));
    const next = [...new Set([startKey, endKey].filter((key) => key && indexOf.has(key)))];
    if (next.join("\n") !== pins.join("\n")) pins = next;
  }

  // -- The right-click menu (4.2) --

  function messageOf(item) {
    return item?.messageId ? (chat?.byId.get(item.messageId) ?? null) : null;
  }

  function openMenuAt(x, y, key) {
    const item = key ? items[indexOf.get(key)] : null;
    const range = timelineRange(root);
    // The selection's text is read now: the menu's own press may move it.
    openMenu({
      x,
      y,
      ctx: { sessionId, key, item, message: messageOf(item), range: range ? range.cloneRange() : null, text: range ? copySelection(range, items) : "", items },
    });
  }

  function onContextMenu(event) {
    event.preventDefault();
    if (!shown || readOnly) return;
    const key = keyAt(event.target);
    if (key && isSelectable(items[indexOf.get(key)])) selectItem(key);
    openMenuAt(event.clientX, event.clientY, key);
  }

  // Shift+F10 or the Menu key: the menu on the selected item.
  function menuOnSelected() {
    const key = ui.selectedItem;
    if (!key || !indexOf.has(key)) return false;
    let frame = list.frameOf(key);
    if (!frame) {
      list.scrollToKey(key);
      flushSync();
      frame = list.frameOf(key);
    }
    if (!frame) return false;
    const r = frame.getBoundingClientRect();
    openMenuAt(r.left, r.top + Math.min(r.height, 24 * zoom()), key);
    return true;
  }

  // -- Keys (CHAT-096 to CHAT-100; 1.8) --

  function zoom() {
    const size = parseFloat(getComputedStyle(document.documentElement).fontSize);
    return Number.isFinite(size) && size > 0 ? size / 16 : 1;
  }

  // ScrollBy (chat_scroll.go:66-77): up turns following off; down turns it on
  // at the bottom.
  function scrollBy(dy) {
    const bottom = list.scrollByPx(dy);
    if (dy < 0) setFollowed(false);
    else if (bottom) setFollowed(true);
  }

  function selectedIndex() {
    const key = ui.selectedItem;
    return key && indexOf.has(key) ? indexOf.get(key) : -1;
  }

  function select(i) {
    if (i >= 0 && i < items.length && items[i].key !== ui.selectedItem) selectItem(items[i].key);
  }

  // ScrollToSelected (chat_scroll.go:79-84, list/scroll.go:151-191): only an
  // item out of view scrolls in; following is then on exactly at the bottom.
  function showSelected() {
    const key = ui.selectedItem;
    if (key && !list.keyInView(key)) list.scrollToKey(key);
    setFollowed(list.atEnd());
  }

  function selectStep(dir) {
    const i = selectedIndex();
    const next = i < 0 ? (dir < 0 ? lastSelectable(items) : -1) : nextSelectable(items, i, dir);
    if (next >= 0) select(next);
  }

  function selectFirstInView() {
    for (const key of list.keysInView()) {
      if (isSelectable(items[indexOf.get(key)])) return select(indexOf.get(key));
    }
  }

  function selectLastInView() {
    const keys = list.keysInView();
    for (let k = keys.length - 1; k >= 0; k--) {
      if (isSelectable(items[indexOf.get(keys[k])])) return select(indexOf.get(keys[k]));
    }
  }

  function selectLast() {
    select(lastSelectable(items));
  }

  // A scroll by the user (wheel, bar, drag): an out-of-view selection moves to
  // the visible edge nearest it (applyChatScroll and SelectNearestInView,
  // transcript.go:14-28, chat_scroll.go:249-272). With nothing selected only
  // a focused timeline picks one.
  function onScrolled({ up }) {
    if (!list || readOnly) return;
    const key = ui.selectedItem;
    if (key && list.keyInView(key)) return;
    if (!key && ui.focus !== "timeline") return;
    if (!up && list.atEnd()) return selectLast();
    const i = selectedIndex();
    if (i < 0) return up ? selectLastInView() : selectFirstInView();
    const first = list.keysInView()[0];
    if (first !== undefined && i < indexOf.get(first)) selectFirstInView();
    else selectLastInView();
  }

  function onFollow(on) {
    setFollowed(on);
  }

  // The focused control inside an item (a link, a tool's button) keeps its own keys.
  function controlFocused() {
    const el = document.activeElement;
    return Boolean(el && root?.contains(el) && el.matches("a[href], button, input, textarea, select, [role='button']"));
  }

  function copySelected() {
    const i = selectedIndex();
    if (i < 0) return false;
    const copy = itemCopy(items[i], messageOf(items[i]));
    if (!copy) return false;
    writeClipboard(copy.text, copy.notice);
    return true;
  }

  function toggleSelected() {
    if (controlFocused()) return false;
    const toggle = toggleOf(list.frameOf(ui.selectedItem));
    if (toggle) clickToggle(toggle);
    return true;
  }

  // Ctrl+A in the timeline: from the first item's start to the last item's
  // end, mounting only those two (1.6 item 8).
  function selectAll() {
    if (items.length === 0) return false;
    const first = items[0].key;
    const last = items[items.length - 1].key;
    pins = [...new Set([first, last])];
    flushSync();
    return selectAcross(list.frameOf(first), list.frameOf(last));
  }

  function copyTimelineSelection() {
    const range = timelineRange(root);
    if (!range) return false;
    const text = copySelection(range, items);
    if (!text) return false;
    writeClipboard(text, COPIED_SELECTION);
    return true;
  }

  // Esc: clear the text selection; with none, while busy, stop the run
  // (PLAN.md:194 makes the "esc clear selection" hint real).
  function escape() {
    if (timelineRange(root)) {
      document.getSelection()?.removeAllRanges();
      return true;
    }
    if (isBusy) {
      stop();
      return true;
    }
    return false;
  }

  function tabOut() {
    if (!focusTray()) {
      setFocus("composer");
      focusField();
    }
    return true;
  }

  // Each handler runs only while a chat shows; a false lets the key go on.
  function chatKey(fn) {
    return (event) => (shown && list ? fn(event) !== false : false);
  }

  const line = () => READING_LINE * zoom();
  const page = () => list.viewportHeight();

  const TIMELINE_KEYS = [
    // One reading line; the selection follows the edge item (keypress.go:462-475).
    [["ArrowUp", "k"], () => {
      scrollBy(-line());
      if (!ui.selectedItem || !list.keyInView(ui.selectedItem)) {
        selectStep(-1);
        showSelected();
      }
    }],
    [["ArrowDown", "j"], () => {
      scrollBy(line());
      if (!ui.selectedItem || !list.keyInView(ui.selectedItem)) {
        selectStep(1);
        showSelected();
      }
    }],
    // One item (keypress.go:476-483).
    [["Shift+ArrowUp", "K"], () => {
      selectStep(-1);
      showSelected();
    }],
    [["Shift+ArrowDown", "J"], () => {
      selectStep(1);
      showSelected();
    }],
    // Half a page and a page (keypress.go:484-499).
    [["u"], () => {
      scrollBy(-page() / 2);
      selectFirstInView();
    }],
    [["d"], () => {
      scrollBy(page() / 2);
      selectLastInView();
    }],
    [["b", "PageUp"], () => {
      scrollBy(-page());
      selectFirstInView();
    }],
    [["f", "PageDown"], () => {
      scrollBy(page());
      selectLastInView();
    }],
    // Top and bottom (keypress.go:500-506).
    [["g", "Home"], () => {
      list.scrollToTop();
      setFollowed(false);
      const i = items.findIndex(isSelectable);
      select(i);
    }],
    [["G", "End"], () => {
      setFollowed(true);
      list.scrollToBottom();
      selectLast();
    }],
    [["Space"], toggleSelected],
    [["c", "y", "C", "Y"], copySelected],
    [["Escape"], escape],
    [["Ctrl+A"], selectAll],
    [["ContextMenu", "Shift+F10"], menuOnSelected],
    [["Tab"], tabOut],
    [["Shift+Tab"], () => {
      runAction("mode.cycle");
    }],
    [["Ctrl+Enter"], () => {
      interruptAndSend();
    }],
  ];

  // Ctrl+End: the bottom, following (CHAT-098), from the timeline and the typing box.
  function jumpToEnd() {
    setFollowed(true);
    list.scrollToBottom();
  }

  // For a readOnly timeline's owner (LIVE's helper view, desktop-design-5-6 7.1):
  // n reading lines or pages (negative: up), and the bottom, following.
  export function scrollLines(n) {
    if (shown && list) scrollBy(n * line());
  }

  export function scrollPages(n) {
    if (shown && list) scrollBy(n * page());
  }

  export function scrollEnd() {
    if (shown && list) jumpToEnd();
  }

  // -- The rail's end --

  let railBottom = $state(null); // CSS px from the column's bottom to the composer block's top

  function measureRail() {
    const column = root?.parentElement;
    const rule = column?.querySelector(":scope > .rule.above-composer");
    if (!column || !rule) {
      railBottom = null;
      return;
    }
    const bottom = column.clientHeight - (rule.offsetTop + rule.offsetHeight);
    if (bottom !== railBottom) railBottom = bottom;
  }

  let rail = $state(null);

  onMount(() => {
    const column = root.parentElement;
    const sizes = new ResizeObserver(measureRail);
    const watch = () => {
      sizes.disconnect();
      sizes.observe(column);
      for (const part of column.children) if (part !== rail) sizes.observe(part);
    };
    const parts = new MutationObserver(() => {
      watch();
      measureRail();
    });
    parts.observe(column, { childList: true });
    watch();
    measureRail();

    const unbind = [];
    if (!readOnly) {
      for (const [combos, fn] of TIMELINE_KEYS) for (const combo of combos) unbind.push(bindKey("timeline", combo, chatKey(fn)));
      for (const scope of ["timeline", "composer"]) unbind.push(bindKey(scope, "Ctrl+End", chatKey(jumpToEnd)));
    }
    unbind.push(bindKey("global", "Ctrl+C", chatKey(copyTimelineSelection)));

    document.addEventListener("selectionchange", onSelectionChange);
    window.addEventListener("mouseup", onMouseUp, true);
    window.addEventListener("mousemove", onMouseMove, true);
    if (!readOnly) shownProbe = { light: probeLight, heavy: probeHeavy };
    return () => {
      sizes.disconnect();
      parts.disconnect();
      for (const fn of unbind) fn();
      document.removeEventListener("selectionchange", onSelectionChange);
      window.removeEventListener("mouseup", onMouseUp, true);
      window.removeEventListener("mousemove", onMouseMove, true);
      clearTimeout(copyTimer);
      if (held) clearTimeout(held.timer);
      if (shownProbe?.light === probeLight) shownProbe = null;
    };
  });

  // The page's own listeners (not Svelte's delegated ones), so their order
  // against the link guard and the items' own handlers is the page's.
  function wire(node) {
    const on = [
      ["mousedown", onMouseDown, false],
      ["click", onClickCapture, true],
      ["focusin", onFocusIn, false],
      ["contextmenu", onContextMenu, false],
    ];
    for (const [type, fn, capture] of on) node.addEventListener(type, fn, capture);
    return {
      destroy() {
        for (const [type, fn, capture] of on) node.removeEventListener(type, fn, capture);
      },
    };
  }

  // -- Scroll requests (4.3: a figure heading shows its chip; ui.scrollTo) --

  $effect(() => {
    const target = ui.scrollTo;
    if (readOnly || !target || !shown || !list) return;
    untrack(() => {
      if (target.sessionId === sessionId) reveal(target);
      clearScrollTo();
    });
  });

  function reveal({ messageId, figureId }) {
    const item =
      items.find((it) => it.messageId === messageId && (it.kind === "reply" || it.kind === "user")) ?? items.find((it) => it.messageId === messageId);
    if (!item) return;
    setFollowed(false);
    list.scrollKeyToTop(item.key);
    if (!figureId) return;
    const chip = [...(list.frameOf(item.key)?.querySelectorAll("[data-figure]") ?? [])].find((el) => el.dataset.figure === figureId);
    if (!chip) return;
    const view = list.element().getBoundingClientRect();
    const r = chip.getBoundingClientRect();
    if (r.top < view.top || r.bottom > view.bottom) scrollBy(r.top - view.top - (view.height - r.height) / 2);
  }

  // -- The probe (1.9) --

  function textOfMessage(m) {
    return (m?.parts ?? []).filter((p) => p?.type === "text").map((p) => p.data?.text ?? "").join("");
  }

  function probeLight() {
    const sid = sessionId ?? "";
    const out = {
      id: shown ? sid : "",
      title: (shown && sessions.byId[sid]?.title) || "",
      path: shown ? conn.project.path || conn.project.cwd || "" : "",
      busy: Boolean(isBusy),
      follow: ui.follow,
      status: built.status,
      scroll_offset: list ? Math.round(list.lastOffset() * 100) / 100 : 0,
      mounted: list ? list.mountedCount() : 0,
      window: list ? list.windowCount() : 0,
      pinned: pins,
      scrollbar: { mode: scrollbar, shown: list ? list.barShowing() : false },
      items: items.map((item) => ({
        key: item.key,
        kind: item.kind,
        id: item.messageId || item.submissionId || item.requestId || "",
        expanded: list ? expandedOf(list.drawnFrameOf(item.key)) : null,
        selected: item.key === ui.selectedItem,
        letter: item.letter,
      })),
      messages: [],
    };
    if (!shown) return out;
    for (const id of chat.ids) {
      const m = chat.byId.get(id);
      if (m) out.messages.push({ id: m.id, role: m.role, finished: isFinished(m), text: textOfMessage(m) });
    }
    // This window's prompts on their way show in the chat too (TYPE-042, TYPE-048).
    for (const p of pending) out.messages.push({ id: `pending:${p.submissionId}`, role: "user", finished: false, text: p.text ?? "", pending: true });
    return out;
  }

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  function wordsInView(limit = 400) {
    const out = [];
    if (!list) return out;
    const view = list.element().getBoundingClientRect();
    const range = document.createRange();
    for (const item of items) {
      const frame = list.frameOf(item.key);
      if (!frame) continue;
      const box = frame.getBoundingClientRect();
      if (box.bottom <= view.top || box.top >= view.bottom) continue;
      const walker = document.createTreeWalker(frame, NodeFilter.SHOW_TEXT);
      let i = 0;
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        for (const word of node.data.matchAll(/\S+/g)) {
          const n = i++;
          range.setStart(node, word.index);
          range.setEnd(node, word.index + word[0].length);
          const r = range.getBoundingClientRect();
          if (!r.width || !r.height || r.bottom <= view.top || r.top >= view.bottom) continue;
          out.push({ mid: item.messageId || item.key, key: item.key, i: n, text: word[0], rect: { x: r.x, y: r.y, w: r.width, h: r.height } });
          if (out.length >= limit) return out;
        }
      }
    }
    return out;
  }

  function probeHeavy() {
    if (!list) return { items: [], words: [], links: [], selection: null, animations: document.getAnimations().length };
    const scroller = list.element();
    const canvas = scroller.firstElementChild;
    const style = canvas ? getComputedStyle(canvas) : null;
    const column = root?.parentElement;
    const rule = column?.querySelector(":scope > .rule.above-composer");
    const links = [];
    const frames = [];
    for (const item of items) {
      const frame = list.frameOf(item.key);
      frames.push({ key: item.key, rect: rectOf(frame), scroll_w: frame?.scrollWidth ?? null, client_w: frame?.clientWidth ?? null });
      if (!frame) continue;
      for (const a of frame.querySelectorAll("a[href]")) {
        links.push({ session_id: sessionId, mid: item.messageId, text: a.textContent, href: a.getAttribute("href"), rects: [...a.getClientRects()].map((r) => ({ x: r.x, y: r.y, w: r.width, h: r.height })) });
      }
    }
    const range = timelineRange(root);
    let selection = null;
    if (range) {
      const { startKey, endKey } = selectionEnds(range);
      const from = indexOf.get(startKey) ?? -1;
      const to = indexOf.get(endKey) ?? -1;
      selection = {
        text: copySelection(range, items),
        visible: range.toString(),
        items: from >= 0 && to >= from ? items.slice(from, to + 1).map((item) => item.key) : [],
      };
    }
    return {
      scroll_top: scroller.scrollTop,
      scroll_height: scroller.scrollHeight,
      view: rectOf(scroller),
      client_w: scroller.clientWidth,
      content: canvas && { x: canvas.getBoundingClientRect().x + parseFloat(style.paddingLeft), w: canvas.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) },
      column: rectOf(column),
      rail: rectOf(rail),
      rule: rectOf(rule),
      composer_top: rule?.nextElementSibling ? Math.min(rule.getBoundingClientRect().bottom, rule.nextElementSibling.getBoundingClientRect().top) : null,
      zoom: zoom(),
      items: frames,
      words: wordsInView(),
      selection,
      links,
      animations: document.getAnimations().length,
    };
  }
</script>

{#if railBottom !== null && !readOnly}
  <div class="rail" bind:this={rail} style:bottom="{railBottom}px" aria-hidden="true"></div>
{/if}

<section class="timeline" aria-label="Conversation" bind:this={root} use:wire use:linkGuard>
  {#if shown}
    {#key sessionId}
      <VirtualList
        bind:this={list}
        items={drawn}
        selectedKey={readOnly ? null : ui.selectedItem}
        focusKey={readOnly || focusOnly ? null : tabKey}
        pinnedKeys={pins}
        {follow}
        {scrollbar}
        onfollow={onFollow}
        onscrolled={onScrolled}
        label="Messages"
      />
    {/key}
  {/if}
</section>

<style>
  /* The rail runs from the head's rule to the top of the composer block. */
  .rail {
    position: absolute;
    left: calc(30 * var(--px));
    top: calc(121 * var(--px));
    width: calc(3 * var(--px));
    background: var(--vermilion);
    pointer-events: none;
  }
  .rail::before {
    content: "";
    position: absolute;
    left: calc(-2 * var(--px));
    top: calc(1 * var(--px) + 1px);
    width: calc(7 * var(--px));
    height: calc(7 * var(--px));
    border-radius: 50%;
    background: var(--vermilion);
  }

  .timeline {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .timeline :global(::selection) {
    background: var(--selection);
  }
</style>
