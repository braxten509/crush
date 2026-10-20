<script>
  // A `!` command and its output (CHAT-038 to CHAT-045). A 1px ink rule on
  // the left; "$ command" in mono at weight 500, a multi-line command on one
  // line; "Running…" while it runs, then "(Canceled)" for exit 130 or "(exit
  // N)" in --error-ink. The output in mono slate with the program's colors:
  // the first 10 lines and "… N more lines" once done, the last 10 and "… N
  // earlier lines" while streaming, the activity mark under the latest line.
  // A click or Space shows every line (CHAT-042). Long lines never wrap;
  // Shift+← →, H, L and a sideways wheel scroll them 5 characters at a time,
  // with a "…" at the left edge while scrolled (CHAT-043). c/y copies "$ cmd"
  // and the output without colors (CHAT-044). Port of chat/shell.go.
  //   item  a ShellItem (lib/types.js)
  import { onDestroy } from "svelte";
  import { cutLeft } from "../../lib/diff.js";
  import { ansiLines, stripAnsi } from "../../lib/render/ansi.js";
  import { selectItem } from "../../lib/stores/ui.svelte.js";
  import { singleClick } from "../../lib/tools/click.js";
  import { copyWithNotice } from "../../lib/tools/clip.js";
  import { SHELL_COPIED, shellCopyText } from "../../lib/tools/copy.js";
  import { show, size } from "../../lib/tools/nodes.js";
  import { isOpen, registerItem, setOpen } from "../../lib/tools/open.svelte.js";
  import { addProbeReader } from "../../lib/tools/probe.js";
  import { SHELL_STEP, shellView } from "../../lib/tools/shell.js";
  import ActivityMark from "../timeline/ActivityMark.svelte";

  let { item } = $props();

  let output = $state(null);
  let cell = $state(0);
  let offset = $state(0); // cells scrolled, read from the output's scrollLeft

  const expanded = $derived(isOpen(item.key));
  const view = $derived(shellView({ command: item.command, output: item.output, exitCode: item.exitCode, running: item.running, expanded }));
  const fragments = $derived(ansiLines(view.lines.join("\n")));
  const plain = $derived(view.lines.map((line) => stripAnsi(line)));
  // shell.go:303-306: a scrolled line that still shows text gets the mark.
  const leads = $derived(plain.map((line) => offset > 0 && cutLeft(line, offset).trim() !== ""));

  function toggle() {
    setOpen(item.key, !isOpen(item.key));
  }

  function copy() {
    return copyWithNotice(shellCopyText(item.command, item.output), SHELL_COPIED);
  }

  // dir −1 left, +1 right; the web view clamps at the longest line.
  function scroll(dir) {
    if (!output || cell <= 0) return false;
    output.scrollLeft = Math.max(0, output.scrollLeft + dir * SHELL_STEP * cell);
    onScroll();
    return true;
  }

  function onScroll() {
    if (output && cell > 0) offset = Math.round(output.scrollLeft / cell);
  }

  const click = singleClick(() => toggle());
  function onClick(event) {
    if (item.key) selectItem(item.key);
    click.onclick(event);
  }

  $effect(() => {
    if (!item.key) return;
    return registerItem(item.key, { toggle, copy, scroll });
  });

  const stopProbe = addProbeReader(
    "shells",
    Symbol("shell"),
    () => ({
      key: item.key,
      request_id: item.requestId,
      command: view.command,
      mark: view.mark?.text ?? "",
      running: item.running,
      open: expanded,
      lines: plain,
      above: view.above,
      below: view.below,
      spinner: view.spinner,
      offset,
    }),
    () => ({ key: item.key, scroll_left: output?.scrollLeft ?? 0, cell, leads: leads.filter(Boolean).length, text: output?.textContent ?? "" }),
  );
  onDestroy(() => {
    stopProbe();
    click.cancel();
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="shell" data-shell={item.key} onpointerdown={click.onpointerdown} onclick={onClick}>
  <p class="command">
    <span class="text">$ {view.command}</span>{#if item.running}<span class="state"> Running…</span>{:else if view.mark}<span
        class="state {view.mark.tone}"> {view.mark.text}</span
      >{/if}
  </p>
  {#if view.above}<p class="more">{view.above}</p>{/if}
  {#if view.lines.length > 0}
    <div class="output-box">
      <div class="output" bind:this={output} onscroll={onScroll}>
        <span class="ruler" aria-hidden="true" {@attach size((rect) => (cell = rect.width / 10))}>0000000000</span>
        {#each fragments as line, n (n)}
          <div class="line" {@attach show(() => line)}></div>
        {/each}
      </div>
      {#if offset > 0}
        <div class="leads" aria-hidden="true">
          {#each leads as lead, n (n)}<div class="lead">{lead ? "…" : ""}</div>{/each}
        </div>
      {/if}
    </div>
  {/if}
  {#if view.below}<p class="more">{view.below}</p>{/if}
  {#if view.spinner}<div class="working"><ActivityMark size="line" /></div>{/if}
</div>

<style>
  .shell {
    min-width: 0;
    padding-left: calc(16 * var(--px));
    border-left: 1px solid var(--ink);
  }
  .shell p {
    margin: 0;
  }
  .command {
    display: flex;
    min-width: 0;
    font: var(--t-mono-strong);
    white-space: pre;
  }
  .command .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .state {
    flex: none;
    font-weight: 400;
    color: var(--slate);
  }
  .state.error {
    color: var(--error-ink);
  }
  .more {
    font: var(--t-small);
    color: var(--slate);
  }
  .output-box {
    position: relative;
    margin-top: calc(4 * var(--px));
  }
  .output {
    position: relative;
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
    font: var(--t-code);
    color: var(--slate);
  }
  .output::-webkit-scrollbar {
    display: none;
  }
  .ruler {
    position: absolute;
    visibility: hidden;
    pointer-events: none;
    white-space: pre;
  }
  .line {
    min-height: calc(24 * var(--px));
    white-space: pre;
  }
  /* The "…" marks sit over the left edge of each scrolled line. */
  .leads {
    position: absolute;
    top: 0;
    left: 0;
    pointer-events: none;
    font: var(--t-code);
    color: var(--slate);
  }
  .lead {
    height: calc(24 * var(--px));
    min-width: 1ch;
    background: var(--paper);
  }
  .lead:empty {
    background: transparent;
  }
  .working {
    display: flex;
    align-items: center;
    height: calc(24 * var(--px));
  }
</style>
