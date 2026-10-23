<script>
  // The typing field (TYPE-001, TYPE-005, TYPE-010 to TYPE-014, TYPE-016,
  // TYPE-051): a textarea of 1 to 15 lines of 26 picture px that then
  // scrolls. Its keys go through lib/keys.js (the popup and composer scopes,
  // lib/plugins/composer.js); here are the events only the element sees:
  // typing, pasting, dropped files, focus, and the keys delete mode eats.
  import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
  import { dropText, fileUriPaths } from "../../lib/composer/paste.js";
  import {
    attachField,
    blurPopup,
    caretMoved,
    composer,
    currentPlaceholder,
    draftOf,
    dropPaths,
    insertText,
    pastePicture,
    pasteText,
    resumePopup,
    typed,
  } from "../../lib/stores/composer.svelte.js";
  import { setFocus } from "../../lib/stores/ui.svelte.js";

  const MAX_LINES = 15;

  // A sub-agent's chat shows its field but takes no typing (owner,
  // 2026-10-22): the text stays readable and selectable, and no key of the
  // field's own acts.
  let { readOnly = false } = $props();

  let el = $state(null);
  let lines = $state(1);
  let scrolls = $state(false);

  const value = $derived(draftOf().text); // the chat typed into: the opening one while a switch is on its way

  $effect(() => (el ? attachField(el) : undefined));

  // The height follows the text, 1 to 15 lines (TYPE-001).
  function fit() {
    if (!el) return;
    const line = parseFloat(getComputedStyle(el).lineHeight) || 26;
    el.style.height = "0px";
    const needed = Math.max(1, Math.round(el.scrollHeight / line));
    el.style.height = "";
    lines = Math.min(MAX_LINES, needed);
    scrolls = needed > MAX_LINES;
    if (!scrolls) el.scrollTop = 0;
  }

  // Under 15 lines the field never scrolls. Its lines (28) are taller than
  // its 26 a line, so Chromium scrolled it a few pixels to show the caret and
  // kept that after the text was cleared: the words rose above the cursor
  // (owner, 2026-10-20).
  function scrolled() {
    if (!scrolls && el.scrollTop !== 0) el.scrollTop = 0;
  }

  $effect(() => {
    void value;
    fit();
  });

  $effect(() => {
    if (!el) return;
    const observer = new ResizeObserver(() => fit());
    observer.observe(el);
    return () => observer.disconnect();
  });

  // Files dropped on the window (TYPE-012's GUI version).
  $effect(() => {
    let stop = null;
    let gone = false;
    try {
      getCurrentWebviewWindow()
        .onDragDropEvent((event) => {
          if (event.payload?.type === "drop") dropPaths(event.payload.paths ?? []);
        })
        .then((unlisten) => (gone ? unlisten() : (stop = unlisten)))
        .catch(() => {});
    } catch {
      // Not inside the app window.
    }
    return () => {
      gone = true;
      stop?.();
    };
  });

  function input(event) {
    typed({
      before: draftOf().text,
      value: el.value,
      caret: el.selectionStart ?? el.value.length,
      data: event.data ?? "",
      paste: event.inputType === "insertFromPaste",
    });
  }

  // A paste (TYPE-010 to TYPE-014): a picture, copied files, then text. Text
  // the store does not take goes in natively, keeping the field's undo.
  function paste(event) {
    const data = event.clipboardData;
    if (!data || composer.deleteMode) {
      event.preventDefault();
      return;
    }
    const files = [...(data.files ?? [])];
    const picture = files.find((file) => file.type.startsWith("image/"));
    if (picture) {
      event.preventDefault();
      pastePicture(picture);
      return;
    }
    const uris = fileUriPaths(data.getData("text/uri-list"));
    if (uris.length > 0) {
      event.preventDefault();
      const text = dropText(uris);
      if (!pasteText(text)) insertText(text);
      return;
    }
    if (files.length > 0) {
      event.preventDefault();
      pastePicture(files[0]);
      return;
    }
    const text = data.getData("text/plain");
    if (text && pasteText(text)) event.preventDefault();
  }

  // Delete mode eats every key the key router left (TYPE-038).
  function keydown(event) {
    if (composer.deleteMode) event.preventDefault();
  }
</script>

<textarea
  bind:this={el}
  class="input"
  class:scrolls
  rows="1"
  {value}
  placeholder={currentPlaceholder()}
  aria-label="Message"
  readonly={readOnly}
  tabindex={readOnly ? -1 : undefined}
  aria-disabled={readOnly ? "true" : undefined}
  style:--lines={lines}
  onfocus={() => {
    setFocus("composer");
    resumePopup();
  }}
  onblur={() => blurPopup()}
  oninput={readOnly ? undefined : input}
  onpaste={readOnly ? undefined : paste}
  onkeydown={readOnly ? undefined : keydown}
  onkeyup={caretMoved}
  onpointerup={caretMoved}
  onscroll={scrolled}
></textarea>

<style>
  .input {
    flex: 1;
    min-width: 0;
    box-sizing: content-box;
    /* Each line is 28 tall; one line keeps the 26 box, so the row stays 58.
       26 a line clipped the last line's g and y as lines were added. */
    height: calc((var(--lines, 1) * 28 - 2) * var(--px));
    margin: calc(16 * var(--px)) calc(14 * var(--px)) calc(14 * var(--px));
    padding: 0;
    border: 0;
    outline: 0;
    background: none;
    resize: none;
    overflow: hidden;
    font: var(--t-composer);
    color: var(--ink);
    caret-color: var(--ink);
  }
  .input.scrolls {
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .input::-webkit-scrollbar {
    width: calc(6 * var(--px));
  }
  .input::-webkit-scrollbar-thumb {
    background: var(--rule-control);
  }
  .input::-webkit-scrollbar-track {
    background: transparent;
  }
  .input::placeholder {
    color: var(--slate);
    opacity: 1;
  }
  .input::selection {
    background: var(--selection);
    color: var(--ink);
  }
</style>
