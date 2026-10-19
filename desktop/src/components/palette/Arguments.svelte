<script>
  // The arguments sheet (desktop-design-5-6 2.2 "Arguments sheet", look 5.3;
  // DLG-258 to DLG-263): a port of internal/ui/dialog/arguments.go. One
  // field per argument, labelled in Title Case with "*" when required; ↓/Tab
  // go to the next field, ↑/Shift+Tab to the one before; Enter moves on and
  // submits on the last. A missing required value shows "Required argument
  // '<Title>' is missing." on its field and focuses it. While an MCP prompt
  // is generated the footer says so and Esc cancels.
  //   title, args: [{id, title, description, required}], onSubmit(values)
  import { onDestroy, tick, untrack } from "svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { cancelPrompt, palette } from "../../lib/palette/store.svelte.js";

  const ID = "arguments";

  let { title = "Arguments", args = [], onSubmit = () => {} } = $props();

  let values = $state(untrack(() => Object.fromEntries(args.map((a) => [a.id, ""]))));
  let errors = $state({});
  let focused = $state(0);

  const help = $derived(
    palette.generating
      ? [{ key: "esc", text: "cancel" }]
      : [
          { key: "tab", text: "next" },
          { key: "enter", text: "confirm" },
          { key: "esc", text: "cancel" },
        ],
  );

  async function focusField(at) {
    focused = Math.max(0, Math.min(at, args.length - 1));
    await tick();
    document.querySelectorAll(`[data-sheet="${ID}"] input`)[focused]?.focus();
  }

  // The sheet closing any other way also cancels its fetch.
  onDestroy(() => {
    if (palette.generating) cancelPrompt();
  });

  function submit() {
    errors = {};
    for (const [at, arg] of args.entries()) {
      if (arg.required && String(values[arg.id] ?? "").trim() === "") {
        errors = { [arg.id]: `Required argument '${arg.title}' is missing.` };
        focusField(at);
        return;
      }
    }
    onSubmit({ ...values });
  }

  function onKey(event, combo) {
    if (palette.generating) {
      if (combo === "Escape" || combo === "Alt+Escape") {
        cancelPrompt();
        closeDialog(ID);
      }
      return true;
    }
    const at = [...document.querySelectorAll(`[data-sheet="${ID}"] input`)].indexOf(document.activeElement);
    if (at >= 0) focused = at;
    switch (combo) {
      case "ArrowDown":
      case "Tab":
        focusField(focused + 1);
        return true;
      case "ArrowUp":
      case "Shift+Tab":
        focusField(focused - 1);
        return true;
      case "Enter":
        if (focused >= args.length - 1) submit();
        else focusField(focused + 1);
        return true;
      default:
        return false;
    }
  }
</script>

<Sheet id={ID} {title} width="small" {help} {onKey}>
  <div class="fields">
    {#each args as arg (arg.id)}
      <label class="arg">
        <span class="label">{arg.title}{arg.required ? " *" : ""}</span>
        <Field bind:value={values[arg.id]} prompt="> " placeholder={arg.description ?? ""} label={arg.title} error={errors[arg.id] ?? ""} />
      </label>
    {/each}
  </div>
  {#if palette.generating}
    <p class="generating"><Icon name="working" size={16} /> Generating Prompt...</p>
  {/if}
</Sheet>

<style>
  .fields {
    display: flex;
    flex-direction: column;
    gap: calc(16 * var(--px));
    overflow-y: auto;
    min-height: 0;
  }
  .arg {
    display: flex;
    flex-direction: column;
    gap: calc(6 * var(--px));
  }
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .generating {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    margin: calc(12 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
</style>
