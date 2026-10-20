<script>
  // One tool call (CHAT-046 to CHAT-079, desktop-design 4.6): its line and,
  // when open, its hook lines, an agent's task and tree, and its body. In a
  // group a tool is compact (its line only) until a click opens it
  // (CHAT-086); the open body starts at 10 lines, and a click on the body or
  // its hidden-lines note shows every line and wraps the line's parameters
  // (CHAT-052, CHAT-053). On its own (Question, Agent, Agentic Fetch, which
  // never fold) a tool is always open and a click expands it. The hook lines
  // sit under the line, above the body, so the clicked line never moves.
  //   call        a ToolCallView (lib/types.js)
  //   itemKey     the timeline item that holds it (selection)
  //   nested      in a group
  //   workingDir  the project folder
  //   sessionId   the chat
  //   xOffset     side-scroll of its diff
  import { onDestroy } from "svelte";
  import { prompts } from "../../lib/stores/prompts.svelte.js";
  import { selectItem } from "../../lib/stores/ui.svelte.js";
  import { singleClick } from "../../lib/tools/click.js";
  import { detailFor, detailState, requestDetail } from "../../lib/tools/details.svelte.js";
  import { callHookLines } from "../../lib/tools/hooks.js";
  import { isOpen, setOpen } from "../../lib/tools/open.svelte.js";
  import { addProbeReader } from "../../lib/tools/probe.js";
  import { lineCounts, toolView } from "../../lib/tools/registry.js";
  import { toolStatus } from "../../lib/tools/status.js";
  import Icon from "../Icon.svelte";
  import AgentTree from "./AgentTree.svelte";
  import HookLines from "./HookLines.svelte";
  import ToolBody from "./ToolBody.svelte";
  import ToolHeader from "./ToolHeader.svelte";

  let { call, itemKey = "", nested = false, workingDir = "", sessionId = "", xOffset = 0 } = $props();

  let header = $state(null);
  let tree = $state(null);
  let content = $state(null);

  const openKey = $derived("open:" + call.id);
  const fullKey = $derived("full:" + call.id);
  const shown = $derived(!nested || isOpen(openKey));
  const full = $derived(shown && isOpen(fullKey));
  const status = $derived(toolStatus(call, prompts.callStatus));
  // A saved result's diff comes from its review record (lib/tools/details.js);
  // until it is read the body draws without it.
  const drawn = $derived(shown ? (detailFor(call, sessionId) ?? call) : call);
  const view = $derived(toolView(drawn, { status, workingDir, compact: !shown }));
  const hooks = $derived(shown ? callHookLines(call) : []);

  $effect(() => {
    if (shown) requestDetail(call, sessionId);
  });
  const counts = $derived(lineCounts(call));

  // A line's click opens or closes a nested tool, and expands one on its own.
  export function toggle() {
    if (nested) {
      const next = !isOpen(openKey);
      setOpen(openKey, next);
      if (!next) setOpen(fullKey, false);
    } else setOpen(fullKey, !isOpen(fullKey));
  }

  export function expand(on = !full) {
    setOpen(openKey, true);
    setOpen(fullKey, on);
  }

  const lineClick = singleClick(() => toggle());
  const bodyClick = singleClick(() => expand());

  function onLineClick(event) {
    if (itemKey) selectItem(itemKey);
    lineClick.onclick(event);
  }

  // Clicks on the body's own buttons and links do their own thing.
  function onBodyClick(event) {
    if (itemKey) selectItem(itemKey);
    if (event.target?.closest?.("button, a, input")) return;
    bodyClick.onclick(event);
  }

  function partsProbe() {
    return view.body.map((part) => {
      switch (part.type) {
        case "early":
        case "empty":
        case "started":
          return { type: part.type, text: part.text };
        case "error":
        case "note":
          return { type: part.type, tag: part.tag, text: part.text };
        case "image":
          return { type: "image", mime: part.mime, size: part.size };
        case "skill":
          return { type: "skill", name: part.name, description: part.description };
        case "diff":
          return { type: "diff", files: part.files.map((f) => f.path), cut_above: part.cutAbove };
        case "todos":
          return { type: "todos", items: part.items.map((i) => `${i.icon} ${i.text}`) };
        case "code":
          return { type: "code", path: part.path, offset: part.offset };
      }
      return { type: part.type };
    });
  }

  const stopProbe = addProbeReader(
    "tools",
    Symbol("tool"),
    () => ({
      call_id: call.id,
      item: itemKey,
      tool: call.name,
      nested,
      status,
      open: shown,
      full,
      header: header?.probe?.() ?? null,
      hooks: hooks.map((h) => ({ name: h.name, matcher: h.matcher, detail: h.detail.text, extra: h.detail.rewrote || h.detail.reason || "" })),
      agent: tree?.probe?.() ?? null,
      body: shown ? partsProbe() : [],
      detail: detailState(call, sessionId),
      counts,
    }),
    () => {
      const r = content?.getBoundingClientRect();
      return {
        call_id: call.id,
        text: content?.textContent ?? "",
        body_rect: r && { x: r.x, y: r.y, w: r.width, h: r.height },
        hidden_note: content?.querySelector(".hidden-note")?.textContent ?? "",
        code_first: Number(content?.querySelector(".gutter")?.textContent ?? 0) || null,
        code_lines: content?.querySelectorAll(".code .line").length ?? 0,
        output_lines: content?.querySelectorAll(".output .line").length ?? 0,
      };
    },
  );
  onDestroy(() => {
    stopProbe();
    lineClick.cancel();
    bodyClick.cancel();
  });
</script>

<div class="entry" class:nested class:shown data-tool={call.id}>
  <button class="tool-line" type="button" aria-expanded={shown && (nested ? true : full)} onpointerdown={lineClick.onpointerdown} onclick={onLineClick}>
    {#if nested}<span class="disclosure" class:open={shown}><Icon name="disclosure" size={10} /></span>{/if}
    <ToolHeader bind:this={header} {view} {status} expanded={full} {counts} />
  </button>
  {#if shown && (hooks.length > 0 || view.agent || view.body.length > 0)}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="content" bind:this={content} onpointerdown={bodyClick.onpointerdown} onclick={onBodyClick}>
      <HookLines lines={hooks} />
      {#if view.agent}
        <AgentTree bind:this={tree} {call} agent={view.agent} expanded={full} {workingDir} />
      {/if}
      {#if view.body.length > 0}
        <div class="body-box" class:wide={view.wide}>
          <ToolBody parts={view.body} expanded={full} {sessionId} messageId={call.messageId} onexpand={() => expand(true)} {xOffset} />
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .entry {
    min-width: 0;
  }
  .tool-line {
    position: relative;
    display: flex;
    align-items: center;
    box-sizing: border-box;
    width: 100%;
    min-height: calc(36 * var(--px));
    padding: calc(5 * var(--px)) calc(12 * var(--px)) calc(5 * var(--px)) calc(21 * var(--px));
    text-align: left;
    cursor: pointer;
  }
  /* A nested tool's disclosure sits in the margin left of its mark; it shows
     while the tool is open, or hovered or focused, so nothing moves. */
  .disclosure {
    position: absolute;
    left: calc(6 * var(--px));
    top: calc(18 * var(--px));
    transform: translateY(-50%);
    display: flex;
    color: var(--slate);
    visibility: hidden;
  }
  .disclosure :global(.icon) {
    transition: transform var(--motion) var(--ease-paper);
  }
  .disclosure.open :global(.icon) {
    transform: rotate(90deg);
  }
  .tool-line:hover .disclosure,
  .tool-line:focus-visible .disclosure,
  .disclosure.open {
    visibility: visible;
  }
  @media (prefers-reduced-motion: reduce) {
    .disclosure :global(.icon) {
      transition: none;
    }
  }
  /* The body opens under its line, indented to the name (4.6). */
  .content {
    display: flex;
    flex-direction: column;
    gap: calc(10 * var(--px));
    min-width: 0;
    padding: 0 calc(12 * var(--px)) calc(12 * var(--px)) calc(44 * var(--px));
  }
  .body-box {
    min-width: 0;
  }
  /* The call's output settles in under its line (the paper motion). */
  .content {
    animation: paper-settle var(--motion) var(--ease-paper);
  }
</style>
