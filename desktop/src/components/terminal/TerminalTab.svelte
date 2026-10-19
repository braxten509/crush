<script>
  // A terminal tab in the middle column (desktop-design-5-6 2.25, look 5.18):
  // the chat's head with the tab's title ("Terminal", "Editor" or "Remote
  // setup") and its folder as the path line, the head's rule, then the
  // terminal widget filling the column with 12 of padding on --paper, with
  // nothing around it. While the shell starts slowly the loading line shows
  // (look 5.0); a failed start shows its error there in --error-ink.
  //
  // The widget belongs to the tab (lib/terminal/tabs.svelte.js) and moves in
  // and out of this view, so scrollback and selection survive switching. The
  // wheel, pastes and the copy on release are taken here before the widget
  // sees them (2.25, LIST-021 to LIST-023).
  import "@xterm/xterm/css/xterm.css";
  import { dialogs } from "../../lib/dialogs.js";
  import { setFocus, ui } from "../../lib/stores/ui.svelte.js";
  import { addPictures, copySelection, fitTab, getTab, pasteInto, shortFolder, terminals, wheel } from "../../lib/terminal/tabs.svelte.js";
  import Icon from "../Icon.svelte";
  import Rule from "../Rule.svelte";

  let { id } = $props();

  const entry = $derived(terminals.find((tab) => tab.id === id) ?? null);
  let screen = $state();
  let slow = $state(false);

  // The loading line only for a start that takes a moment (look pick).
  $effect(() => {
    slow = false;
    if (entry?.state !== "starting") return;
    const timer = setTimeout(() => (slow = true), 150);
    return () => clearTimeout(timer);
  });

  // The tab's widget in this view.
  $effect(() => {
    const tab = getTab(id);
    const host = screen;
    if (!tab || !host) return;
    let gone = false;
    let frame = 0;
    let observer = null;
    (async () => {
      const term = await tab.created;
      await document.fonts?.ready;
      if (gone || tab.disposed) return;
      if (!tab.opened) {
        term.open(host);
        tab.opened = true;
        addPictures(tab);
      } else {
        host.appendChild(term.element);
      }
      fitTab(tab);
      tab.focus = () => {
        if (dialogs.length === 0) term.focus();
      };
      if (ui.focus === "main") tab.focus();
      observer = new ResizeObserver(() => {
        cancelAnimationFrame(frame);
        frame = requestAnimationFrame(() => fitTab(tab));
      });
      observer.observe(host);
    })();
    return () => {
      gone = true;
      observer?.disconnect();
      cancelAnimationFrame(frame);
      tab.focus = null;
      tab.term?.element?.remove();
    };
  });

  // Input the view takes first: the wheel and pastes (capture, so the widget
  // never handles them its own way), and the copy when a drag ends.
  $effect(() => {
    const host = screen;
    if (!host) return;
    const onWheel = (event) => {
      const tab = getTab(id);
      if (tab && wheel(tab, event)) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    const onPaste = (event) => {
      event.preventDefault();
      event.stopPropagation();
      pasteInto(getTab(id), event.clipboardData?.getData("text/plain") ?? "");
    };
    let dragging = false;
    const onDown = (event) => {
      dragging = event.button === 0;
    };
    // The selection is final once the widget has seen the release too.
    const onUp = (event) => {
      if (!dragging || event.button !== 0) return;
      dragging = false;
      setTimeout(() => copySelection(getTab(id)), 0);
    };
    host.addEventListener("wheel", onWheel, { capture: true, passive: false });
    host.addEventListener("paste", onPaste, { capture: true });
    host.addEventListener("mousedown", onDown, { capture: true });
    document.addEventListener("mouseup", onUp, { capture: true });
    return () => {
      host.removeEventListener("wheel", onWheel, { capture: true });
      host.removeEventListener("paste", onPaste, { capture: true });
      host.removeEventListener("mousedown", onDown, { capture: true });
      document.removeEventListener("mouseup", onUp, { capture: true });
    };
  });
</script>

<section class="terminal-tab" data-terminal-tab={id} aria-label={entry?.title ?? "Terminal"} onfocusin={() => setFocus("main")}>
  <header class="head">
    <h2 class="title">{entry?.title ?? ""}</h2>
    {#if entry?.cwd}<p class="path" aria-label="Path" title={entry.cwd}>{shortFolder(entry.cwd)}</p>{/if}
  </header>

  <Rule marks="both" class="under-head" />

  <div class="body">
    {#if entry?.state === "failed"}
      <p class="status error" role="alert">{entry.error}</p>
    {:else if entry?.state === "starting" && slow}
      <p class="status"><Icon name="working" size={16} /><span>Starting…</span></p>
    {/if}
    <div class="screen" bind:this={screen}></div>
  </div>
</section>

<style>
  .terminal-tab {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--paper);
  }

  /* The chat's head (ChatHead.svelte): title in --t-title, the folder as the path line. */
  .head {
    padding: calc(17 * var(--px)) calc(30 * var(--px)) 0;
    height: calc(121 * var(--px));
    box-sizing: border-box;
    flex: none;
    min-width: 0;
  }
  .title {
    min-height: calc(50 * var(--px));
    font: var(--t-title);
    letter-spacing: -0.02em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    margin-top: calc(11 * var(--px));
    font: var(--t-path);
    letter-spacing: var(--track-path);
    text-transform: uppercase;
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .terminal-tab :global(.rule.under-head) {
    flex: none;
    z-index: 1;
  }

  .body {
    position: relative;
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    padding: calc(12 * var(--px));
  }
  /* The loading line (look 5.0) and a failed start's error in its place. */
  .status {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    flex: none;
    margin: 0 0 calc(12 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .status.error {
    color: var(--error-ink);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .screen {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow: hidden;
  }
  .screen :global(.xterm) {
    height: 100%;
  }
  /* No focus ring on the widget's hidden text box; the cursor shows the focus. */
  .screen :global(.xterm-helper-textarea:focus) {
    outline: none;
  }
  /* The scrollbar as 5.0's: a 6 wide thumb, no track. */
  .screen :global(.xterm-scrollable-element > .scrollbar.vertical) {
    width: calc(6 * var(--px)) !important;
    background: none !important;
  }
  .screen :global(.xterm-scrollable-element > .scrollbar.vertical > .slider) {
    width: calc(6 * var(--px)) !important;
    left: 0 !important;
  }
</style>
