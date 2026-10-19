<script>
  // One tool's line (CHAT-046 to CHAT-048, CHAT-053, desktop-design 4.6): the
  // status mark, the name at weight 500, the main parameter in mono slate
  // (a command colored as shell), the other parameters as "(key=value, …)"
  // only while 30 or more characters stay for the main one (toolParamList),
  // and the +N −N counts at the right end. Expanded, a long parameter wraps
  // under its own first character (a hanging indent).
  //   view      lib/tools/registry.js toolView(…)
  //   status    lib/tools/status.js toolStatus(…)
  //   expanded  wrap the parameters in full
  //   counts    {adds, dels} or null
  import { cells } from "../../lib/diff.js";
  import { codeLine } from "../../lib/tools/highlight.js";
  import { show, size } from "../../lib/tools/nodes.js";
  import { pairsFit, pairsText } from "../../lib/tools/registry.js";
  import { statusMark } from "../../lib/tools/status.js";
  import Icon from "../Icon.svelte";
  import ActivityMark from "../timeline/ActivityMark.svelte";

  let { view, status, expanded = false, counts = null } = $props();

  let room = $state(0);
  let cell = $state(0);

  const mark = $derived(view.forceDone ? "check" : view.pending ? "activity" : statusMark(status));
  const columns = $derived(cell > 0 ? Math.floor(room / cell) : 0);
  const fits = $derived(columns > 0 && pairsFit(view.pairs, columns, cells));
  const wraps = $derived(expanded || view.wrap);

  export function probe() {
    return {
      mark,
      name: nameOf(view.name),
      main: view.main,
      note: view.note,
      pairs: fits ? `(${pairsText(view.pairs)})` : "",
      all_pairs: view.pairs.length > 0 ? `(${pairsText(view.pairs)})` : "",
      room: columns,
      invalid: view.invalid,
      wraps,
    };
  }

  function nameOf(name) {
    if (name.server !== undefined) return `${name.server} → ${name.tool}`;
    if (name.docker !== undefined) return `${name.docker} → ${name.action}`;
    return name.text;
  }
</script>

<span class="header" class:wraps>
  <span class="mark" data-mark={mark}>
    {#if mark === "activity"}
      <ActivityMark size="small" />
    {:else}
      <Icon name={mark} class={mark} />
    {/if}
  </span>
  {#if view.invalid}
    <!-- The terminal draws "ERROR Invalid parameters" in place of the header. -->
    <span class="tag">ERROR</span><span class="invalid">{view.invalid}</span>
  {:else}
    <span class="name">
      {#if view.name.server !== undefined}
        {view.name.server}<span class="arrow"> → </span>{view.name.tool}
      {:else if view.name.docker !== undefined}
        {view.name.docker}<span class="arrow"> → </span><span class="action {view.name.tone}">{view.name.action}</span>
      {:else}
        {view.name.text}
      {/if}
    </span>
  {/if}
  {#if !view.invalid && !view.pending}
    <span class="params" {@attach size((rect) => (room = rect.width))}>
      <span class="ruler" aria-hidden="true" {@attach size((rect) => (cell = rect.width / 10))}>0000000000</span>
      {#if view.job}
        <span class="main"><span class="job-action">({view.job.action})</span> PID {view.job.pid}{#if view.job.description}<span class="note"> {view.job.description}</span>{/if}</span>
      {:else}
        {#if view.main !== "" || view.note !== ""}
          <span class="main" class:ratio={view.ratio}>
            {#if view.mainLang}
              <span class="code" {@attach show(() => codeLine(view.main, view.mainLang))}></span>
            {:else}{view.main}{/if}{#if view.note}<span class="note">{view.note}</span>{/if}
          </span>
        {/if}
        {#if fits}<span class="pairs">({pairsText(view.pairs)})</span>{/if}
      {/if}
    </span>
  {/if}
  {#if counts}
    <span class="counts"><span class="adds">+{counts.adds}</span> <span class="dels">−{counts.dels}</span></span>
  {/if}
</span>

<style>
  .header {
    display: flex;
    align-items: center;
    min-width: 0;
    flex: 1;
    font: var(--t-body-small);
    text-align: left;
  }
  .header.wraps {
    align-items: flex-start;
  }
  /* The mark is 16 wide; the name starts 7 after it, where the group row's
     own text starts (4.6). */
  .mark {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: calc(16 * var(--px));
    height: calc(26 * var(--px));
    margin-right: calc(7 * var(--px));
  }
  .mark :global(.icon.cross) {
    color: var(--error-ink);
  }
  .mark :global(.icon.dot) {
    color: var(--slate);
  }
  .name {
    flex: none;
    font-weight: 500;
    white-space: nowrap;
  }
  .arrow {
    color: var(--slate);
  }
  .action.add {
    color: var(--diff-add-ink);
  }
  .action.remove {
    color: var(--error-ink);
  }
  .params {
    position: relative;
    display: flex;
    align-items: baseline;
    flex: 1;
    min-width: 0;
    margin-left: calc(12 * var(--px));
    font: 400 calc(17 * var(--px)) / calc(26 * var(--px)) var(--face-mono);
    color: var(--slate);
    white-space: pre;
    overflow: hidden;
  }
  .ruler {
    position: absolute;
    visibility: hidden;
    pointer-events: none;
  }
  .main {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pairs {
    flex: none;
    margin-left: 1ch;
  }
  .note,
  .job-action {
    font: var(--t-body-small);
  }
  /* Expanded: the parameter wraps, each wrapped line starting under its own
     first character (CHAT-053). */
  .wraps .params {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .wraps .main {
    overflow: visible;
  }
  .tag {
    flex: none;
    margin-right: calc(10 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    color: var(--error-ink);
  }
  .invalid {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--error-ink);
  }
  .counts {
    flex: none;
    margin-left: calc(16 * var(--px));
    font: 400 calc(17 * var(--px)) / calc(26 * var(--px)) var(--face-mono);
    white-space: nowrap;
  }
  .adds {
    color: var(--diff-add-ink);
  }
  .dels {
    color: var(--error-ink);
  }
</style>
