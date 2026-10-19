<script>
  // The model button at the field's right (SIDE-006's label; 4.9): the
  // agent's model and effort as the footer items allow, "Unknown" until the
  // agent has a model. A click opens the models sheet, which phase 5
  // registers (until then nothing opens).
  import Icon from "../Icon.svelte";
  import { modelButtonText } from "../../lib/composer/modes.js";
  import { openDialog } from "../../lib/dialogs.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";

  const label = $derived(modelButtonText(ws.agent, ws.config?.options?.tui?.composer_footer));
</script>

<button class="model" type="button" aria-label={label ? `Model: ${label}` : "Model"} onclick={() => openDialog("models")}>
  <span class="label">{label}</span>
  <Icon name="caret" />
</button>

<style>
  /* The one-line field's full inner height, kept at the bottom as the field grows. */
  .model {
    flex: none;
    align-self: flex-end;
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
  .label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
