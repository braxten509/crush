<script>
  // A row of sheet buttons (desktop-design-5-6 2.1, DLG-020; look 5.1): real
  // <button>s, the whole button a click area, each label with its shortcut
  // letter underlined (the terminal's buttons, internal/ui/common/button.go).
  // Looks: rest, hover (--control-hover), the keyboard-selected one (`selected`:
  // --band with a 1px --ink outline), focus (the app's vermilion hairline) and
  // disabled. A click calls onPress(id) and leaves `selected` as it was, so a
  // sheet may make a first click select (the quit sheet, 2.24). While no
  // field has the focus, ←/→ move `selected`, Enter or Space press the focused
  // button or else the selected one, and a shortcut letter presses its button;
  // the front sheet's onKey hears all of these first.
  //   buttons: [{id, label, key?, destructive?, disabled?}], selected
  //   (bindable id), onPress(id), stack? (one under another)
  import { setPart, typingIn } from "../../lib/kit/sheetkeys.js";

  let { buttons = [], selected = $bindable(null), onPress = () => {}, stack = false } = $props();

  let row = $state(null);

  // The label cut around its shortcut letter's first appearance.
  function parts(button) {
    const label = String(button.label ?? "");
    const key = button.key ? String(button.key) : "";
    const at = key.length === 1 ? label.toLowerCase().indexOf(key.toLowerCase()) : -1;
    if (at < 0) return { before: label, letter: "", after: "" };
    return { before: label.slice(0, at), letter: label.slice(at, at + 1), after: label.slice(at + 1) };
  }

  const usable = $derived(buttons.filter((button) => !button.disabled));

  function press(id) {
    const button = buttons.find((b) => b.id === id);
    if (!button || button.disabled) return false;
    onPress(id);
    return true;
  }

  function step(delta) {
    if (usable.length === 0) return false;
    const at = usable.findIndex((b) => b.id === selected);
    const next = at < 0 ? (delta > 0 ? 0 : usable.length - 1) : (at + delta + usable.length) % usable.length;
    selected = usable[next].id;
    // Focus inside the row follows the selection, so Enter presses the
    // button that looks selected (2.1, 5.1).
    if (row?.contains(document.activeElement)) [...row.querySelectorAll("[data-button]")].find((el) => el.dataset.button === String(selected))?.focus();
    return true;
  }

  function keys(event, combo) {
    if (typingIn()) return false;
    const focused = row?.contains(document.activeElement) ? document.activeElement.dataset.button : null;
    if (combo === "ArrowLeft") return step(-1);
    if (combo === "ArrowRight") return step(1);
    if (combo === "Enter" || combo === "Space") return press(focused ?? selected);
    if (event.ctrlKey || event.altKey || event.metaKey || event.key.length !== 1) return false;
    const button = usable.find((b) => b.key && String(b.key).toLowerCase() === event.key.toLowerCase());
    return button ? press(button.id) : false;
  }

  // Keyboard focus (Tab) moves the selection with it; a click's focus does not.
  function onfocus(event, id) {
    if (event.currentTarget.matches(":focus-visible")) selected = id;
  }

  $effect(() => {
    if (!row) return;
    return setPart(row, {
      kind: "buttons",
      keys,
      report: () => ({
        kind: "buttons",
        selected,
        buttons: buttons.map((b) => ({ id: b.id, label: b.label, key: b.key ?? null, destructive: Boolean(b.destructive), disabled: Boolean(b.disabled) })),
        focused: row.contains(document.activeElement) ? (document.activeElement.dataset.button ?? null) : null,
      }),
      heavy: () => ({
        kind: "buttons",
        row: rectOf(row),
        buttons: [...row.querySelectorAll("[data-button]")].map((el) => {
          const style = getComputedStyle(el);
          const letter = el.querySelector("u");
          return {
            id: el.dataset.button,
            rect: rectOf(el),
            background: style.backgroundColor,
            border: style.borderTopColor,
            outline: style.outlineStyle === "none" ? null : style.outlineColor,
            hovered: el.matches(":hover"),
            letter: letter ? { text: letter.textContent, rect: rectOf(letter), line: getComputedStyle(letter).textDecorationLine } : null,
          };
        }),
      }),
    });
  });

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }
</script>

<div class="buttons" class:stack role="group" data-kit-buttons bind:this={row}>
  {#each buttons as button (button.id)}
    {@const label = parts(button)}
    <button
      type="button"
      class="button"
      class:selected={button.id === selected}
      class:destructive={Boolean(button.destructive)}
      disabled={Boolean(button.disabled)}
      data-button={button.id}
      aria-keyshortcuts={button.key || undefined}
      onclick={() => press(button.id)}
      onfocus={(event) => onfocus(event, button.id)}
    >
      <span class="label">{label.before}{#if label.letter}<u>{label.letter}</u>{/if}{label.after}</span>
    </button>
  {/each}
</div>

<style>
  .buttons {
    display: flex;
    flex: none;
    flex-wrap: wrap;
    gap: calc(12 * var(--px));
  }
  .buttons.stack {
    flex-direction: column;
    align-items: stretch;
  }
  .button {
    display: flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    height: calc(44 * var(--px));
    min-width: calc(112 * var(--px));
    padding: 0 calc(20 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--control-rest);
    color: var(--ink);
    text-align: center;
  }
  .label {
    font: var(--t-button);
    white-space: nowrap;
  }
  u {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .button:hover:not(:disabled) {
    background: var(--control-hover);
  }
  /* The keyboard-selected button: a band fill inside an ink hairline. */
  .button.selected:not(:disabled) {
    background: var(--band);
    border-color: var(--ink);
  }
  /* Destructive words stay ink: the word carries the meaning (look 5.1). */
  .button.destructive {
    color: var(--ink);
  }
  .button:disabled {
    background: var(--control-disabled);
    border-color: var(--rule-faint);
    color: var(--mark);
    cursor: default;
  }
</style>
