<script>
  // The window: a framed page of three columns (chats, the conversation, and the
  // project panel). The side columns can be dragged wider or narrower; the
  // conversation takes the rest, and the widths are remembered. The panel hides
  // when the window is too small for it or in compact mode (D11). The engine gate
  // stands in for the conversation until the engine is ready, and the overlay
  // host draws the menu, the viewer, help, details and the sheets over it all.
  import { onMount, untrack } from "svelte";
  import ChatList from "./components/ChatList.svelte";
  import ColumnDivider from "./components/ColumnDivider.svelte";
  import Conversation from "./components/Conversation.svelte";
  import Frame from "./components/Frame.svelte";
  import OverlayHost from "./components/overlay/OverlayHost.svelte";
  import SidePanel from "./components/SidePanel.svelte";
  import EngineGate from "./components/start/EngineGate.svelte";
  import { COLUMN_LIMITS, dragColumn, fitColumns, loadColumns, saveColumns } from "./lib/columns.js";
  import { focusField } from "./lib/stores/composer.svelte.js";
  import { start } from "./lib/stores/connection.svelte.js";
  import { setPanelHidden, ui } from "./lib/stores/ui.svelte.js";
  import { ws } from "./lib/stores/workspace.svelte.js";

  // D11 (SIDE-002): the project panel hides when keeping it would leave the
  // conversation under its minimum with both side columns at theirs (1,060
  // picture px of inner width), when the window is under 640 picture px tall,
  // or in compact mode.
  const PANEL_ROOM = COLUMN_LIMITS.left.min + COLUMN_LIMITS.right.min + COLUMN_LIMITS.middleMin;
  const PANEL_MIN_HEIGHT = 640;

  const storage = (() => {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  })();

  let widths = $state(loadColumns(storage));
  let page = $state();

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
    if (!ui.panelHidden) return fitColumns(next, inner());
    const { min } = COLUMN_LIMITS.right;
    return { left: fitColumns({ left: next.left, right: min }, inner() + min).left, right: next.right };
  }

  function drag(side, delta) {
    widths = ui.panelHidden ? fit({ ...widths, left: widths.left + delta }) : dragColumn(widths, side, delta, inner());
  }
  function done() {
    saveColumns(storage, widths);
  }

  // On every size or zoom change: hide or show the panel, then keep the
  // conversation readable when the window shrinks.
  $effect(() => {
    if (!page) return;
    const compact = ws.config?.options?.tui?.compact_mode === true;
    const update = () => {
      const hidden = compact || inner() < PANEL_ROOM || window.innerHeight / scale() < PANEL_MIN_HEIGHT;
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

  // Join the engine once the page is up (connection.svelte.js).
  onMount(() => {
    start();
  });
</script>

<div class="sheet" data-probe="shell">
  <div
    class="page"
    class:panel-hidden={ui.panelHidden}
    bind:this={page}
    style:--left={widths.left}
    style:--right={widths.right}
  >
    <Frame panel={!ui.panelHidden} />
    <div class="column chats-column">
      <ChatList />
    </div>
    <ColumnDivider
      side="left"
      column="3"
      label="Resize the chat list"
      value={widths.left}
      min={COLUMN_LIMITS.left.min}
      max={COLUMN_LIMITS.left.max}
      ondrag={drag}
      ondone={done}
    />
    <div class="column conversation-column">
      <EngineGate>
        <Conversation />
      </EngineGate>
    </div>
    {#if !ui.panelHidden}
      <ColumnDivider
        side="right"
        column="5"
        label="Resize the project panel"
        value={widths.right}
        min={COLUMN_LIMITS.right.min}
        max={COLUMN_LIMITS.right.max}
        ondrag={drag}
        ondone={done}
      />
      <div class="column panel-column">
        <SidePanel />
      </div>
    {/if}
  </div>
</div>

<OverlayHost />

<style>
  .sheet {
    position: fixed;
    inset: 0;
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

  .column {
    grid-row: 2;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .column > :global(*) {
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
