<script>
  // A diff (CHAT-107 to CHAT-113, DLG-299, DLG-310). Rows come from
  // lib/diff.js, the terminal's diff view: unified with two line-number
  // columns, "+"/"−" and hunk headings, or side by side when the box is at
  // least 120 characters of the diff font wide; a file name row per file in a
  // multi-file diff. The Changes sheet passes its review files ({path, kind,
  // lines}), drawn unified with wrapped code.
  //
  // Props (desktop-design 1.5.6; the last two are this piece's own):
  //   files        DiffFile[] ({path, before, after}) or review files
  //   mode         "auto" | "unified" | "split"
  //   maxLines     a tool body's collapse (CHAT-113): more rows than this (and
  //                more than one over it) show maxLines rows and "… (N lines
  //                hidden) [click or space to expand]"; 0 shows every row
  //   lineNumbers  the number columns
  //   wrap         long lines wrap; their continuation has no numbers
  //   xOffset      cells cut from the left of the code, with a "…" mark
  //   yOffset      rows dropped from the top
  //   cutAbove     the hidden-rows note goes above the rows (Multi-Edit)
  //   onexpand     a click on the hidden-rows note
  import { onDestroy } from "svelte";
  import { theme } from "../../lib/themes/current.svelte.js";
  import { DELETE, diffRows, INSERT, numberDigits, prepareFile, tintFor } from "../../lib/diff.js";
  import { diffShown, hiddenText } from "../../lib/tools/body.js";
  import { codeLine, cutCells, diffLanguage } from "../../lib/tools/highlight.js";
  import { show, size } from "../../lib/tools/nodes.js";
  import { addProbeReader } from "../../lib/tools/probe.js";
  import { ADD, DEL, HUNK } from "../../lib/tools/review.js";
  import { codeText, numberDigits as reviewDigits } from "../../lib/tools/sheet.js";

  let {
    files = [],
    mode = "auto",
    maxLines = 0,
    lineNumbers = true,
    wrap = false,
    xOffset = 0,
    yOffset = 0,
    cutAbove = false,
    onexpand = null,
  } = $props();

  // diffview: split when the box is this many cells wide.
  const SPLIT_AT = 120;

  let root = $state(null);
  let width = $state(0);
  let cell = $state(0);
  let tint = $state("light");

  const list = $derived(Array.isArray(files) ? files.filter(Boolean) : []);
  const review = $derived(list.length > 0 && list.every((f) => Array.isArray(f.lines)));
  const columns = $derived(cell > 0 ? Math.floor(width / cell) : 0);
  const layout = $derived(review ? "unified" : mode === "split" || mode === "unified" ? mode : columns >= SPLIT_AT ? "split" : "unified");
  const prepared = $derived(review ? [] : list.map((f) => prepareFile(f)));
  const names = $derived(list.length > 1);

  // Every row, each with the index of its file.
  const all = $derived.by(() => {
    if (review) return reviewRows(list);
    let fi = names ? -1 : 0;
    return diffRows(prepared, { layout, names }).rows.map((row) => {
      if (row.type === "file") fi++;
      return { ...row, fi };
    });
  });
  const dropped = $derived(Math.max(0, Math.min(Math.floor(yOffset) || 0, all.length - 1)));
  const rows = $derived(all.slice(dropped));
  const cut = $derived(diffShown(rows.length, maxLines));
  const visible = $derived(rows.slice(0, cut.shown));
  const digits = $derived(review ? [Math.max(...list.map(reviewDigits)), Math.max(...list.map(reviewDigits))] : numberDigits(prepared));
  const languages = $derived(list.map((f) => diffLanguage(f.path, review ? sample(f) : f.before)));
  const shift = $derived(Math.max(0, Math.floor(xOffset) || 0));
  const half = $derived(Math.floor(width / 2));

  function sample(f) {
    let text = "";
    for (const l of f.lines) {
      if (l.kind === HUNK || l.kind === ADD) continue;
      text += l.text + "\n";
      if (text.length > 20 * 1024) break;
    }
    return text;
  }

  function reviewRows(reviewFiles) {
    const out = [];
    reviewFiles.forEach((f, fi) => {
      if (reviewFiles.length > 1) out.push({ type: "file", path: f.path, fi });
      for (const l of f.lines) {
        if (l.kind === HUNK) out.push({ type: "hunk", text: l.text, fi });
        else {
          const kind = l.kind === ADD ? INSERT : l.kind === DEL ? DELETE : "equal";
          out.push({ type: "line", kind, before: l.old || null, after: l.new || null, text: codeText(l.text), fi });
        }
      }
    });
    return out;
  }

  // The colored code of a line, cut by xOffset: the terminal colors the
  // whole line, then cuts it (diffview/render.go lineContent).
  const colored = new Map();
  function code(text, fi) {
    const language = languages[fi] ?? "plaintext";
    const key = language + "\u0000" + text;
    let fragment = colored.get(key);
    if (!fragment) {
      fragment = codeLine(text, language);
      if (colored.size > 2000) colored.clear();
      colored.set(key, fragment);
    }
    return cutCells(fragment.cloneNode(true), shift);
  }

  const sign = (kind) => (kind === INSERT ? "+" : kind === DELETE ? "−" : " ");
  const lead = (text) => shift > 0 && text !== "";

  // Re-read when the theme or the mode changes.
  $effect(() => {
    void theme.dark;
    void theme.revision;
    if (!root) return;
    tint = tintFor(getComputedStyle(root).getPropertyValue("--paper"));
  });

  // The probe reads what this diff shows (CHAT-108, CHAT-110, CHAT-113).
  const probeId = Symbol("diff");
  const stopProbe = addProbeReader(
    "diffs",
    probeId,
    () => ({ layout, rows: visible.length, total: rows.length, hidden: cut.hidden, columns, files: list.map((f) => f.path) }),
    () => {
      const box = root?.getBoundingClientRect();
      const add = root?.querySelector(".row.insert");
      const code = add?.querySelector(".code")?.getBoundingClientRect();
      const gutter = add?.querySelector(".num")?.getBoundingClientRect();
      return {
        rect: box && { x: box.x, y: box.y, w: box.width, h: box.height },
        cell,
        text: root?.querySelector(".rows")?.textContent ?? "",
        add_code: code && { x: code.x, y: code.y, w: code.width, h: code.height },
        add_gutter: gutter && { x: gutter.x, y: gutter.y, w: gutter.width, h: gutter.height },
      };
    },
  );
  onDestroy(stopProbe);
</script>

{#snippet note()}
  {#if cut.hidden > 0}
    {#if onexpand}
      <button class="hidden-note" type="button" onclick={onexpand}>{hiddenText(cut.hidden)}</button>
    {:else}
      <p class="hidden-note">{hiddenText(cut.hidden)}</p>
    {/if}
  {/if}
{/snippet}

{#snippet numbers(before, after)}
  {#if lineNumbers}
    <span class="num before">{before ?? ""}</span><span class="num after">{after ?? ""}</span>
  {/if}
{/snippet}

<div
  class="diff {tint} {layout}"
  class:wrap
  class:numbered={lineNumbers}
  style:--nb={digits[0]}
  style:--na={digits[1]}
  style:--half={layout === "split" ? `${half}px` : null}
  bind:this={root}
  {@attach size((rect) => (width = rect.width))}
  data-layout={layout}
  data-rows={visible.length}
  data-hidden={cut.hidden}
>
  <span class="ruler" aria-hidden="true" {@attach size((rect) => (cell = rect.width / 10))}>0000000000</span>
  {#if cutAbove}{@render note()}{/if}
  {#if visible.length > 0}
    <div class="rows">
      {#each visible as row, i (i)}
        {#if row.type === "file"}
          <div class="row file"><span class="name">{row.path}</span></div>
        {:else if row.type === "hunk"}
          <div class="row hunk">{@render numbers(null, null)}<span class="sign"></span><span class="code">{row.text}</span></div>
        {:else if row.type === "line"}
          <div class="row {row.kind}">
            {@render numbers(row.before, row.after)}<span class="sign">{sign(row.kind)}{lead(row.text) ? "…" : ""}</span><span class="code" {@attach show(() => code(row.text, row.fi))}></span>
          </div>
        {:else if row.type === "pair"}
          <div class="row pair">
            {#each [row.left, row.right] as side, s (s)}
              <span class="side {side?.kind ?? 'missing'}" class:left={s === 0}>
                {#if lineNumbers}<span class="num">{side?.number ?? ""}</span>{/if}
                <span class="sign">{side ? sign(side.kind) : ""}{side && lead(side.text) ? "…" : ""}</span>
                <span class="code" {@attach show(() => (side ? code(side.text, row.fi) : null))}></span>
              </span>
            {/each}
          </div>
        {/if}
      {/each}
    </div>
  {/if}
  {#if !cutAbove}{@render note()}{/if}
</div>

<style>
  /* The tints of 4.0 on a light paper; the terminal bench's dark set on a
     dark one (DLG-310). */
  .diff {
    --d-add-bg: var(--diff-add-bg);
    --d-add-gutter: var(--diff-add-gutter);
    --d-add-ink: var(--diff-add-ink);
    --d-del-bg: var(--diff-del-bg);
    --d-del-gutter: var(--diff-del-gutter);
    --d-del-ink: var(--error-ink);
    --d-hunk-bg: var(--band);
    --d-code-bg: var(--code-bg);
    --d-ink: var(--ink);
    --d-quiet: var(--slate);
    position: relative;
    min-width: 0;
    font: var(--t-code);
    color: var(--d-ink);
    background: var(--d-code-bg);
  }
  .diff.dark {
    --d-add-bg: var(--diff-dark-add-bg);
    --d-add-gutter: var(--diff-dark-add-gutter);
    --d-add-ink: var(--diff-dark-add-ink);
    --d-del-bg: var(--diff-dark-del-bg);
    --d-del-gutter: var(--diff-dark-del-gutter);
    --d-del-ink: var(--diff-dark-del-ink);
    --d-hunk-bg: var(--diff-dark-hunk-bg);
    --d-code-bg: var(--diff-dark-code-bg);
    --d-ink: var(--diff-dark-ink);
    --d-quiet: var(--diff-dark-quiet);
  }
  .ruler {
    position: absolute;
    visibility: hidden;
    white-space: pre;
    pointer-events: none;
  }
  .row {
    display: flex;
    min-width: 0;
  }
  .num {
    flex: none;
    box-sizing: content-box;
    width: calc(var(--nb) * 1ch);
    padding: 0 calc(8 * var(--px));
    text-align: right;
    color: var(--d-quiet);
    font-variant-numeric: tabular-nums;
    user-select: none;
  }
  .num.after {
    width: calc(var(--na) * 1ch);
  }
  .sign {
    flex: none;
    width: 2ch;
    padding-left: calc(4 * var(--px));
    white-space: pre;
    user-select: none;
  }
  .code {
    flex: 1;
    min-width: 0;
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .wrap .code {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .insert {
    background: var(--d-add-bg);
  }
  .insert .num {
    background: var(--d-add-gutter);
    color: var(--d-add-ink);
  }
  .insert .sign {
    color: var(--d-add-ink);
  }
  .delete {
    background: var(--d-del-bg);
  }
  .delete .num {
    background: var(--d-del-gutter);
    color: var(--d-del-ink);
  }
  .delete .sign {
    color: var(--d-del-ink);
  }
  .hunk {
    background: var(--d-hunk-bg);
    color: var(--d-quiet);
  }
  .file {
    padding-left: calc(12 * var(--px));
    color: var(--d-quiet);
  }
  .file .name {
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* Side by side: the left half is the floor of half the width, so an odd
     width gives the right side the extra pixel (CHAT-108). */
  .pair .side {
    display: flex;
    min-width: 0;
    flex: 1;
  }
  .pair .side.left {
    flex: none;
    width: var(--half);
  }
  .pair .missing {
    background: var(--d-hunk-bg);
  }
  .hidden-note {
    display: block;
    margin: 0;
    padding: calc(2 * var(--px)) calc(12 * var(--px));
    font: var(--t-small);
    color: var(--d-quiet);
    text-align: left;
  }
  button.hidden-note {
    cursor: pointer;
  }
  button.hidden-note:hover {
    color: var(--d-ink);
  }
</style>
