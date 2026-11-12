<script>
  // The window's own title bar on Linux (owner 2026-10-20, design D): the
  // system bar's parts in the edition's type, the chat's title as a running
  // head with the folder after it in italic, and on the right the right
  // panel's switch (owner, 2026-10-20, A: filled while the panel shows), the
  // pin (keep the window above the others), minimize, maximize and close.
  // Pinned, only the pin changes: it fills in (owner, 2026-10-20). After the
  // folder, the Git branch in the path style, or on a branch "Branched from
  // <parent title>" with the title as a link that opens it, reopening it
  // first when it is closed; the chat's own head is gone (owner, 2026-10-20).
  //
  // Moving, snapping to screen edges, double-click to maximize and the
  // right-click window menu are the compositor's: the bar is a drag area
  // (app-region), the buttons are not. Close runs the page's close steps, as
  // the system's button does (electron/main.cjs). Where the system draws the
  // bar (Mac, Windows, outside the app) this draws nothing.
  import { invoke } from "@tauri-apps/api/core";
  import { api } from "../../lib/engine/api.js";
  import { conversationMarkdown } from "../../lib/export-conversation.js";
  import { notify } from "../../lib/stores/ui.svelte.js";
  import appIcon from "../../assets/heart.svg?url";
  import { getCurrentWindow } from "../../lib/bridge/window.js";
  import { isClosed, reopen } from "../../lib/stores/closed.svelte.js";
  import { parentOf } from "../../lib/stores/links.svelte.js";
  import { openChat, sessions } from "../../lib/stores/sessions.svelte.js";
  import { panelSetting, setPanelShown } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
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
  // A branch whose parent is still listed names it; a deleted parent took its
  // link along (engine 5.11), and the branch reads as any other chat.
  const parent = $derived.by(() => {
    const id = sessions.currentId ? parentOf(sessions.currentId) : null;
    return id && sessions.byId[id] ? { id, title: sessions.byId[id].title || "New Session" } : null;
  });
  const activeBranch = $derived.by(() => {
    const sid = sessions.currentId;
    const chatBranch = sid ? sessions.byId[sid]?.branch : "";
    return chatBranch || ws.branch || "";
  });

  let branchMenuOpen = $state(false);
  let branchList = $state([]);
  let creatingBranch = $state(false);
  let newBranchName = $state("");
  let branchSaving = $state(false);
  let branchWrap = $state(null);

  async function toggleBranchMenu() {
    if (!sessions.currentId) return;
    if (branchMenuOpen) {
      branchMenuOpen = false;
      creatingBranch = false;
      newBranchName = "";
      return;
    }
    branchMenuOpen = true;
    creatingBranch = false;
    newBranchName = "";
    try {
      const res = await api.gitBranches(sessions.currentId);
      branchList = Array.isArray(res?.branches) ? res.branches : [];
    } catch {
      branchList = activeBranch ? [activeBranch] : [];
    }
  }

  async function pickBranch(branch, create = false) {
    const sid = sessions.currentId;
    const target = String(branch ?? "").trim();
    if (!sid || !target || branchSaving) return;
    branchSaving = true;
    try {
      const updated = await api.setSessionBranch(sid, { branch: target, create });
      if (updated?.id && sessions.byId[updated.id]) {
        sessions.byId[updated.id] = { ...sessions.byId[updated.id], ...updated };
      }
      branchMenuOpen = false;
      creatingBranch = false;
      newBranchName = "";
    } catch (error) {
      notify("error", error?.message ?? String(error));
    } finally {
      branchSaving = false;
    }
  }

  $effect(() => {
    if (!branchMenuOpen) return;
    function onPointerDown(ev) {
      if (branchWrap && !branchWrap.contains(ev.target)) {
        branchMenuOpen = false;
        creatingBranch = false;
        newBranchName = "";
      }
    }
    function onKeyDown(ev) {
      if (ev.key === "Escape") {
        branchMenuOpen = false;
        creatingBranch = false;
        newBranchName = "";
      }
    }
    window.addEventListener("pointerdown", onPointerDown, true);
    window.addEventListener("keydown", onKeyDown, true);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown, true);
      window.removeEventListener("keydown", onKeyDown, true);
    };
  });

  async function exportChat() {
    const sid = sessions.currentId;
    if (!sid) return;
    const title = sessions.byId[sid]?.title || "Conversation";
    try {
      const messages = await api.messages(sid);
      const name = title.replace(/[\\/:*?"<>|\x00-\x1f]/g, "_").slice(0, 150) || "Conversation";
      const saved = await invoke("save_conversation", { name: `${name}.md`, text: conversationMarkdown(title, messages) });
      if (saved) notify("info", "Conversation exported.");
    } catch (error) { notify("error", error?.message ?? String(error)); }
  }

  async function openParent() {
    const id = parent?.id;
    if (!id) return;
    if (isClosed(id) && !(await reopen(id))) return;
    await openChat(id);
  }

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
    <span class="words">
    <span class="chat" data-probe="title-chat">{parts.chat}</span>
    {#if parts.project}
      <span class="project" data-probe="title-project">{parts.project}</span>
    {/if}
    {#if parent}
      <span class="path branched" data-probe="title-branch">
        <span class="from">{"Branched from "}</span><button class="parent" type="button" tabindex="-1" title="{isClosed(parent.id) ? 'Reopen' : 'Open'} {parent.title}" onclick={openParent}><span class="parent-title">{parent.title}</span></button>
      </span>
    {:else if activeBranch}
      <span class="branch-wrap" bind:this={branchWrap}>
        {#if sessions.currentId}
          <button
            type="button"
            tabindex="-1"
            class="path branch-btn"
            data-probe="title-branch"
            title="Switch branch for this chat"
            aria-expanded={branchMenuOpen}
            onclick={toggleBranchMenu}
          >{activeBranch}</button>
        {:else}
          <span class="path" data-probe="title-branch" title="Git branch">{activeBranch}</span>
        {/if}
        {#if branchMenuOpen}
          <div class="branch-menu" data-probe="title-branch-menu" role="menu">
            {#each branchList as b (b)}
              <button
                type="button"
                class="branch-item"
                class:current={b === activeBranch}
                role="menuitem"
                disabled={branchSaving}
                onclick={() => pickBranch(b, false)}
              >
                <span class="branch-check">{b === activeBranch ? "✓" : ""}</span>
                <span class="branch-name">{b}</span>
              </button>
            {/each}
            <div class="branch-sep"></div>
            {#if creatingBranch}
              <form
                class="branch-new-form"
                onsubmit={(ev) => {
                  ev.preventDefault();
                  pickBranch(newBranchName, true);
                }}
              >
                <input
                  type="text"
                  class="branch-new-input"
                  placeholder="New branch name"
                  bind:value={newBranchName}
                  disabled={branchSaving}
                  autofocus
                />
              </form>
            {:else}
              <button
                type="button"
                class="branch-item branch-create"
                role="menuitem"
                disabled={branchSaving}
                onclick={() => (creatingBranch = true)}
              >
                <span class="branch-check">+</span>
                <span class="branch-name">New branch…</span>
              </button>
            {/if}
          </div>
        {/if}
      </span>
    {/if}
    </span>
    <span class="buttons">
      {#if sessions.currentId}
        <button type="button" title="Export conversation" aria-label="Export conversation" onclick={exportChat}><Icon name="download" /></button>
      {/if}
      <button
        type="button"
        tabindex="-1"
        class="panel-switch"
        title={panelSetting.off ? "Show the right panel" : "Hide the right panel"}
        aria-label={panelSetting.off ? "Show the right panel" : "Hide the right panel"}
        aria-pressed={!panelSetting.off}
        data-probe="title-panel"
        onclick={() => setPanelShown(panelSetting.off)}
      >
        <Icon name={panelSetting.off ? "panel" : "panel-on"} />
      </button>
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
        data-maximized={state.maximized ? "true" : "false"}
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

  /* The heart bit alone, no tile (owner, 2026-10-20: no background, bigger,
     then 1-2 px smaller). */
  .app-icon {
    flex: none;
    width: calc(25 * var(--px));
    height: calc(21.3 * var(--px));
    margin-right: calc(14 * var(--px));
  }

  /* The chat's title gives way first; the folder keeps its name until it
     alone would crowd the buttons. */
  /* The title, folder and branch share one baseline (owner, 2026-10-20: "not
     aligned"); the run is centred in the bar as a whole, then set so the
     title's capitals sit on the bar's middle. */
  .words {
    position: relative;
    top: 0.5px;
    display: flex;
    align-items: baseline;
    flex: 0 1 auto;
    min-width: 0;
  }
  .chat {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font-weight: 500;
  }
  /* Its floor is 60 or its own width, if less, so a short name leaves no
     wider gap before the branch (owner, 2026-10-20). */
  .project {
    flex: 0 10 auto;
    min-width: calc-size(max-content, min(size, calc(60 * var(--px))));
    overflow: hidden;
    text-overflow: ellipsis;
    margin-left: calc(15 * var(--px));
    font-style: italic;
    color: var(--slate);
  }

  /* The branch, as the chat head's path line was: it gives way after the
     chat's title and before the folder. */
  .path {
    flex: 0 5 auto;
    min-width: calc-size(max-content, min(size, calc(60 * var(--px))));
    overflow: hidden;
    text-overflow: ellipsis;
    margin-left: calc(15 * var(--px));
    font: var(--t-path);
    letter-spacing: var(--track-path);
    text-transform: uppercase;
    color: var(--slate);
  }
  .branch-wrap {
    position: relative;
    display: inline-flex;
    align-items: baseline;
    flex: 0 5 auto;
    min-width: 0;
    -webkit-app-region: no-drag;
    app-region: no-drag;
  }
  .branch-btn {
    padding: 0;
    border: 0;
    background: none;
    cursor: pointer;
    -webkit-app-region: no-drag;
    app-region: no-drag;
  }
  .branch-btn:hover {
    color: var(--ink);
  }
  .branch-menu {
    position: absolute;
    top: calc(100% + 6 * var(--px));
    left: calc(15 * var(--px));
    z-index: 80;
    min-width: calc(180 * var(--px));
    max-width: calc(280 * var(--px));
    max-height: calc(260 * var(--px));
    overflow-y: auto;
    padding: calc(4 * var(--px));
    background: var(--paper-raised, var(--paper));
    border: 1px solid var(--rule);
    border-radius: calc(6 * var(--px));
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.14);
    font: var(--t-item);
    letter-spacing: normal;
    text-transform: none;
    color: var(--ink);
  }
  .branch-item {
    display: flex;
    align-items: center;
    gap: calc(6 * var(--px));
    width: 100%;
    padding: calc(5 * var(--px)) calc(8 * var(--px));
    border: 0;
    border-radius: calc(4 * var(--px));
    background: none;
    font: inherit;
    color: var(--ink);
    text-align: left;
    cursor: pointer;
  }
  .branch-item:hover {
    background: var(--control-hover);
  }
  .branch-item.current {
    font-weight: 500;
  }
  .branch-check {
    flex: none;
    width: calc(14 * var(--px));
    color: var(--slate);
    text-align: center;
  }
  .branch-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .branch-sep {
    height: 1px;
    margin: calc(4 * var(--px)) 0;
    background: var(--rule-faint);
  }
  .branch-new-form {
    padding: calc(4 * var(--px));
  }
  .branch-new-input {
    width: 100%;
    box-sizing: border-box;
    padding: calc(4 * var(--px)) calc(6 * var(--px));
    border: 1px solid var(--rule);
    border-radius: calc(4 * var(--px));
    background: var(--paper);
    font: inherit;
    color: var(--ink);
  }
  /* On a branch: "Branched from " keeps its width and a long parent title is
     cut with "…" (a button draws its text in an inner box, so the cut is on
     a span inside it). The link is clickable, not part of the drag area. */
  .branched {
    display: flex;
    align-items: baseline;
  }
  .from {
    flex: none;
    white-space: pre;
  }
  .parent {
    min-width: 0;
    overflow: hidden;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    color: var(--link);
    cursor: pointer;
    -webkit-app-region: no-drag;
    app-region: no-drag;
  }
  .parent-title {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .parent:hover .parent-title {
    text-decoration-thickness: max(1px, calc(1.5 * var(--px)));
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
  /* Out of the Tab order, so no focus ring after a click (owner, 2026-10-20). */
  .buttons button:focus-visible {
    outline: none;
  }
  .buttons .pin,
  .buttons .panel-switch {
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
