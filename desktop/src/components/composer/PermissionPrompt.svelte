<script>
  // The permission prompt in the slot (DLG-119 to DLG-140; 4.9): the title
  // and the tool, the request's facts, the content (the command, the JSON or
  // the diff), the hints that apply and Allow / Allow for Session / Deny. The
  // keys are the slot scope's while the prompt is mounted; for a moment after
  // it opens every key is absorbed (lib/grace.js), so one meant for the field
  // cannot answer it. The answer is POST permissions/grant; the slot closes
  // when it is taken. A diff is P3.6's DiffView; while that draws nothing the
  // changed lines show as plain text.
  import DiffView from "../tools/DiffView.svelte";
  import {
    BUTTONS,
    contentOf,
    forcedFull,
    headerLines,
    hints,
    homeOf,
    plainDiff,
    promptKey,
    startsSplit,
    TITLE,
    toolLabel,
  } from "../../lib/composer/permission.js";
  import { api } from "../../lib/engine/api.js";
  import { createGrace } from "../../lib/grace.js";
  import { bindKey, shortcuts } from "../../lib/keys.js";
  import { highlight } from "../../lib/render/code.js";
  import { attachSlot, composer, projectDir, setSlotButtons } from "../../lib/stores/composer.svelte.js";
  import { clearPermission } from "../../lib/stores/prompts.svelte.js";
  import { notify, setFocus } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";

  let { request } = $props();

  // promptKey's keys (lib/composer/permission.js).
  const KEYS = [
    "Escape", "d", "D", "ArrowRight", "l", "Tab", "ArrowLeft", "h", "Enter", "Ctrl+Y", "a", "A", "Ctrl+A", "s", "S", "Ctrl+S",
    "t", "f", "Shift+ArrowDown", "J", "Shift+ArrowUp", "K", "Shift+ArrowLeft", "H", "Shift+ArrowRight", "L",
    "PageUp", "PageDown", "Home", "End", "ArrowUp", "ArrowDown", "Space",
  ];
  // The window's own keys, absorbed during the grace too (the quit key never is, 1.8).
  const GLOBAL = [...new Set(shortcuts.filter((s) => s.scope.split(" ").includes("global")).flatMap((s) => s.keys))].filter(
    (combo) => combo !== "Ctrl+C" && !KEYS.includes(combo),
  );
  const CODE_LINE = 24; // --t-code's line, picture px

  const dir = projectDir();
  const home = homeOf(dir);
  const content = $derived(contentOf(request, { workingDir: dir, home }));
  const diff = $derived(content?.kind === "diff");
  // The tool is on the title line, so the facts start after it.
  const facts = $derived(headerLines(request, home).filter((line) => line.key !== "Tool"));

  let root = $state(null);
  let scroller = $state(null);
  let diffBox = $state(null);
  let selected = $state(0);
  let splitChoice = $state(null); // T's choice; until then the width decides (DLG-137)
  let full = $state(false); // F (DLG-138)
  let forced = $state(false);
  let columns = $state(0); // the content box's width in mono characters
  let scrollable = $state(false);
  let diffDrawn = $state(true);
  let xOffset = $state(0);
  let answering = false;

  const split = $derived(splitChoice ?? startsSplit(ws.config?.options?.tui?.diff_mode, columns));
  const grace = createGrace("permission");

  function unit() {
    return (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16) / 16;
  }

  let canvas = null;
  function monoWidth() {
    canvas ??= document.createElement("canvas");
    const context = canvas.getContext("2d");
    context.font = getComputedStyle(scroller ?? root).font;
    return context.measureText("0").width || 8;
  }

  function measure() {
    if (!root) return;
    const ch = monoWidth();
    if (scroller) {
      columns = Math.floor(scroller.clientWidth / ch);
      scrollable = scroller.scrollHeight > scroller.clientHeight + 1;
    }
    // The slot at its largest: 60% of the column (4.9).
    const column = root.closest(".conversation");
    const lines = column ? Math.floor((column.getBoundingClientRect().height * 0.6) / (CODE_LINE * unit())) : Infinity;
    forced = forcedFull(Math.floor(root.clientWidth / ch), lines);
  }

  $effect(() => {
    if (!root) return;
    const observer = new ResizeObserver(() => measure());
    observer.observe(root);
    if (scroller) observer.observe(scroller);
    const column = root.closest(".conversation");
    if (column) observer.observe(column);
    measure();
    return () => observer.disconnect();
  });

  $effect(() => {
    void content;
    void split;
    queueMicrotask(measure);
  });

  // DiffView placeholder check: an element it drew means it draws.
  $effect(() => {
    if (!diffBox) return;
    const check = () => (diffDrawn = diffBox.childElementCount > 0);
    check();
    const observer = new MutationObserver(check);
    observer.observe(diffBox, { childList: true });
    return () => observer.disconnect();
  });

  $effect(() => {
    composer.slotFull = full || forced;
    composer.slotTall = diff;
  });

  $effect(() => {
    setSlotButtons(BUTTONS.map((button, index) => ({ label: button.label, selected: index === selected })));
  });

  // Mounted: the keys, the focus, and the field's way back to this form.
  $effect(() => {
    if (!root) return;
    const unbind = [
      ...KEYS.map((combo) => bindKey("slot", combo, (event) => key(combo, event))),
      ...GLOBAL.map((combo) => bindKey("slot", combo, (event) => grace.absorb(event))),
    ];
    const detach = attachSlot(() => root?.focus({ preventScroll: true }));
    setFocus("slot");
    root.focus({ preventScroll: true });
    return () => {
      for (const off of unbind) off();
      detach();
      grace.close();
      setSlotButtons([]);
      composer.slotFull = false;
      composer.slotTall = false;
    };
  });

  function key(combo, event) {
    if (grace.absorb(event)) return true;
    const action = promptKey(combo, { selected, diff });
    if (!action) return false;
    if (action.answer) answer(action.answer);
    else if (action.selected !== undefined) selected = action.selected;
    else if (action.toggleSplit) splitChoice = !split;
    else if (action.toggleFull) full = !full;
    else if (action.scrollY) scroller?.scrollBy({ top: action.scrollY * CODE_LINE * unit() });
    else if (action.scrollX) {
      if (diff) xOffset = Math.max(0, xOffset + action.scrollX);
      else scroller?.scrollBy({ left: action.scrollX * monoWidth() });
    }
    return true;
  }

  // A key no binding took still restarts the grace's quiet time.
  function leftover(event) {
    if (grace.absorb(event)) event.preventDefault();
  }

  async function answer(action) {
    if (answering) return;
    answering = true;
    const permission = $state.snapshot(request);
    try {
      await api.grantPermission(permission, action);
      clearPermission(permission.tool_call_id);
    } catch (error) {
      answering = false;
      notify("error", error?.message ?? String(error));
    }
  }

  function regain() {
    setFocus("slot");
    root?.focus({ preventScroll: true });
  }

  // The command and the JSON, highlighted (render/code.js returns cleaned nodes).
  function highlighted(node, part) {
    const draw = (p) => node.replaceChildren(highlight(p.text, p.lang));
    draw(part);
    return { update: draw };
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  class="prompt"
  class:full={full || forced}
  role="dialog"
  aria-label="{TITLE}: {toolLabel(request?.tool_name ?? '')}"
  tabindex="-1"
  bind:this={root}
  onkeydown={leftover}
  onpointerdown={regain}
>
  <header class="title">
    <span class="label">{TITLE}</span>
    <span class="tool">{toolLabel(request?.tool_name ?? "")}</span>
  </header>
  {#if facts.length > 0}
    <dl class="facts">
      {#each facts as fact, at (at)}
        <dt>{fact.key}</dt>
        <dd title={fact.value}>{fact.value}</dd>
      {/each}
    </dl>
  {/if}
  {#if content}
    <div class="content" class:diff bind:this={scroller} data-slot-content>
      {#if diff}
        <div class="diffview" bind:this={diffBox}>
          <DiffView files={[content.file]} mode={split ? "split" : "unified"} maxLines={0} lineNumbers={true} wrap={false} {xOffset} yOffset={0} />
        </div>
        {#if !diffDrawn}<pre class="plain" style:--x={xOffset}>{plainDiff(content.file)}</pre>{/if}
      {:else}
        <pre class="text">{#each content.parts as part, at (at)}{#if part.lang}<code use:highlighted={part}></code>{:else}{part.text}{/if}{/each}</pre>
      {/if}
    </div>
  {/if}
  <p class="hint">{hints({ diff, scrollable })}</p>
  <div class="buttons">
    {#each BUTTONS as button, index (button.action)}
      <button
        type="button"
        class="choice"
        class:selected={index === selected}
        tabindex="-1"
        data-slot-button={button.label}
        aria-pressed={index === selected}
        onclick={() => answer(button.action)}
      >
        {button.label.slice(0, button.underline)}<u>{button.label[button.underline]}</u>{button.label.slice(button.underline + 1)}
      </button>
    {/each}
  </div>
</div>

<style>
  .prompt {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: calc(10 * var(--px));
    padding: calc(14 * var(--px)) calc(16 * var(--px)) calc(16 * var(--px));
    container-type: inline-size;
  }
  /* The selected button shows where the keys go. */
  .prompt:focus-visible {
    outline: none;
  }
  .title {
    display: flex;
    align-items: baseline;
    gap: calc(12 * var(--px));
    min-width: 0;
  }
  .label {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .tool {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-body-small);
    color: var(--ink);
  }
  .facts {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    column-gap: calc(12 * var(--px));
    margin: 0;
    font: var(--t-small);
  }
  .facts dt {
    color: var(--slate);
  }
  .facts dd {
    margin: 0;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--ink);
  }
  .content {
    flex: 0 1 auto;
    min-height: calc(3 * 24 * var(--px));
    overflow: auto;
    background: var(--code-bg);
    font: var(--t-code);
    color: var(--ink);
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .content::-webkit-scrollbar {
    width: calc(6 * var(--px));
    height: calc(6 * var(--px));
  }
  .content::-webkit-scrollbar-thumb {
    background: var(--rule-control);
  }
  .content::-webkit-scrollbar-track {
    background: transparent;
  }
  .content.diff {
    flex: 1 1 auto;
    overflow-x: hidden;
  }
  .text,
  .plain {
    margin: 0;
    padding: calc(10 * var(--px)) calc(14 * var(--px));
    font: inherit;
  }
  .text {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .plain {
    white-space: pre;
    transform: translateX(calc(var(--x, 0) * -1ch));
  }
  .text ::selection,
  .plain::selection {
    background: var(--selection);
  }
  .hint {
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-small);
    color: var(--slate);
  }
  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: calc(8 * var(--px));
  }
  /* Full: the buttons sit in the middle (permissions.go Draw). */
  .prompt.full .buttons {
    justify-content: center;
  }
  .choice {
    flex: none;
    box-sizing: border-box;
    height: calc(44 * var(--px));
    padding: 0 calc(18 * var(--px));
    border: 1px solid var(--rule-control);
    font: var(--t-body-small);
    color: var(--ink);
    white-space: nowrap;
  }
  .choice:hover {
    border-color: var(--ink);
  }
  .choice.selected {
    border-color: var(--ink);
    background: var(--band);
  }
  .choice u {
    text-decoration-thickness: 1px;
    text-underline-offset: calc(3 * var(--px));
  }
  /* A narrow column stacks the buttons (DLG-139). */
  @container (max-width: 32rem) {
    .buttons {
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
