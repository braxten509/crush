<script>
  // A one-line field (desktop-design-5-6 2.1, DLG-013 to DLG-017; look 5.1):
  // an <input> with the terminal's editing keys on top of the browser's
  // (lib/kit/fieldkeys.js). The keys reach it only after the front sheet's
  // onKey, so a sheet that binds one of them gets it first ("Screen keys
  // win"); `reserved` names keys the field leaves alone even then
  // ("Ctrl+D"). Ctrl+V, Shift+Insert and a middle click paste; a pasted text
  // goes on one line, its line breaks and tabs turned into spaces, as the
  // terminal's field does (textinput.go:297-305; desktop-design-5-6 2.0 7).
  //   value (bindable), placeholder?, prompt? (drawn before the text, "> "),
  //   masked? (a password field; its words never show), error? (the line
  //   under the field), reserved?: string[] (combos as lib/keys.js writes them)
  //   icon? (an Icon name drawn at the left, "search" for a filter field),
  //   label? (its accessible name; the placeholder otherwise)
  import { comboOf } from "../../lib/keys.js";
  import { applyEdit, fieldEdit, fieldState, oneLine } from "../../lib/kit/fieldkeys.js";
  import { setPart } from "../../lib/kit/sheetkeys.js";
  import Icon from "../Icon.svelte";

  let {
    value = $bindable(""),
    placeholder = "",
    prompt = "",
    masked = false,
    error = "",
    reserved = [],
    icon = null,
    label = "",
  } = $props();

  let box = $state(null);
  let input = $state(null);

  const isReserved = (combo) => reserved.some((key) => String(key).toLowerCase() === combo.toLowerCase());

  function onkeydown(event) {
    if (event.defaultPrevented || event.isComposing) return;
    const combo = comboOf(event);
    if (isReserved(combo)) return;
    const edit = fieldEdit(combo, fieldState(input.value, input.selectionStart, input.selectionEnd, input.selectionDirection), { masked });
    if (!edit) return;
    event.preventDefault();
    applyEdit(input, edit);
  }

  function onpaste(event) {
    const text = event.clipboardData?.getData("text/plain");
    if (typeof text !== "string") return;
    const line = oneLine(text);
    if (line === text) return;
    event.preventDefault();
    const at = fieldState(input.value, input.selectionStart, input.selectionEnd);
    const chars = [...at.chars.slice(0, at.start), ...line, ...at.chars.slice(at.end)];
    const caret = at.start + [...line].length;
    applyEdit(input, { chars, start: caret, end: caret });
  }

  export function focus() {
    input?.focus({ preventScroll: true });
  }

  $effect(() => {
    if (!box) return;
    return setPart(box, {
      kind: "field",
      report: () => ({ kind: "field", masked, prompt, placeholder, error, focused: document.activeElement === input }),
      heavy: () => ({ kind: "field", rect: rectOf(box), input: rectOf(input), error_line: rectOf(box.nextElementSibling?.matches?.("[data-field-error]") ? box.nextElementSibling : null) }),
    });
  });

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }
</script>

<!-- A click anywhere on the box puts the caret in the field. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="field" class:invalid={Boolean(error)} class:has-icon={Boolean(icon)} data-kit-field bind:this={box} onclick={() => input?.focus()}>
  {#if icon}<Icon name={icon} />{/if}
  {#if prompt}<span class="prompt" aria-hidden="true">{prompt}</span>{/if}
  <input
    bind:this={input}
    bind:value
    type={masked ? "password" : "text"}
    {placeholder}
    aria-label={label || placeholder || undefined}
    aria-invalid={error ? "true" : undefined}
    autocomplete="off"
    spellcheck="false"
    {onkeydown}
    {onpaste}
  />
</div>
{#if error}
  <p class="error" data-field-error role="alert">{error}</p>
{/if}

<style>
  /* The chat list's search box (ChatList.svelte:305-345), look 5.1. */
  .field {
    display: flex;
    flex: none;
    align-items: center;
    box-sizing: border-box;
    height: calc(50 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(11 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    color: var(--ink);
    cursor: text;
  }
  .field:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .field.invalid {
    border-color: var(--error-ink);
  }
  .field :global(.icon) {
    flex: none;
    margin-right: calc(12 * var(--px));
  }
  .prompt {
    flex: none;
    font: var(--t-field);
    color: var(--slate);
    white-space: pre;
  }
  input {
    flex: 1;
    min-width: 0;
    height: calc(26 * var(--px));
    padding: 0;
    border: 0;
    outline: 0;
    background: none;
    font: var(--t-field);
    color: var(--ink);
    appearance: none;
  }
  input::placeholder {
    color: var(--slate);
    opacity: 1;
  }
  input::selection {
    background: var(--selection);
  }
  .error {
    margin-top: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--error-ink);
  }
</style>
