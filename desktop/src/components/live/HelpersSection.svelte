<script>
  // The tray's helpers entry (desktop-design-5-6 2.15, look 5.15; LIVE-001,
  // LIVE-002): the helpers mark turning a quarter each wall-clock second, then
  // "1 subagent" or "<N> subagents" (internal/ui/model/tasks.go:244-257). A
  // click opens the Sub-agents sheet; the keys are the section's onKey
  // (plugins/live.js). The tray's key hint sits after the last of the two
  // entries (tasks.go:277-287).
  import Icon from "../Icon.svelte";
  import { helpersLabel, openSheet, runningHelpers, trayHint, trayEntryFocused } from "../../lib/live/helpers.svelte.js";
  import { ticker } from "../../lib/ticker.svelte.js";

  const label = $derived(helpersLabel(runningHelpers().length));
  const hint = $derived(trayHint("helpers"));
  const focused = $derived(trayEntryFocused("helpers"));
</script>

<button type="button" class="entry" class:focused tabindex="-1" data-tray-row data-live-entry="helpers" onclick={() => openSheet("sub-agents")}>
  <span class="mark" style:transform="rotate({90 * ticker.second}deg)"><Icon name="half-circle" size={18} /></span>
  <span class="label">{label}</span>
  {#if hint}<span class="hint">{hint}</span>{/if}
</button>

<style>
  /* The entry fills its tray section (whose padding is 6 and 20), 36 high. */
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
    color: var(--busy);
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
  /* Focused or hovered: the band with the vermilion edge (look 5.0's active state). */
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
