<script>
  // BACKGROUND (owner, 2026-10-19: moved here from the tray's entries): the
  // shown chat's running helpers, then its background jobs that are no
  // server (the Background Processes sheet's rows), each with how long it
  // has run. The stop cross keeps its place on every row and shows on hover;
  // it stops a helper or kills a job at once, as the sheet's Stop and Kill.
  import Icon from "../Icon.svelte";
  import { helperInfo, runningHelpers, secondNow, stopHelper } from "../../lib/live/helpers.svelte.js";
  import { jobInfo, jobKey, killJob, trayJobs } from "../../lib/live/jobs.svelte.js";

  const now = $derived(secondNow());
  const rows = $derived([
    ...runningHelpers().map((h) => ({ key: `helper:${h.id}`, text: h.name || "sub-agent", info: helperInfo(h, now), stop: () => stopHelper(h), label: "Stop" })),
    ...trayJobs().map((job) => ({ key: jobKey(job), text: job.command ?? "", info: jobInfo(job, now), stop: () => killJob(job), label: "Kill" })),
  ]);
</script>

<!-- Shown only while it has something in it (owner, 2026-10-20). -->
{#if rows.length > 0}
<section class="panel-section" data-section="background" aria-label="Background">
  <h2 class="label">Background</h2>
  <ul class="rows">
    {#each rows as row (row.key)}
      <li class="line row">
        <span class="bullet" aria-hidden="true">•</span>
        <span class="text" title={row.text}>{row.text}</span>
        <span class="info">{row.info}</span>
        <button class="stop" type="button" tabindex="-1" title={row.label} aria-label="{row.label} {row.text}" onclick={row.stop}><Icon name="cross" size={12} /></button>
      </li>
    {/each}
  </ul>
</section>
{/if}

<style>
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  /* A bulleted list, no lines between rows (owner, 2026-10-20). */
  .rows {
    margin-top: calc(8 * var(--px));
  }
  .line {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-height: calc(28 * var(--px));
    font: var(--t-small);
  }
  .bullet {
    flex: none;
    color: var(--slate);
  }
  .info {
    color: var(--slate);
  }
  .text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .info {
    flex: none;
    font-variant-numeric: tabular-nums;
  }
  .stop {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(20 * var(--px));
    height: calc(20 * var(--px));
    color: var(--slate);
    visibility: hidden;
  }
  .row:hover .stop {
    visibility: visible;
  }
  .stop:hover {
    color: var(--ink);
  }
</style>
