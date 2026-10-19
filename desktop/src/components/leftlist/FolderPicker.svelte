<script>
  // The folder picker, the sheet "folder_picker" (desktop-design-5-6 2.21,
  // look 5.17; LIST-004 to LIST-007, LIST-038 to LIST-042, LIST-048), the
  // desktop form of the terminal's new-session box (internal/ui/sessionhost/
  // picker.go, input.go:73-107, draw.go:622-708). The path field opens on the
  // shown chat's folder (or the shown terminal tab's), with home as "~", a
  // "/" at the end and the caret after it; below it the folders matching
  // what is typed (VIEW's dir_list) and the recent ones (SES's
  // recent_folders), an error line, and the system folder dialog behind
  // "Browse…". Enter, a second click on a row, or a folder from the dialog
  // starts there: a session in this window's folder is a new chat here (as
  // Ctrl+N), in another folder the window for it opens or comes forward with
  // a new chat (window_open); a terminal is TERM's openTerminal. A failure
  // shows on the list's bottom line.
  import { onMount } from "svelte";
  import { runAction } from "../../lib/actions.js";
  import { closeDialog } from "../../lib/dialogs.js";
  import { dirList, file_info, folderPick, recentFolders, windowOpen } from "../../lib/engine/commands.js";
  import { comboOf } from "../../lib/keys.js";
  import { shownTerminal } from "../../lib/leftlist/numbers.js";
  import * as P from "../../lib/leftlist/picker.js";
  import { leftList } from "../../lib/leftlist/store.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { openTerminal, terminals } from "../../lib/terminal/tabs.svelte.js";
  import Sheet from "../kit/Sheet.svelte";

  let { mode = "session" } = $props();

  const title = $derived(mode === "terminal" ? "New terminal in…" : "New session in…");
  // The terminal's hint (draw.go:673) as help pairs.
  const HELP = [
    { key: "↑↓", text: "pick" },
    { key: "tab", text: "open folder" },
    { key: "enter", text: "start" },
    { key: "esc", text: "cancel" },
  ];

  let root = $state(null);
  let field = $state(null);
  let settled = Promise.resolve();

  const p = $derived(leftList.picker);

  // The shown terminal tab's folder, else this window's (host.go:287-300).
  function startFolder() {
    const tab = terminals.find((t) => t.id === shownTerminal());
    return tab?.cwd || conn.project.cwd || "";
  }

  onMount(() => {
    const kind = mode === "terminal" ? "terminal" : "session";
    leftList.picker = P.newPicker({ kind, dir: startFolder(), home: leftList.home });
    const mine = leftList.picker;
    syncField();
    rematch();
    // Recent is read each time the box opens (2.21).
    recentFolders(P.PICKER_ROWS)
      .then((list) => {
        if (leftList.picker === mine) mine.recent = P.newPicker({ recent: list }).recent;
      })
      .catch(() => {});
    field?.focus();
    syncField();
    document.addEventListener("pointerdown", outside, true);
    return () => {
      document.removeEventListener("pointerdown", outside, true);
      if (leftList.picker === mine) leftList.picker = null;
    };
  });

  // The matches for the typed path; the newest typing wins.
  function rematch() {
    const mine = leftList.picker;
    if (!mine) return;
    const version = mine.version;
    settled = P.matchFolders(P.typedPath(mine), dirList).then((matches) => {
      if (leftList.picker === mine && mine.version === version) mine.matches = matches;
    });
  }

  // Runs fn once the matches for what is typed are in.
  function whenSettled(fn) {
    const mine = leftList.picker;
    settled.then(() => {
      if (leftList.picker === mine) fn(mine);
    });
  }

  function syncField() {
    const mine = leftList.picker;
    if (!field || !mine) return;
    if (field.value !== mine.input) field.value = mine.input;
    if (document.activeElement === field) field.setSelectionRange(mine.cursor, mine.cursor);
  }

  function changed() {
    syncField();
    rematch();
  }

  function readCaret() {
    const mine = leftList.picker;
    if (mine && field && document.activeElement === field) mine.cursor = field.selectionStart ?? mine.cursor;
  }

  // Typing, Backspace, Delete and the like are the text box's own; the
  // typed path follows it (picker.go:168-199).
  function onInput() {
    const mine = leftList.picker;
    if (!mine || !field) return;
    const text = P.cleanText(field.value);
    const caret = Math.min(field.selectionStart ?? text.length, text.length);
    if (text !== field.value) {
      field.value = text;
      field.setSelectionRange(caret, caret);
    }
    mine.cursor = caret;
    if (text === mine.input) return;
    mine.input = text;
    P.edited(mine);
    rematch();
  }

  // A paste loses its new lines, returns and tabs (picker.go:168-182).
  function onPaste(event) {
    const mine = leftList.picker;
    if (!mine || !field) return;
    event.preventDefault();
    const text = event.clipboardData?.getData("text/plain") ?? "";
    if (P.insert(mine, text, field.selectionStart ?? mine.cursor, field.selectionEnd ?? mine.cursor)) changed();
  }

  // The box's keys (input.go:73-107). True when the box took the key; the
  // rest (typing, ←, →, Home, End, Backspace, Delete) go to the field.
  function onKey(event) {
    const mine = leftList.picker;
    if (!mine) return false;
    readCaret();
    switch (comboOf(event)) {
      case "Escape":
        closeDialog("folder_picker");
        return true;
      case "Enter":
        whenSettled(startPicked);
        return true;
      case "ArrowUp":
      case "Shift+Tab":
      case "Ctrl+P":
        whenSettled((m) => P.move(m, -1));
        return true;
      case "ArrowDown":
      case "Ctrl+N":
        whenSettled((m) => P.move(m, 1));
        return true;
      case "Tab":
        whenSettled((m) => {
          if (P.complete(m)) changed();
        });
        return true;
      case "Ctrl+A":
        P.caretHome(mine);
        syncField();
        return true;
      case "Ctrl+E":
        P.caretEnd(mine);
        syncField();
        return true;
      case "Ctrl+D":
        if (P.deleteForward(mine)) changed();
        return true;
      case "Ctrl+W":
      case "Alt+Backspace":
        if (P.deleteWord(mine)) changed();
        return true;
      case "Ctrl+U":
        P.setInput(mine, "");
        changed();
        return true;
    }
    // Alt with a character types nothing (input.go:103).
    if (event.altKey && !event.ctrlKey && !event.metaKey && event.key.length === 1) return true;
    if (field && document.activeElement !== field) field.focus();
    return false;
  }

  function onFieldKey(event) {
    if (event.defaultPrevented || event.isComposing) return;
    if (onKey(event)) {
      event.preventDefault();
      event.stopPropagation();
    }
  }

  // Enter (startPicked, host.go:306-320): the picked row or the typed path,
  // if it is a folder; else the box says why and stays.
  async function startPicked(mine) {
    const dir = P.choice(mine);
    if (dir === null) return;
    if (!(await isFolder(dir))) {
      if (leftList.picker === mine) P.noFolder(mine, dir);
      return;
    }
    if (leftList.picker !== mine) return;
    closeDialog("folder_picker");
    startIn(mine.kind, dir);
  }

  async function isFolder(dir) {
    try {
      const info = await file_info(dir, conn.project.cwd || "/");
      return info?.exists === true && info?.is_dir === true;
    } catch {
      return false;
    }
  }

  // Starting (2.21): the failure line is cleared by each attempt.
  async function startIn(what, dir) {
    leftList.failure = "";
    if (what === "terminal") {
      try {
        const tab = await openTerminal({ cwd: dir });
        if (!tab?.id) throw new Error("no terminal tab");
        // The tab exists at once; whether its shell started comes later.
        const start = await tab.started;
        if (start?.error) throw new Error(start.error);
      } catch (error) {
        console.error("Could not start a terminal", error?.message ?? error);
        leftList.failure = "Couldn't start the terminal.";
      }
      return;
    }
    if (P.clean(dir) === P.clean(conn.project.cwd || "")) {
      runAction("chat.new");
      return;
    }
    try {
      await windowOpen(dir, null, true, null);
    } catch (error) {
      console.error("Could not start a Crush session", error?.message ?? error);
      leftList.failure = "Couldn't start a session.";
    }
  }

  // Browse…: the system folder dialog; a chosen folder starts at once,
  // cancelling changes nothing.
  async function browse() {
    const mine = leftList.picker;
    if (!mine) return;
    const typed = P.clean(P.typedPath(mine) || mine.base || "/");
    let chosen = null;
    try {
      chosen = await folderPick(typed.startsWith("/") ? typed : mine.base);
    } catch (error) {
      console.error("The folder dialog failed", error?.message ?? error);
      return;
    }
    if (!chosen || leftList.picker !== mine) return;
    closeDialog("folder_picker");
    startIn(mine.kind, chosen);
  }

  // A click on a row picks it; on the picked row it starts there
  // (clickPicker, draw.go:690-708).
  function clickRow(i) {
    const mine = leftList.picker;
    if (!mine) return;
    if (i === mine.sel) {
      startPicked(mine);
      return;
    }
    mine.sel = i;
    mine.err = "";
  }

  // A click outside the sheet closes it; inside, rows and Browse keep the
  // caret in the field.
  function frame() {
    return root?.closest('[role="dialog"]') ?? root;
  }
  function outside(event) {
    const box = frame();
    if (box && !box.contains(event.target)) closeDialog("folder_picker");
  }
  function keepCaret(event) {
    if (event.target !== field) event.preventDefault();
  }
</script>

<Sheet id="folder_picker" {title} width="standard" help={HELP} {onKey}>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="folder-picker" bind:this={root} onmousedown={keepCaret}>
    <div class="path-row">
      <label class="path" class:error={Boolean(p?.err)}>
        <input
          bind:this={field}
          class="path-field"
          type="text"
          aria-label={title}
          aria-invalid={p?.err ? "true" : undefined}
          spellcheck="false"
          autocomplete="off"
          oninput={onInput}
          onpaste={onPaste}
          onkeydown={onFieldKey}
        />
      </label>
      <button class="browse" type="button" onclick={browse}>Browse…</button>
    </div>
    {#if p?.err}
      <p class="error" role="alert"><span class="end">{p.err}</span></p>
    {/if}
    <!-- The rows answer the mouse; their keys are the field's (↑ ↓ Tab Enter). -->
    {#if p && p.matches.length > 0}
      <ul class="rows matches" role="listbox" aria-label="Matching folders">
        {#each p.matches as dir, i (dir)}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_interactive_supports_focus -->
          <li class="row" class:picked={p.sel === i} role="option" aria-selected={p.sel === i} data-path={dir} onclick={() => clickRow(i)}>
            <span class="end">{P.short(dir, p.home)}</span>
          </li>
        {/each}
      </ul>
    {/if}
    {#if p && p.recent.length > 0}
      <h3 class="heading">Recent</h3>
      <ul class="rows recent" role="listbox" aria-label="Recent">
        {#each p.recent as dir, j (dir)}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_interactive_supports_focus -->
          <li
            class="row"
            class:picked={p.sel === p.matches.length + j}
            role="option"
            aria-selected={p.sel === p.matches.length + j}
            data-path={dir}
            onclick={() => clickRow(p.matches.length + j)}
          >
            <span class="end">{P.short(dir, p.home)}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</Sheet>

<style>
  .folder-picker {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  /* The field as the sheets' filter field without the icon (look 5.1), its
     text the path face; "Browse…" right beside it at its height. */
  .path-row {
    display: flex;
    gap: calc(12 * var(--px));
    min-width: 0;
  }
  .path {
    flex: 1 1 auto;
    display: flex;
    align-items: center;
    box-sizing: border-box;
    min-width: 0;
    height: calc(50 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(11 * var(--px));
    border: 1px solid var(--rule-control);
    cursor: text;
  }
  .path:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .path.error {
    border-color: var(--error-ink);
  }
  .path-field {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: 0;
    outline: 0;
    background: none;
    font: var(--t-agent-mono);
    color: var(--ink);
    caret-color: var(--choice);
  }
  .browse {
    flex: none;
    box-sizing: border-box;
    height: calc(50 * var(--px));
    padding: 0 calc(20 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    white-space: nowrap;
  }
  .browse:hover {
    background: var(--control-hover);
  }
  .browse:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }

  /* A path cut from the front with "…" when it is long (keepEnd, draw.go:680-688). */
  .end {
    unicode-bidi: isolate;
    direction: ltr;
  }
  .error,
  .row {
    direction: rtl;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .error {
    margin: calc(8 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--error-ink);
  }

  .rows {
    margin: calc(13 * var(--px)) 0 0;
  }
  .heading {
    margin: calc(14 * var(--px)) 0 0;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .heading + .rows {
    margin-top: calc(13 * var(--px));
  }

  /* One-line rows 36 high, text from 23; the picked row as the active state. */
  .row {
    position: relative;
    box-sizing: border-box;
    height: calc(36 * var(--px));
    padding: 0 calc(24 * var(--px)) 0 calc(23 * var(--px));
    font: var(--t-agent-mono);
    line-height: calc(36 * var(--px));
    color: var(--ink);
    cursor: default;
  }
  .row:hover .end {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .row.picked {
    background: var(--band);
  }
  .row.picked::before {
    content: "";
    position: absolute;
    inset: 0;
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    pointer-events: none;
  }
  .row.picked::after {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }
</style>
