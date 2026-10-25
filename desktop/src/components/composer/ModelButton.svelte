<script>
  // The model button at the field's right (SIDE-006's label; 4.9): the
  // agent's model and effort as the footer items allow, "Unknown" until the
  // agent has a model. A click opens the model menu over the button
  // (ModelMenu.svelte, owner 2026-10-19), and a second click closes it; the
  // full models sheet stays on Ctrl+P. A sub-agent's chat shows that
  // sub-agent's model in its place, with no picker: its model was fixed when
  // it started (owner, 2026-10-22).
  import Icon from "../Icon.svelte";
  import { modelButtonText } from "../../lib/composer/modes.js";
  import { shownAgent, watchModels } from "../../lib/models/store.svelte.js";
  import { helperAgent } from "../../lib/live/helpers.svelte.js";
  import { helperChats } from "../../lib/stores/helperchats.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { closeMenu, openMenu, ui } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";

  const items = $derived(ws.config?.options?.tui?.composer_footer);
  const helper = $derived(helperChats.byId[sessions.currentId] ?? null);
  const label = $derived(helper ? modelButtonText(helperAgent(helper, ws.providers).agent, items) : modelButtonText(shownAgent(), items));
  const open = $derived(ui.menu?.kind === "model");
  let button = $state();

  // The saved models, read once and kept fresh, so the label matches the menu.
  $effect(() => watchModels());

  function toggle() {
    if (open) closeMenu();
    else openMenu({ kind: "model", anchor: button });
  }
</script>

{#if helper}
  <span class="model" class:open data-helper-model title="This sub-agent's model">
    <span class="label">{label}</span>
  </span>
{:else}
  <button bind:this={button} class="model" class:open type="button" aria-label={label ? `Model: ${label}` : "Model"} aria-haspopup="dialog" aria-expanded={open} onclick={toggle}>
    <span class="label">{label}</span>
    <Icon name="caret" />
  </button>
{/if}

<style>
  /* The one-line field's full inner height, centred as the field grows
     (owner, 2026-10-20). */
  .model {
    flex: none;
    align-self: center;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: calc(14 * var(--px));
    min-width: calc(163 * var(--px));
    max-width: calc(320 * var(--px));
    box-sizing: border-box;
    height: calc(56 * var(--px));
    padding: 0 calc(13 * var(--px)) 0 calc(14 * var(--px));
    font: var(--t-small);
    color: var(--ink);
  }
  /* Inside the field, whose own hairline shows too. */
  .model:focus-visible {
    outline-offset: calc(-6 * var(--px));
  }
  /* The caret turns over while the menu is open. */
  .model :global(svg) {
    transition: transform var(--motion) var(--ease-paper);
  }
  .model.open :global(svg) {
    transform: rotate(180deg);
  }
  .label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
