<script>
  // The help sheet (SIDE-027, SIDE-029, SIDE-035, TYPE-039; 4.14): Ctrl+G
  // shows the terminal's full help over the timeline's bottom, above the
  // composer, at most half the timeline's height; Ctrl+G or Esc closes it
  // and nothing below moves (lib/plugins/panel.js has the keys). The entries
  // follow the state and focus (panel/help.js, a port of help.go): three
  // columns of key and action pairs under small-caps group labels. It is not
  // a sheet of the stack: keys keep their usual way (1.8). Placed by
  // OverlayHost.
  import { unfold } from "../lib/motion.js";
  import { api } from "../lib/engine/api.js";
  import { sessions } from "../lib/stores/sessions.svelte.js";
  import { ui } from "../lib/stores/ui.svelte.js";
  import { helpGroups } from "./panel/help.js";
  import { helpNow, panel } from "./panel/panel.svelte.js";
  import { timelineBox, trackBox } from "./panel/place.js";
  import Rule from "./Rule.svelte";

  // With no chat shown, the project may be waiting for its set-up (the
  // terminal's uiInitialize, START-002): asked once each time help opens.
  $effect(() => {
    if (!ui.help || sessions.currentId) return;
    let live = true;
    api.needsInit().then(
      (answer) => {
        if (live) panel.needsInit = answer?.needs_init === true;
      },
      () => {},
    );
    return () => {
      live = false;
      panel.needsInit = false;
    };
  });

  const groups = $derived(helpGroups(helpNow()));

  let box = $state(null);
  $effect(() => {
    if (!ui.help) return;
    return trackBox(() => (box = timelineBox()));
  });
</script>

{#if ui.help && box}
  <div
    class="help-sheet"
    role="region"
    aria-label="Keyboard help"
    data-help-sheet
    transition:unfold={{ from: "top" }}
    style:left="{box.left}px"
    style:width="{box.width}px"
    style:bottom="{window.innerHeight - box.bottom}px"
    style:max-height="{Math.max(0, (box.bottom - box.top) / 2)}px"
  >
    <Rule marks="both" class="help-rule" />
    <div class="groups">
      {#each groups as group (group.id)}
        <section class="group" aria-label={group.label}>
          <h2 class="label">{group.label}</h2>
          <dl class="pairs">
            {#each group.entries as entry, i (i)}
              <dt class="key">{entry.key}</dt>
              <dd class="action">{entry.action}</dd>
            {/each}
          </dl>
        </section>
      {/each}
    </div>
  </div>
{/if}

<style>
  .help-sheet {
    position: fixed;
    z-index: 5;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    background: var(--paper-raised);
  }
  .help-sheet :global(.rule.help-rule) {
    flex: none;
  }
  /* The timeline's text column: 66 in from the left, 23 from the right. */
  .groups {
    min-height: 0;
    overflow-y: auto;
    padding: calc(16 * var(--px)) calc(23 * var(--px)) calc(18 * var(--px)) calc(66 * var(--px));
    columns: 3;
    column-gap: calc(32 * var(--px));
  }
  .group {
    break-inside: avoid;
    padding-bottom: calc(14 * var(--px));
  }
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
  }
  .pairs {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    column-gap: calc(12 * var(--px));
    margin: calc(4 * var(--px)) 0 0;
  }
  .key {
    font: var(--t-mono);
    white-space: nowrap;
  }
  .action {
    margin: 0;
    font: var(--t-small);
    line-height: calc(24 * var(--px));
    min-width: 0;
  }
</style>
