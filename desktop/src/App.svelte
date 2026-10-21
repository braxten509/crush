<script>
  // The window: a framed page of three columns (chats, the conversation, and the
  // project panel). The side columns can be dragged wider or narrower; the
  // conversation takes the rest, and the widths are remembered. The panel hides
  // when the window is too small for it or in compact mode (D11). The engine gate
  // stands in for the conversation until the engine is ready, and the overlay
  // host draws the menu, the viewer, help, details and the sheets over it all.
  // Phases 5 and 6 (desktop-design-5-6 7.1): the left column shows the narrow
  // strip instead of the list while leftList.strip; a helper's chat or a
  // terminal tab (mainView.current) takes the conversation's place while the
  // conversation stays mounted, hidden, so its draft and scroll survive; and
  // the debug overlay shows with CRUSH_UI_DEBUG=true. On Linux the window's
  // title bar is the page's own (TitleBar.svelte); the page sits under it.
  import { onMount, untrack } from "svelte";
  import ChatList from "./components/ChatList.svelte";
  import ColumnDivider from "./components/ColumnDivider.svelte";
  import Conversation from "./components/Conversation.svelte";
  import Frame from "./components/Frame.svelte";
  import Strip from "./components/leftlist/Strip.svelte";
  import MainHost from "./components/MainHost.svelte";
  import OverlayHost from "./components/overlay/OverlayHost.svelte";
  import SettingsNav from "./components/settings/SettingsNav.svelte";
  import SidePanel from "./components/SidePanel.svelte";
  import EngineGate from "./components/start/EngineGate.svelte";
  import DebugOverlay from "./components/system/DebugOverlay.svelte";
  import TitleBar from "./components/system/TitleBar.svelte";
  import "./lib/closing.js"; // the window's close steps answer close-requested
  import { COLUMN_LIMITS, dragColumn, fitColumns, loadColumns, saveColumns } from "./lib/columns.js";
  import { app_info } from "./lib/engine/commands.js";
  import { leftList } from "./lib/leftlist/store.svelte.js";
  import { slidePage } from "./lib/motion.js";
  import { settingsOpen } from "./lib/settings/page.svelte.js";
  import { mainView } from "./lib/mainview.js";
  import { focusField } from "./lib/stores/composer.svelte.js";
  import { start } from "./lib/stores/connection.svelte.js";
  import { panelSetting, setPanelHidden, ui } from "./lib/stores/ui.svelte.js";
  import { ws } from "./lib/stores/workspace.svelte.js";
  import { listRoom, meterRoom } from "./components/panel/panel.svelte.js";

  // D11 (SIDE-002): the project panel hides when keeping it would leave the
  // conversation under its minimum with both side columns at theirs (1,060
  // picture px of inner width), when the window is under 640 picture px tall,
  // or when its own switch is off (not the terminal's compact mode).
  // Each side's minimum: its own, or what its words need, the panel's meters
  // and the list's two buttons (owner, 2026-10-20).
  const rightMin = $derived(Math.max(COLUMN_LIMITS.right.min, meterRoom.width));
  const leftMin = $derived(Math.max(COLUMN_LIMITS.left.min, listRoom.width));
  const mins = $derived({ left: leftMin, right: rightMin });
  const panelRoom = () => leftMin + rightMin + COLUMN_LIMITS.middleMin;
  const PANEL_MIN_HEIGHT = 640;
  // The left list's narrow strip is 64 picture px wide and cannot be dragged
  // (desktop-design-5-6 5.2 "Strip"); the list's own width is kept for when it opens.
  const STRIP_WIDTH = 64;

  const storage = (() => {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  })();

  let widths = $state(loadColumns(storage));
  let page = $state();
  let uiDebug = $state(false);

  // CSS pixels per picture pixel (the zoom).
  function scale() {
    return parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
  }

  // The page's inner width in picture pixels (four one-pixel lines excluded).
  function inner() {
    if (!page) return null;
    return (page.clientWidth - 4) / scale();
  }

  // The widths the page can hold. A hidden panel takes no room and keeps its
  // width for when it comes back.
  function fit(next) {
    if (!ui.panelHidden) return fitColumns(next, inner(), mins);
    const min = rightMin;
    return { left: fitColumns({ left: next.left, right: min }, inner() + min, mins).left, right: next.right };
  }

  function drag(side, delta) {
    widths = ui.panelHidden ? fit({ ...widths, left: widths.left + delta }) : dragColumn(widths, side, delta, inner(), mins);
  }
  function done() {
    saveColumns(storage, widths);
  }

  // On every size or zoom change: hide or show the panel, then keep the
  // conversation readable when the window shrinks.
  $effect(() => {
    if (!page) return;
    // Again when the words' room changes a side's minimum.
    void mins;
    // Settings takes the window (owner, 2026-10-19): the panel steps aside.
    const off = panelSetting.off || settingsOpen();
    const update = () => {
      const hidden = off || inner() < panelRoom() || page.parentElement.clientHeight / scale() < PANEL_MIN_HEIGHT;
      if (hidden !== ui.panelHidden) {
        // Focus in the panel moves to the field (SIDE-002; the terminal's compact switch, internal/ui/model/layout.go:70-79).
        if (hidden && ui.focus === "panel") focusField();
        setPanelHidden(hidden);
      }
      const fitted = fit(widths);
      if (fitted.left !== widths.left || fitted.right !== widths.right) widths = fitted;
    };
    untrack(update);
    const observer = new ResizeObserver(update);
    observer.observe(page);
    return () => observer.disconnect();
  });

  // The panel's column stays while the panel slides away (lib/motion.js:
  // owner, 2026-10-20, everything closes as it opens), then gives its room
  // to the conversation.
  let panelKept = $state(!ui.panelHidden);
  $effect.pre(() => {
    if (!ui.panelHidden) panelKept = true;
  });

  // Join the engine once the page is up (connection.svelte.js).
  onMount(() => {
    start();
    app_info()
      .then((info) => (uiDebug = info?.ui_debug === true))
      .catch(() => {});
  });
</script>

<TitleBar />

<div class="sheet" data-probe="shell">
  <div
    class="page"
    class:panel-hidden={!panelKept}
    bind:this={page}
    style:--left={leftList.strip && !settingsOpen() ? STRIP_WIDTH : widths.left}
    style:--right={widths.right}
  >
    <Frame panel={panelKept} />
    <div class="column chats-column">
      {#if settingsOpen()}
        <SettingsNav />
      {:else if leftList.strip}
        <Strip />
      {:else}
        <ChatList />
      {/if}
    </div>
    <ColumnDivider
      side="left"
      column="3"
      label="Resize the chat list"
      fixed={leftList.strip && !settingsOpen()}
      value={widths.left}
      min={leftMin}
      max={COLUMN_LIMITS.left.max}
      ondrag={drag}
      ondone={done}
    />
    <div class="column conversation-column">
      {#if mainView.current}
        <MainHost />
      {/if}
      <div class="conversation-keep" hidden={mainView.current ? true : undefined}>
        <EngineGate>
          <Conversation />
        </EngineGate>
      </div>
    </div>
    {#if panelKept}
      <ColumnDivider
        side="right"
        column="5"
        label="Resize the project panel"
        value={widths.right}
        min={rightMin}
        max={COLUMN_LIMITS.right.max}
        ondrag={drag}
        ondone={done}
      />
      {#if !ui.panelHidden}
        <div class="column panel-column" transition:slidePage|global={{ dir: 1 }} onoutroend={() => (panelKept = false)}>
          <SidePanel />
        </div>
      {/if}
    {/if}
  </div>
</div>

<OverlayHost />

{#if uiDebug}
  <DebugOverlay />
{/if}

<style>
  .sheet {
    position: fixed;
    inset: var(--titlebar) 0 0;
    overflow: hidden;
    background: var(--paper);
  }

  /* The frame sits 20 / 28 / 36 / 30 picture pixels in from the window's edges. */
  .page {
    position: absolute;
    top: calc(20 * var(--px));
    right: calc(28 * var(--px));
    bottom: calc(36 * var(--px));
    left: calc(30 * var(--px));
    display: grid;
    grid-template-columns:
      1px calc(var(--left) * var(--px)) 1px minmax(0, 1fr)
      1px calc(var(--right) * var(--px)) 1px;
    grid-template-rows: 1px minmax(0, 1fr) 1px;
  }
  /* Without the panel (D11) the conversation runs to the frame's right line. */
  .page.panel-hidden {
    grid-template-columns: 1px calc(var(--left) * var(--px)) 1px minmax(0, 1fr) 1px;
  }

  /* Positioned, so a piece closing over its replacement lies on the column
     (lib/motion.js, overlay). */
  .column {
    position: relative;
    grid-row: 2;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .column > :global(*) {
    flex: 1;
  }
  /* The conversation's wrapper takes no box of its own, so the column lays the
     conversation out as before; hidden, it keeps the conversation mounted. */
  .conversation-keep {
    display: contents;
  }
  .conversation-keep[hidden] {
    display: none;
  }
  .conversation-keep > :global(*) {
    flex: 1;
  }
  .chats-column {
    grid-column: 2;
  }
  .conversation-column {
    grid-column: 4;
  }
  .panel-column {
    grid-column: 6;
  }
</style>
