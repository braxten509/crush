<script>
  // The window: a framed page of three columns (chats, the conversation, and the
  // project panel). The side columns can be dragged wider or narrower; the
  // conversation takes the rest, and the widths are remembered. The engine gate
  // stands in for the conversation until the engine is ready, and the overlay
  // host draws the menu, the viewer, help, details and the sheets over it all.
  import { onMount } from "svelte";
  import ChatList from "./components/ChatList.svelte";
  import ColumnDivider from "./components/ColumnDivider.svelte";
  import Conversation from "./components/Conversation.svelte";
  import Frame from "./components/Frame.svelte";
  import OverlayHost from "./components/overlay/OverlayHost.svelte";
  import SidePanel from "./components/SidePanel.svelte";
  import EngineGate from "./components/start/EngineGate.svelte";
  import { COLUMN_LIMITS, dragColumn, fitColumns, loadColumns, saveColumns } from "./lib/columns.js";
  import { sample } from "./lib/sample.js";
  import { start } from "./lib/stores/connection.svelte.js";

  const storage = (() => {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  })();

  let widths = $state(loadColumns(storage));
  let page = $state();

  // The page's inner width in picture pixels (four one-pixel lines excluded).
  function inner() {
    if (!page) return null;
    const scale = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
    return (page.clientWidth - 4) / scale;
  }

  function drag(side, delta) {
    widths = dragColumn(widths, side, delta, inner());
  }
  function done() {
    saveColumns(storage, widths);
  }

  // Keep the conversation readable when the window shrinks.
  $effect(() => {
    if (!page) return;
    const observer = new ResizeObserver(() => {
      const fitted = fitColumns(widths, inner());
      if (fitted.left !== widths.left || fitted.right !== widths.right) widths = fitted;
    });
    observer.observe(page);
    return () => observer.disconnect();
  });

  // Join the engine once the page is up (connection.svelte.js).
  onMount(() => {
    start();
  });
</script>

<div class="sheet" data-probe="shell">
  <div class="page" bind:this={page} style:--left={widths.left} style:--right={widths.right}>
    <Frame />
    <div class="column chats-column">
      <ChatList groups={sample.chats.groups} />
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
      <SidePanel project={sample.project} usage={sample.usage} visuals={sample.visuals} />
    </div>
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
