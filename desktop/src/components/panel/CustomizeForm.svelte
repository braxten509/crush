<script>
  // Customize Composer (SIDE-016 to SIDE-024): what the meters, the strip,
  // their detail and the model button show, in the slot (the typing box's
  // place), as the terminal's inline multi-choice form (composer_footer.go:
  // 142-181): the eight rows with their boxes, Space toggles, 1-8 toggle a row,
  // Enter saves options.tui.composer_footer globally ("Composer updated";
  // nothing picked saves "none"), Esc cancels. Its keys take the slot scope
  // while focus is here (1.8); the help sheet lists them (props.help).
  // Opened by the composer.customize action (lib/plugins/panel.js).
  import { onMount } from "svelte";
  import { api, SCOPE } from "../../lib/engine/api.js";
  import { FOOTER_CHOICES, footerItems } from "../../lib/footer.js";
  import { bindKey, comboOf } from "../../lib/keys.js";
  import { focusField } from "../../lib/stores/composer.svelte.js";
  import { closeSlot, notify, setFocus, ui } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { CUSTOMIZE, customizeClick, customizeKey, customizeSave, startCustomize } from "./customize.js";
  import { customizing as form } from "./panel.svelte.js";

  const KEYS = ["ArrowUp", "k", "ArrowDown", "j", "Space", "Enter", "Escape", "Alt+Escape", "1", "2", "3", "4", "5", "6", "7", "8"];

  // The saved choices when the form opened (a later config change does not
  // undo the user's toggles).
  Object.assign(form, startCustomize(footerItems(ws.config)));
  let root;

  function ours() {
    return ui.slot?.kind === "customize";
  }

  function close() {
    closeSlot("customize");
    setFocus("composer");
    focusField();
  }

  async function save() {
    const items = customizeSave(form);
    close();
    try {
      await api.setConfig(SCOPE.global, "options.tui.composer_footer", items);
      notify("info", "Composer updated");
    } catch (error) {
      notify("error", error?.message ? String(error.message) : String(error));
    }
  }

  function apply(answer) {
    form.cursor = answer.state.cursor;
    form.picked = answer.state.picked;
    if (answer.done === "save") save();
    else if (answer.done === "cancel") close();
    return answer.handled;
  }

  function key(event) {
    if (!ours()) return false;
    return apply(customizeKey(form, comboOf(event)));
  }

  function click(index) {
    root?.focus({ preventScroll: true });
    const next = customizeClick(form, index);
    form.cursor = next.cursor;
    form.picked = next.picked;
  }

  onMount(() => {
    const unbind = KEYS.map((combo) => bindKey("slot", combo, key));
    root?.focus({ preventScroll: true });
    setFocus("slot");
    return () => unbind.forEach((off) => off());
  });
</script>

<!-- The rows' list holds focus (a multi-select list); the cursor's row is its aria-activedescendant. -->
<div class="customize" role="group" aria-labelledby="customize-label customize-text" data-customize onfocusin={() => setFocus("slot")}>
  <p class="title">
    <span class="label" id="customize-label">{CUSTOMIZE.label}</span>
    <span class="text" id="customize-text">{CUSTOMIZE.text}</span>
  </p>
  <p class="description" id="customize-description">{CUSTOMIZE.description}</p>
  <ul
    class="rows"
    role="listbox"
    aria-multiselectable="true"
    aria-labelledby="customize-text"
    aria-describedby="customize-description"
    aria-activedescendant="customize-row-{form.cursor}"
    tabindex="0"
    bind:this={root}
  >
    {#each FOOTER_CHOICES as choice, i (choice.id)}
      {@const on = form.picked.includes(choice.id)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <li
        class="row"
        class:cursor={form.cursor === i}
        id="customize-row-{i}"
        role="option"
        aria-selected={on}
        data-choice={choice.id}
        onclick={() => click(i)}
      >
        <span class="box" class:on aria-hidden="true">
          {#if on}<svg viewBox="0 0 12 12" focusable="false"><path d="M2.2 6.3 4.9 9 9.8 3.2" fill="none" stroke="currentColor" stroke-width="1.6" /></svg>{/if}
        </span>
        <span class="number" aria-hidden="true">{i + 1}</span>
        <span class="choice">{choice.label}</span>
      </li>
    {/each}
  </ul>
  <div class="foot">
    <p class="hint">space toggle · 1-8 quick select · enter save · esc cancel</p>
    <button type="button" class="button" onclick={close}>Cancel</button>
    <button type="button" class="button primary" onclick={save}>Save</button>
  </div>
</div>

<style>
  .customize {
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
    overflow-y: auto;
    padding: calc(14 * var(--px)) calc(16 * var(--px)) calc(12 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .title {
    display: flex;
    align-items: baseline;
    gap: calc(12 * var(--px));
  }
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .text {
    font: var(--t-body-small);
  }
  .description {
    margin-top: calc(4 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .rows {
    margin-top: calc(10 * var(--px));
  }
  /* The cursor's row has a bar at its left. */
  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    min-height: calc(30 * var(--px));
    padding-left: calc(12 * var(--px));
    font: var(--t-small);
    cursor: pointer;
  }
  .row.cursor::before {
    content: "";
    position: absolute;
    left: 0;
    top: calc(3 * var(--px));
    bottom: calc(3 * var(--px));
    width: calc(2 * var(--px));
    background: var(--ink);
  }
  .box {
    flex: none;
    display: grid;
    place-items: center;
    box-sizing: border-box;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
  }
  .box.on {
    border-color: var(--ink);
    color: var(--ink);
  }
  .box svg {
    width: calc(12 * var(--px));
    height: calc(12 * var(--px));
  }
  .number {
    flex: none;
    min-width: calc(10 * var(--px));
    color: var(--slate);
    font-variant-numeric: tabular-nums;
  }
  .choice {
    min-width: 0;
  }
  .foot {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    margin-top: calc(10 * var(--px));
  }
  .hint {
    flex: 1;
    min-width: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .button {
    flex: none;
    box-sizing: border-box;
    height: calc(44 * var(--px));
    padding: 0 calc(18 * var(--px));
    border: 1px solid var(--rule-control);
    font: var(--t-body-small);
    color: var(--ink);
  }
  .button.primary {
    border-color: var(--ink);
    background: var(--band);
  }
</style>
