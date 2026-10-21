<script>
  // A folded tool run (CHAT-080 to CHAT-086, desktop-design 4.6): one row
  // that says how many tools ran and what they did. Scaffold S3 moved this
  // row's markup and CSS out of Conversation.svelte; the approved picture
  // (edition.png, key C) is kept as it was.
  //
  // After "Ran N tools" and the dot: the files the run changed ("edited a.go
  // +3 −1"), or the tools' verbs when none changed ("read session.go, ran a
  // command"); " · N backgrounded"; " · N failed" in --error-ink; while the
  // last group works, its live status (StatusLine, P3.5) after any line
  // counts. The group's mark comes first after the dot: a cross when the
  // last tool failed, a gear when it went on in the background, the
  // activity mark while a tool still streams; none when done (CHAT-081).
  // "Changes" at the right end, only when the run changed files, opens the
  // Changes sheet (CHAT-084). A click opens the group: its last 5 tools and
  // "… N earlier", a text button that shows the rest (CHAT-086). The open
  // state is kept by group id for the window's life (CHAT-080).
  //   item  a ToolGroupItem (lib/types.js); scaffold S3's sample item
  //         {count, summary} without calls still draws as before
  //
  // Under a working group, the registered tool hint (lib/toolhints.js;
  // LIVE's 60-second hint, desktop-design-5-6 6.3) when it gives one.
  import { onDestroy } from "svelte";
  import { openDialog } from "../../lib/dialogs.js";
  import { settle } from "../../lib/motion.js";
  import { prompts } from "../../lib/stores/prompts.svelte.js";
  import { selectItem } from "../../lib/stores/ui.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { cells } from "../../lib/diff.js";
  import { singleClick } from "../../lib/tools/click.js";
  import { copyWithNotice } from "../../lib/tools/clip.js";
  import { TOOL_COPIED, toolCopyText } from "../../lib/tools/copy.js";
  import { allShown, isOpen, registerItem, setOpen, showAll } from "../../lib/tools/open.svelte.js";
  import { addProbeReader } from "../../lib/tools/probe.js";
  import { activityKey, activityStart, groupStatus, toolStatus } from "../../lib/tools/status.js";
  import { SHELL_STEP } from "../../lib/tools/shell.js";
  import { groupRow } from "../../lib/tools/summary.js";
  import { toolView } from "../../lib/tools/registry.js";
  import { toolHint } from "../../lib/toolhints.js";
  import Icon from "../Icon.svelte";
  import ActivityMark from "../timeline/ActivityMark.svelte";
  import StatusLine from "../timeline/StatusLine.svelte";
  import ToolEntry from "./ToolEntry.svelte";

  let { item } = $props();

  // toolgroup.go toolGroupShown.
  const SHOWN = 5;

  let rowEl = $state(null);
  let changesEl = $state(null);
  let listEl = $state(null);

  const sample = $derived(!Array.isArray(item.calls));
  const calls = $derived(sample ? [] : item.calls);
  const groupId = $derived(item.groupId ?? item.id ?? item.key);
  const openKey = $derived("group:" + groupId);
  const open = $derived(!sample && isOpen(openKey));
  const workingDir = $derived(conn.project?.cwd ?? "");

  // The live status: the timeline's, else the words this group's last tool
  // gives while the group works.
  const live = $derived.by(() => {
    if (sample) return null;
    if (item.live) return item.live;
    if (!item.working) return null;
    const last = calls.at(-1);
    const words = groupStatus({ live: true, last: last ? { kind: "tool", name: last.name, status: toolStatus(last, prompts.callStatus) } : null, busy: true });
    if (!words) return null;
    return { kind: "status", key: "status:" + groupId, version: 0, letter: "", sessionId: item.sessionId, messageId: item.messageId, words, startedAt: activityStart(groupId, activityKey(words, last?.id ?? "")), thought: "" };
  });
  const row = $derived(sample ? null : groupRow({ groupKey: item.key, calls, marks: prompts.callStatus, status: live?.words ?? "", working: Boolean(item.working) }));

  // The segments after the dot, in order.
  const segments = $derived.by(() => {
    if (!row) return [];
    const out = [];
    if (row.summary) out.push({ kind: "summary", label: row.summary.label, adds: row.summary.adds, dels: row.summary.dels });
    else if (row.verbs) out.push({ kind: "verbs", label: row.verbs });
    for (const extra of row.extras) out.push({ kind: "extra", label: extra.text, tone: extra.tone });
    return out;
  });
  const mark = $derived(row && !live ? row.mark : null);
  const hint = $derived(!sample && item.working ? toolHint(item) : null);

  const earlier = $derived(open && !allShown.has(openKey) ? Math.max(0, calls.length - SHOWN) : 0);
  const shownCalls = $derived(earlier > 0 ? calls.slice(earlier) : calls);
  const changes = $derived(
    item.changes ?? { sessionId: item.sessionId, messageId: calls[0]?.messageId ?? item.messageId, toolCallIds: calls.map((c) => c.id), title: row?.count ?? "" },
  );

  // Side-scroll of the open diffs (Shift+← →, H, L), up to the longest line.
  let shift = $state(0);
  const longest = $derived.by(() => {
    let max = 0;
    for (const call of calls) {
      if (!isOpen("open:" + call.id)) continue;
      for (const part of toolView(call, { status: toolStatus(call, prompts.callStatus), workingDir }).body) {
        if (part.type !== "diff") continue;
        for (const f of part.files) for (const text of [f.before, f.after]) for (const l of String(text).split("\n")) max = Math.max(max, cells(l));
      }
    }
    return max;
  });

  export function toggle() {
    if (sample) return;
    setOpen(openKey, !isOpen(openKey));
    // syncCompact: a group's tools stay closed until each is opened.
    for (const call of calls) {
      setOpen("open:" + call.id, false);
      setOpen("full:" + call.id, false);
    }
  }

  function copy() {
    if (sample || calls.length === 0) return;
    const text = calls.map((call) => toolCopyText(call, toolStatus(call, prompts.callStatus))).join("\n\n");
    copyWithNotice(text, TOOL_COPIED);
  }

  function scroll(dir) {
    shift = Math.max(0, Math.min(longest, shift + dir * SHELL_STEP));
    return true;
  }

  function openChanges() {
    if (item.key) selectItem(item.key);
    openDialog("changes", { sessionId: changes.sessionId, messageId: changes.messageId, toolCallIds: changes.toolCallIds, title: changes.title });
  }

  const rowClick = singleClick(() => toggle());
  function onRowClick(event) {
    if (item.key) selectItem(item.key);
    rowClick.onclick(event);
  }

  $effect(() => {
    if (!item.key || sample) return;
    return registerItem(item.key, { toggle, copy, scroll });
  });

  const summaryText = (segment) => {
    const counts = segment.adds === null || segment.adds === undefined ? "" : `+${segment.adds} −${segment.dels}`;
    return [segment.label, counts].filter(Boolean).join(" ");
  };

  const stopProbe = addProbeReader(
    "groups",
    Symbol("group"),
    () => ({
      key: item.key,
      group_id: groupId,
      open,
      count: row?.count ?? `Ran ${item.count} tools`,
      mark,
      after: segments.map(summaryText),
      live: live?.words ?? "",
      has_changes: Boolean(row?.hasChanges),
      changes: row?.hasChanges ? changes : null,
      shown: open ? shownCalls.map((c) => c.id) : [],
      earlier,
      shift,
    }),
    () => ({ key: item.key, row_text: rowEl?.textContent ?? "", row_rect: rectOf(rowEl), changes_rect: rectOf(changesEl), list_rect: rectOf(listEl) }),
  );
  onDestroy(() => {
    stopProbe();
    rowClick.cancel();
  });

  function rectOf(el) {
    const r = el?.getBoundingClientRect();
    return r ? { x: r.x, y: r.y, w: r.width, h: r.height } : null;
  }
</script>

<div class="group" class:open>
  {#if sample}
    <button class="tool-row" type="button" aria-expanded="false">
      <Icon name="disclosure" />
      <span class="line"><span class="count">Ran {item.count} tools</span><span class="dot" aria-hidden="true"></span><span class="summary">{item.summary}</span></span>
    </button>
  {:else}
    <button
      class="tool-row"
      class:with-changes={row.hasChanges}
      type="button"
      aria-expanded={open}
      bind:this={rowEl}
      onpointerdown={rowClick.onpointerdown}
      onclick={onRowClick}
    >
      <span class="disclosure" class:open><Icon name="disclosure" /></span>
      <span class="line"
        ><span class="count">{row.count}</span
        >{#if mark || segments.length > 0 || live}<span class="dot" aria-hidden="true"></span>{/if}{#if mark === "cross"}<span class="mark cross"
            ><Icon name="cross" /></span
          >{:else if mark === "gear"}<span class="mark gear"><Icon name="gear" /></span>{:else if mark === "activity"}<span class="mark"
            ><ActivityMark size="small" /></span
          >{/if}{#each segments as segment, i (i)}{#if i > 0}<span class="dot" aria-hidden="true"></span>{/if}{#if segment.kind === "extra"}<span
              class="summary"
              class:error={segment.tone === "error"}>{segment.label}</span
            >{:else}<span class="summary">{segment.label}</span>{#if segment.adds !== null && segment.adds !== undefined}{segment.label ? " " : ""}<span
                class="adds">+{segment.adds}</span
              > <span class="dels">−{segment.dels}</span>{/if}{/if}{/each}{#if live}{#if segments.length > 0}<span class="dot" aria-hidden="true"
            ></span>{/if}<span class="live"><StatusLine item={live} /></span>{/if}</span
      >
    </button>
    {#if row.hasChanges}
      <button class="changes" type="button" bind:this={changesEl} onclick={openChanges}>Changes</button>
    {/if}
    {#if open}
      <div class="list" bind:this={listEl} transition:settle={{ fold: true }}>
        {#if earlier > 0}
          <button class="earlier" type="button" onclick={() => showAll(openKey)}>… {earlier} earlier</button>
        {/if}
        {#each shownCalls as call (call.id)}
          <ToolEntry {call} itemKey={item.key} nested={true} {workingDir} sessionId={item.sessionId} xOffset={shift} />
        {/each}
      </div>
    {/if}
    {#if hint !== null}
      <p class="hint">{hint}</p>
    {/if}
  {/if}
</div>

<style>
  .group {
    position: relative;
  }
  .tool-row {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    width: 100%;
    height: calc(44 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(21 * var(--px));
    border: 1px solid var(--rule-control);
    font: var(--t-body-small);
  }
  /* The Changes button sits over the row's right end, so the line stops
     short of it. */
  .tool-row.with-changes {
    padding-right: calc(104 * var(--px));
  }
  .disclosure {
    display: flex;
    flex: none;
  }
  .disclosure :global(.icon) {
    transition: transform var(--motion) var(--ease-paper);
  }
  .disclosure.open :global(.icon) {
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .disclosure :global(.icon) {
      transition: none;
    }
  }
  /* One line of text, so the dot can sit on the letters' middle and a long
     summary ends in an ellipsis. */
  .tool-row .line {
    flex: 1;
    min-width: 0;
    margin-left: calc(15 * var(--px));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    text-align: left;
  }
  .tool-row .count {
    letter-spacing: -0.01em;
  }
  /* vertical-align: middle centres the dot half an x-height above the
     baseline, read from the font itself, in every renderer and at every zoom. */
  .tool-row .dot {
    display: inline-block;
    vertical-align: middle;
    width: calc(4 * var(--px));
    height: calc(4 * var(--px));
    margin: 0 calc(10 * var(--px)) 0 calc(9 * var(--px));
    border-radius: 50%;
    background: var(--ink);
  }
  .tool-row .summary {
    color: var(--slate);
  }
  .tool-row .summary.error {
    color: var(--error-ink);
  }
  .mark {
    display: inline-flex;
    vertical-align: middle;
    margin-right: calc(8 * var(--px));
  }
  .mark.cross {
    color: var(--error-ink);
  }
  .mark.gear {
    color: var(--slate);
  }
  .adds {
    color: var(--diff-add-ink);
  }
  .dels {
    color: var(--error-ink);
  }
  /* The status runs inline in the line, so its words share the row's
     baseline and its dots centre like the separator dot at every zoom; a box
     set by its middle matched only at some zooms (owner, 2026-10-20). */
  .live,
  .live :global(.status) {
    display: inline;
  }
  .changes {
    position: absolute;
    top: 0;
    right: calc(1 * var(--px));
    height: calc(44 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(16 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
    cursor: pointer;
  }
  .changes:hover {
    color: var(--ink);
  }
  /* The open group continues the row's box (4.6). */
  /* The calls settle in under the row and roll up into it closing (the
     paper motion); the box takes its full height at once, so the timeline's
     measures never wait on it. */
  .list {
    border: 1px solid var(--rule-control);
    border-top: 0;
  }
  .list > :global(.entry + .entry),
  .list > .earlier + :global(.entry) {
    border-top: 1px solid var(--rule-faint);
  }
  .earlier {
    display: block;
    box-sizing: border-box;
    width: 100%;
    height: calc(36 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(21 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
    text-align: left;
    cursor: pointer;
  }
  .earlier:hover {
    color: var(--ink);
  }
  .hint {
    margin-top: calc(6 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
</style>
