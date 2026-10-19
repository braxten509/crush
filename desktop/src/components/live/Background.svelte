<script>
  // The Background Processes sheet, and with kind "sub-agents" the Sub-agents
  // sheet (desktop-design-5-6 2.15, look 5.15; DLG-223 to DLG-230): a port of
  // internal/ui/dialog/background.go, one sheet for both as there. Rows are
  // the shown chat's running helpers, oldest first, or its jobs that are no
  // server; each row is its mark, the name or command, and the info line,
  // whose elapsed time is redrawn every second. A refresh keeps the selection
  // on the same row (by identity, else at the same place). ↑/k/Ctrl+P and
  // ↓/j/Ctrl+N move and wrap; Enter opens a helper's chat; x, Delete and
  // Backspace stop or kill at once; Esc and Alt+Esc close (KIT's frame, which
  // also hands the keys to onKey). A click selects, a double click opens a
  // helper, and the selected or hovered row has a Stop or Kill button. With no
  // rows (on the start screen too) the sheet says so and stays open.
  import { onMount, untrack } from "svelte";
  import Icon from "../Icon.svelte";
  import Scrollbar from "../kit/Scrollbar.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import HelperView from "./HelperView.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { comboOf } from "../../lib/keys.js";
  import { helperInfo, probes, runningHelpers, secondNow, SHEET, stopHelper } from "../../lib/live/helpers.svelte.js";
  import { jobInfo, jobKey, killJob, trayJobs } from "../../lib/live/jobs.svelte.js";
  import { showMain } from "../../lib/mainview.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { ui } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { ticker } from "../../lib/ticker.svelte.js";

  let { kind = SHEET.jobs } = $props();

  const forHelpers = $derived(kind === SHEET.helpers);
  // The terminal's texts (background.go:91-100, :205-213, :224-227).
  const title = $derived(forHelpers ? "Sub-agents" : "Background Processes");
  const empty = $derived(forHelpers ? "No sub-agents are running." : "No background processes are running.");
  const action = $derived(forHelpers ? "Stop" : "Kill");
  const help = $derived([
    { key: "enter", text: "open chat", disabled: !forHelpers },
    { key: "↑/↓", text: "choose" },
    { key: "x", text: forHelpers ? "stop" : "kill" },
    { key: "esc", text: "exit" },
  ]);
  const scrollbar = $derived(ws.config?.options?.tui?.scrollbar ?? "default");

  const now = $derived(secondNow());
  const rows = $derived(
    forHelpers
      ? runningHelpers().map((h) => ({ key: h.id, text: h.name ?? "", info: helperInfo(h, now), helper: h }))
      : trayJobs().map((job) => ({ key: jobKey(job), text: job.command ?? "", info: jobInfo(job, now), job })),
  );

  // The selection: the row with this identity, else the row now at its place.
  let selectedKey = $state(null);
  let selectedAt = $state(0);
  const selected = $derived.by(() => {
    if (rows.length === 0) return -1;
    const at = selectedKey === null ? -1 : rows.findIndex((row) => row.key === selectedKey);
    return at >= 0 ? at : Math.max(0, Math.min(selectedAt, rows.length - 1));
  });
  $effect(() => {
    const at = selected;
    const key = rows[at]?.key ?? null;
    untrack(() => {
      if (at < 0) return;
      selectedAt = at;
      selectedKey = key;
    });
  });

  let list = $state(null);

  function select(at) {
    if (at < 0 || at >= rows.length) return;
    selectedAt = at;
    selectedKey = rows[at].key;
    queueMicrotask(() => list?.querySelector(`[data-row="${at}"]`)?.scrollIntoView({ block: "nearest" }));
  }

  // ↑ on the first row goes to the last and ↓ on the last to the first (background.go:145-158).
  function move(step) {
    if (rows.length > 0) select((selected + step + rows.length) % rows.length);
  }

  // The row leaves at once and the selection stays at its place (background.go:163-171).
  function stop(row) {
    if (!row) return;
    selectedAt = rows.indexOf(row);
    selectedKey = null;
    if (row.helper) stopHelper(row.helper);
    else killJob(row.job);
  }

  // Enter ("open chat") on a helper with a chat of its own, while no form holds
  // the typing box's place: the helper view takes the middle column and this
  // sheet closes (internal/ui/model/subagent_view.go:53-64; DLG-228).
  function open(row) {
    const helper = row?.helper;
    if (!helper?.child_session_id || ui.slot || helper.session_id !== sessions.currentId) return false;
    closeDialog(SHEET.helpers);
    showMain({ kind: "helper", component: HelperView, props: { helper }, key: `helper:${helper.id}` });
    return true;
  }

  // The keys KIT hands to the front sheet. Esc and Alt+Esc are left to KIT,
  // which closes the sheet.
  function onKey(event) {
    switch (comboOf(event)) {
      case "ArrowUp":
      case "k":
      case "Ctrl+P":
        move(-1);
        return true;
      case "ArrowDown":
      case "j":
      case "Ctrl+N":
        move(1);
        return true;
      case "Enter":
        if (forHelpers) open(rows[selected]);
        return true;
      case "x":
      case "Delete":
      case "Backspace":
        stop(rows[selected]);
        return true;
      default:
        return false;
    }
  }

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  onMount(() => {
    const probe = {
      light: () => ({
        id: kind,
        title,
        rows: rows.map((row, at) => ({ key: row.key, text: row.text, info: row.info, selected: at === selected })),
        selected: rows[selected]?.key ?? null,
        empty: rows.length === 0 ? empty : null,
        action,
        help: help.filter((pair) => !pair.disabled).map(({ key, text }) => ({ key, text })),
      }),
      heavy: () => ({
        list: rectOf(list),
        rows: [...(list?.querySelectorAll("[data-row]") ?? [])].map((el) => ({
          key: el.dataset.rowKey,
          rect: rectOf(el),
          button: rectOf(el.querySelector(".act")),
          button_shown: el.querySelector(".act") ? getComputedStyle(el.querySelector(".act")).visibility === "visible" : false,
        })),
      }),
    };
    probes.sheet = probe;
    return () => {
      if (probes.sheet === probe) probes.sheet = null;
    };
  });
</script>

<Sheet id={kind} {title} width="standard" {help} {onKey}>
  {#if rows.length === 0}
    <p class="empty">{empty}</p>
  {:else}
    <div class="list" bind:this={list} role="listbox" aria-label={title}>
      {#each rows as row, at (row.key)}
        <!-- The keys are the sheet's (onKey); a click selects, a double click opens. -->
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div
          class="row"
          class:selected={at === selected}
          role="option"
          tabindex="-1"
          aria-selected={at === selected}
          data-row={at}
          data-row-key={row.key}
          onclick={() => select(at)}
          ondblclick={() => forHelpers && open(row)}
        >
          {#if forHelpers}
            <span class="mark busy" style:transform="rotate({90 * ticker.second}deg)"><Icon name="half-circle" size={18} /></span>
          {:else}
            <span class="mark"><Icon name="gear" size={18} /></span>
          {/if}
          <span class="name">{row.text}</span>
          <span class="info">{row.info}</span>
          <span class="slot">
            <button
              type="button"
              class="act"
              tabindex="-1"
              onclick={(event) => {
                event.stopPropagation();
                stop(row);
              }}>{action}</button
            >
          </span>
        </div>
      {/each}
    </div>
    <Scrollbar target={list} mode={scrollbar} />
  {/if}
</Sheet>

<style>
  .empty {
    margin: 0;
    padding: calc(20 * var(--px)) 0 calc(4 * var(--px)) calc(23 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
  }
  /* At most eight rows show; the rest scroll (KIT's Scrollbar draws the bar). */
  .list {
    max-height: calc(8 * 64 * var(--px));
    overflow-y: auto;
  }
  /* Two-line rows 64 high (look 5.1): the mark and the name on the first line,
     the info 5 below it, and the action's slot kept at the right end. */
  .row {
    position: relative;
    box-sizing: border-box;
    display: grid;
    grid-template-columns: calc(18 * var(--px)) minmax(0, 1fr) auto;
    grid-template-rows: calc(28 * var(--px)) calc(24 * var(--px));
    align-content: center;
    column-gap: calc(12 * var(--px));
    row-gap: calc(5 * var(--px));
    height: calc(64 * var(--px));
    padding: 0 calc(24 * var(--px)) 0 calc(23 * var(--px));
    user-select: none;
    outline: none;
  }
  .row:hover {
    background: var(--control-hover);
  }
  /* The selected row: the band between faint lines, with the vermilion edge. */
  .row.selected {
    background: var(--band);
  }
  .row.selected::before {
    content: "";
    position: absolute;
    inset: 0;
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    pointer-events: none;
  }
  .row.selected::after {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }
  .mark {
    grid-column: 1;
    grid-row: 1;
    display: grid;
    place-items: center;
    color: var(--slate);
  }
  .mark.busy {
    color: var(--busy);
  }
  .name {
    grid-column: 2;
    grid-row: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-item);
    color: var(--ink);
  }
  .info {
    grid-column: 2;
    grid-row: 2;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-small);
    color: var(--info-soft);
  }
  .row:hover .info,
  .row.selected .info {
    color: var(--ink);
  }
  .slot {
    grid-column: 3;
    grid-row: 1 / span 2;
    align-self: center;
  }
  /* Stop and Kill: a 5.1 button at height 32, shown on the selected and the
     hovered row; hidden, it keeps its place. */
  .act {
    box-sizing: border-box;
    min-width: calc(112 * var(--px));
    height: calc(32 * var(--px));
    padding: 0 calc(20 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--control-rest);
    font: var(--t-button);
    line-height: calc(30 * var(--px));
    color: var(--ink);
    visibility: hidden;
  }
  .row.selected .act,
  .row:hover .act {
    visibility: visible;
  }
  .act:hover {
    background: var(--control-hover);
  }
</style>
