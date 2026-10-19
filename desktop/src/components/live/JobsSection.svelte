<script>
  // The tray's jobs entry (desktop-design-5-6 2.15, look 5.15; LIVE-001,
  // LIVE-002, CHAT-062): the gear mark, then "1 background process" or "<N>
  // background processes" (internal/ui/model/tasks.go:259-264); server jobs
  // are the left list's (G-18). A click opens Background Processes; the keys
  // are the section's onKey (plugins/live.js).
  import Icon from "../Icon.svelte";
  import { openSheet, trayHint, trayEntryFocused } from "../../lib/live/helpers.svelte.js";
  import { jobsLabel, trayJobs } from "../../lib/live/jobs.svelte.js";

  const label = $derived(jobsLabel(trayJobs().length));
  const hint = $derived(trayHint("jobs"));
  const focused = $derived(trayEntryFocused("jobs"));
</script>

<button type="button" class="entry" class:focused tabindex="-1" data-tray-row data-live-entry="jobs" onclick={() => openSheet("background")}>
  <span class="mark"><Icon name="gear" size={18} /></span>
  <span class="label">{label}</span>
  {#if hint}<span class="hint">{hint}</span>{/if}
</button>

<style>
  /* As the helpers entry: it fills its tray section, 36 high. */
  .entry {
    position: relative;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    width: calc(100% + 40 * var(--px));
    height: calc(36 * var(--px));
    margin: calc(-6 * var(--px)) calc(-20 * var(--px));
    padding: 0 calc(20 * var(--px));
    text-align: left;
    color: var(--ink);
  }
  .mark {
    flex: none;
    display: grid;
    place-items: center;
    color: var(--slate);
  }
  .label {
    flex: none;
    font: var(--t-small);
    white-space: nowrap;
  }
  .hint {
    min-width: 0;
    margin-left: auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-small);
    color: var(--slate);
  }
  .entry.focused,
  .entry:hover {
    background: var(--band);
  }
  .entry.focused::after,
  .entry:hover::after {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }
  .entry.focused .hint,
  .entry:hover .hint {
    color: var(--ink);
  }
</style>
