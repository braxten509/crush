<script>
  // The window's own title bar on Linux (owner 2026-10-20, design D): the
  // system bar's parts in the edition's type, the chat's title as a running
  // head with the folder after it in italic, and on the right the pin
  // (keep the window above the others), minimize, maximize and close. A
  // pinned window's bottom rule turns vermilion.
  //
  // Moving, snapping to screen edges, double-click to maximize and the
  // right-click window menu are the compositor's: the bar is a drag area
  // (app-region), the buttons are not. Close runs the page's close steps, as
  // the system's button does (electron/main.cjs). Where the system draws the
  // bar (Mac, Windows, outside the app) this draws nothing.
  import appIcon from "../../../src-tauri/icons/icon.svg?url";
  import { getCurrentWindow } from "../../lib/bridge/window.js";
  import { titleParts } from "../../lib/system/title.svelte.js";
  import Icon from "../Icon.svelte";

  let win = null;
  let state = $state({ bar: false, maximized: false, fullscreen: false, pinned: false });
  try {
    win = getCurrentWindow();
    state = win.windowState();
  } catch {}

  const shown = $derived(state.bar && !state.fullscreen);
  const parts = $derived(titleParts());

  $effect(() => win?.onWindowState((next) => (state = next)));

  // The rest of the page sits under the bar (app.css --titlebar).
  $effect(() => {
    const root = document.documentElement;
    if (shown) root.dataset.ownBar = "";
    else delete root.dataset.ownBar;
    return () => delete root.dataset.ownBar;
  });
</script>

{#if shown}
  <header class="title-bar" class:pinned={state.pinned} data-probe="title-bar">
    <img class="app-icon" src={appIcon} alt="" draggable="false" />
    <span class="chat" data-probe="title-chat">{parts.chat}</span>
    {#if parts.project}
      <span class="project" data-probe="title-project">{parts.project}</span>
    {/if}
    <span class="buttons">
      <button
        type="button"
        tabindex="-1"
        class="pin"
        title={state.pinned ? "Don't keep on top" : "Keep on top"}
        aria-label={state.pinned ? "Don't keep on top" : "Keep on top"}
        aria-pressed={state.pinned}
        data-probe="title-pin"
        onclick={() => win.setPinned(!state.pinned)}
      >
        <Icon name={state.pinned ? "pin-on" : "pin"} />
      </button>
      <button type="button" tabindex="-1" title="Minimize" aria-label="Minimize" data-probe="title-minimize" onclick={() => win.minimize()}>
        <Icon name="minimize" />
      </button>
      <button
        type="button"
        tabindex="-1"
        title={state.maximized ? "Restore" : "Maximize"}
        aria-label={state.maximized ? "Restore" : "Maximize"}
        data-probe="title-maximize"
        onclick={() => win.toggleMaximize()}
      >
        <Icon name={state.maximized ? "restore" : "maximize"} />
      </button>
      <button type="button" tabindex="-1" class="close" title="Close" aria-label="Close" data-probe="title-close" onclick={() => win.close()}>
        <Icon name="close" />
      </button>
    </span>
  </header>
{/if}

<style>
  .title-bar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 50;
    box-sizing: border-box;
    height: var(--titlebar);
    display: flex;
    align-items: center;
    padding-left: calc(14 * var(--px));
    background: var(--paper);
    border-bottom: 1px solid var(--rule-faint);
    font: var(--t-item);
    color: var(--ink);
    white-space: nowrap;
    user-select: none;
    -webkit-user-select: none;
    -webkit-app-region: drag;
    app-region: drag;
  }
  /* Pinned: the bottom rule turns vermilion and doubles, drawn over the
     hairline so nothing below moves. */
  .title-bar.pinned::after {
    content: "";
    position: absolute;
    left: 0;
    right: 0;
    bottom: -1px;
    height: 2px;
    background: var(--vermilion);
    pointer-events: none;
  }

  .app-icon {
    flex: none;
    width: calc(24 * var(--px));
    height: calc(24 * var(--px));
    margin-right: calc(14 * var(--px));
  }

  /* The chat's title gives way first; the folder keeps its name until it
     alone would crowd the buttons. */
  .chat {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font-weight: 500;
  }
  .project {
    flex: 0 10 auto;
    min-width: calc(60 * var(--px));
    overflow: hidden;
    text-overflow: ellipsis;
    margin-left: calc(15 * var(--px));
    font-style: italic;
    color: var(--slate);
  }

  .buttons {
    display: flex;
    height: 100%;
    margin-left: auto;
    padding-left: calc(24 * var(--px));
    -webkit-app-region: no-drag;
    app-region: no-drag;
  }
  .buttons button {
    display: grid;
    place-items: center;
    width: calc(54 * var(--px));
    height: 100%;
    padding: 0;
    border: 0;
    background: none;
    color: var(--ink);
    cursor: pointer;
  }
  .buttons button:hover {
    background: var(--control-hover);
  }
  .buttons .pin {
    color: var(--mark);
  }
  .pinned .buttons .pin {
    color: var(--vermilion-ink);
  }
  .buttons .close:hover {
    background: var(--vermilion-fill);
    color: var(--on-accent);
  }
</style>
