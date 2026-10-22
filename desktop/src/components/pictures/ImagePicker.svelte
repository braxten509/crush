<script>
  // The Add Image picker (desktop-design-5-6 2.10, look 5.11; DLG-245 to
  // DLG-257): a port of internal/ui/dialog/filepicker.go. It lists a folder
  // through dir_list (folders first, hidden entries left out); only .jpg,
  // .jpeg and .png files can be chosen. ↑/K/Ctrl+P and ↓/J/Ctrl+N move and
  // stop at the ends; PgUp/PgDn page; g/G first and last; →/L/Enter open a
  // folder; ←/H/Backspace go up and select the folder they came from; Enter
  // on a picture adds it to the typing box as an attachment. A click
  // selects, a double click opens or chooses. The highlighted picture shows
  // beside the list once the list is at least 300 tall.
  import { onMount, tick } from "svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { dirList, file_info, file_read } from "../../lib/engine/commands.js";
  import { baseName, choosable, EMPTY, joinPath, MAX_BYTES, parentOf, shownEntries, TOO_LARGE } from "../../lib/pictures/picker.js";
  import { pictures } from "../../lib/pictures/store.svelte.js";
  import { addAttachment, projectDir } from "../../lib/stores/composer.svelte.js";

  const ID = "filepicker";
  const ROW = 36; // picture px per row (5.11)
  const PREVIEW_MIN = 300;

  let dir = $state("");
  let rows = $state([]);
  let selected = $state(0);
  let error = $state("");
  let loading = $state(true);
  let preview = $state(null); // {path, src}
  let listBox = $state(null);
  let listHeight = $state(0);
  const back = []; // the selection in each folder above, to restore on the way up

  const current = $derived(rows[selected] ?? null);
  const showPreview = $derived(listHeight >= PREVIEW_MIN);
  const page = $derived(Math.max(1, Math.floor(listHeight / ROW) - 1));

  const help = [
    { key: "↑↓←→", text: "navigate" },
    { key: "enter", text: "select" },
    { key: "esc", text: "close/exit" },
  ];

  async function load(path, select = null) {
    loading = true;
    error = "";
    try {
      const list = await dirList(path);
      dir = list.path;
      rows = shownEntries(list.entries);
      const at = select ? rows.findIndex((row) => row.name === select) : -1;
      selected = at >= 0 ? at : 0;
      pictures.lastDir = dir;
      return true;
    } catch (e) {
      error = e?.message ?? String(e);
      return false;
    } finally {
      loading = false;
      await tick();
      scrollToSelected();
    }
  }

  async function start() {
    for (const path of [pictures.lastDir, projectDir(), "~"]) {
      if (path && (await load(path))) return;
    }
  }

  function scrollToSelected() {
    listBox?.querySelector(`[data-index="${selected}"]`)?.scrollIntoView({ block: "nearest" });
  }

  function move(to) {
    if (rows.length === 0) return;
    selected = Math.max(0, Math.min(rows.length - 1, to));
    tick().then(scrollToSelected);
  }

  function openFolder(row) {
    back.push(selected);
    load(joinPath(dir, row.name)).then((ok) => ok || back.pop());
  }

  function goUp() {
    if (dir === "/") return;
    const from = baseName(dir);
    back.pop();
    load(parentOf(dir), from);
  }

  async function choose(row) {
    const path = joinPath(dir, row.name);
    error = "";
    try {
      const info = await file_info(path, dir);
      if (info.size > MAX_BYTES) {
        error = TOO_LARGE;
        return;
      }
    } catch (e) {
      error = `unable to read the image: ${e?.message ?? e}`;
      return;
    }
    let file;
    try {
      file = await file_read(path, dir, MAX_BYTES);
    } catch (e) {
      error = e?.code === "too_big" ? TOO_LARGE : `unable to read the image: ${e?.message ?? e}`;
      return;
    }
    addAttachment({ kind: "picture", name: row.name, mime: file.mime, path, base64: file.base64 });
    closeDialog(ID);
  }

  function enter(row) {
    if (!row) return;
    if (row.is_dir) {
      if (row.readable) openFolder(row);
    } else if (choosable(row) && row.readable) choose(row);
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowUp":
      case "k":
      case "Ctrl+P":
        move(selected - 1);
        return true;
      case "ArrowDown":
      case "j":
      case "Ctrl+N":
        move(selected + 1);
        return true;
      case "PageUp":
        move(selected - page);
        return true;
      case "PageDown":
        move(selected + page);
        return true;
      case "g":
        move(0);
        return true;
      case "G":
        move(rows.length - 1);
        return true;
      case "ArrowRight":
      case "l":
        if (current?.is_dir && current.readable) openFolder(current);
        return true;
      case "Enter":
        enter(current);
        return true;
      case "ArrowLeft":
      case "h":
      case "Backspace":
        goUp();
        return true;
      default:
        return false;
    }
  }

  // The highlighted picture, read once it stays highlighted.
  $effect(() => {
    const row = current;
    const folder = dir;
    if (!row || row.is_dir || !choosable(row) || !row.readable) {
      preview = null;
      return;
    }
    const path = joinPath(folder, row.name);
    if (preview?.path === path) return;
    preview = { path, src: null };
    let live = true;
    const timer = setTimeout(() => {
      file_read(path, folder, MAX_BYTES)
        .then((file) => {
          if (live && preview?.path === path) preview = { path, src: `data:${file.mime};base64,${file.base64}` };
        })
        .catch(() => {}); // preview errors are not shown (DLG-254)
    }, 80);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  });

  let lastClick = { index: -1, at: 0 };
  function click(index) {
    const now = performance.now();
    const double = lastClick.index === index && now - lastClick.at < 400;
    lastClick = { index, at: now };
    selected = index;
    if (double) enter(rows[index]);
  }

  onMount(() => {
    start();
    const observer = new ResizeObserver(() => (listHeight = listBox?.clientHeight ?? 0));
    if (listBox) observer.observe(listBox);
    return () => observer.disconnect();
  });
</script>

<Sheet id={ID} title="Add image" width="wide" maxHeight={640} {help} {onKey}>
  <p class="path">{dir}</p>
  <div class="columns" class:with-preview={showPreview}>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div class="list" bind:this={listBox} role="listbox" aria-label="Files" tabindex="0">
      {#if !loading && rows.length === 0 && !error}
        <p class="empty">{EMPTY}</p>
      {/if}
      {#each rows as row, index (row.name)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div
          class="row"
          class:selected={index === selected}
          class:dim={!row.readable || (!row.is_dir && !choosable(row))}
          role="option"
          aria-selected={index === selected}
          tabindex="-1"
          data-index={index}
          onclick={() => click(index)}
        >
          {row.name}{row.is_dir ? "/" : ""}{row.is_symlink ? " →" : ""}
        </div>
      {/each}
    </div>
    {#if showPreview}
      <div class="preview">
        {#if preview?.src}
          <img src={preview.src} alt={baseName(preview.path)} />
        {/if}
      </div>
    {/if}
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</Sheet>

<style>
  .path {
    margin: 0 0 calc(12 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    overflow-wrap: anywhere;
  }
  .columns {
    display: grid;
    grid-template-columns: 1fr;
    height: calc(440 * var(--px));
    min-height: 0;
  }
  .columns.with-preview {
    grid-template-columns: calc(600 * var(--px)) 1fr;
  }
  /* The list reaches the frame, as FilterList's does: rows 44 high with the
     sheet's 26 gutter, hover and selection drawn 8 in from the frame. */
  .list {
    margin-left: calc(-26 * var(--px));
    overflow-y: auto;
    min-height: 0;
    outline: none;
  }
  .columns:not(.with-preview) .list {
    margin-right: calc(-26 * var(--px));
  }
  .row {
    position: relative;
    isolation: isolate;
    display: flex;
    align-items: center;
    height: calc(44 * var(--px));
    padding: 0 calc(26 * var(--px));
    font: var(--t-item);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }
  .row:hover::before,
  .row.selected::before {
    content: "";
    position: absolute;
    inset: 0 calc(8 * var(--px));
    z-index: -1;
    pointer-events: none;
  }
  .row:hover::before {
    background: var(--control-hover);
  }
  .row.selected::before {
    background: var(--pick);
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
  }
  .row.selected::after {
    content: "";
    position: absolute;
    left: calc(8 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }
  .row.dim {
    color: var(--mark);
  }
  .row.dim.selected::before {
    background: var(--disabled-band);
  }
  .empty {
    margin: calc(12 * var(--px)) 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .preview {
    display: grid;
    place-items: center;
    margin-left: calc(16 * var(--px));
    padding-left: calc(16 * var(--px));
    border-left: 1px solid var(--rule-frame);
    min-height: 0;
  }
  .preview img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    border: 1px solid var(--rule-frame);
  }
  .error {
    margin: calc(8 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--error-ink);
  }
</style>
