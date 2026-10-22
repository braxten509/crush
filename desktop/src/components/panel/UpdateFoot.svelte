<script>
  // The panel's foot (owner, 2026-10-20: bottom right, "just one [button]
  // that changes state"): the build this app runs, and one button that
  // checks for a newer one, then offers Update, which installs it and starts
  // the app again (lib/system/appupdate.svelte.js). While it checks or
  // updates, its sync mark turns about its own middle.
  import { appUpdate, pressUpdate, short } from "../../lib/system/appupdate.svelte.js";
  import Icon from "../Icon.svelte";
  import Rule from "../Rule.svelte";

  const LABELS = {
    idle: "Check for updates",
    checking: "Checking…",
    current: "Up to date",
    ready: "Update",
    updating: "Updating…",
    restarting: "Restarting…",
  };
  const busy = $derived(["checking", "updating", "restarting"].includes(appUpdate.phase));
  const version = $derived(
    appUpdate.phase === "ready" && appUpdate.latest
      ? `${short(appUpdate.current) || "?"} → ${short(appUpdate.latest)}`
      : short(appUpdate.current),
  );
</script>

<Rule marks="both" class="above-update" />
<div class="foot" data-probe="update-foot" data-phase={appUpdate.phase}>
  <span class="version" title={appUpdate.current ? `This build: ${appUpdate.current}` : ""}>{version}</span>
  <button
    type="button"
    class="update"
    class:ready={appUpdate.phase === "ready"}
    class:turning={busy}
    disabled={busy}
    data-update-button
    onclick={() => pressUpdate()}
  >
    {#if busy}<span class="mark"><Icon name="update" size={12} /></span>
    {:else if appUpdate.phase === "current"}<span class="mark"><Icon name="check" size={12} /></span>{/if}
    <span class="label">{LABELS[appUpdate.phase] ?? LABELS.idle}</span>
  </button>
</div>

<style>
  /* Under the panel's sections, a section rule like the one under the
     meters (clear ends), then the foot. */
  :global(.rule.above-update) {
    flex: none;
    margin: 0 calc(-19 * var(--px)) 0 calc(-26 * var(--px));
  }
  .foot {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: calc(12 * var(--px));
    padding: calc(12 * var(--px)) 0 calc(14 * var(--px));
  }
  .version {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    /* The foot's words are as big as Settings' across the window (owner,
       2026-10-21). */
    font: var(--t-body-small);
    font-variant-numeric: tabular-nums;
    color: var(--slate);
  }
  .update {
    flex: none;
    display: flex;
    align-items: center;
    gap: calc(6 * var(--px));
    box-sizing: border-box;
    height: calc(28 * var(--px));
    padding: 0 calc(12 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--control-rest);
    color: var(--ink);
  }
  .update:hover:not(:disabled) {
    background: var(--control-hover);
  }
  .update.ready {
    border-color: var(--vermilion-fill);
    background: var(--vermilion-fill);
    color: var(--on-accent);
  }
  .update.ready:hover {
    background: var(--vermilion-fill);
    filter: brightness(1.08);
  }
  .update:disabled {
    cursor: default;
    color: var(--slate);
  }
  .label {
    font: var(--t-body-small);
    white-space: nowrap;
  }
  .mark {
    display: grid;
    place-items: center;
  }
  /* The whole <svg> turns about its centre, the ring's centre (NoticeLine). */
  @media (prefers-reduced-motion: no-preference) {
    .turning .mark :global(svg) {
      transform-origin: 50% 50%;
      animation: notice-turn 1.2s linear infinite;
    }
  }
</style>
