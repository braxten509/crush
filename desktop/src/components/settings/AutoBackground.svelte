<script>
  // The time before a command is moved to the background
  // (internal/ui/dialog/autobackground.go; options.auto_background_seconds).
  // One field, prompt "Seconds: ", prefilled with the saved value or 120.
  import { untrack } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { autoBackgroundSeconds, options, parseSeconds, saveAutoBackground } from "../../lib/settings/store.svelte.js";

  const ID = "auto_background";

  let text = $state(String(untrack(() => autoBackgroundSeconds(options()))));
  let error = $state("");

  const help = [
    { key: "enter", text: "save" },
    { key: "esc", text: "cancel" },
  ];

  $effect(() => {
    void text;
    error = "";
  });

  async function save() {
    const parsed = parseSeconds(text);
    if (parsed.error) {
      error = parsed.error;
      return;
    }
    if (await saveAutoBackground(parsed.value)) closeDialog(ID);
  }

  function onKey(event, combo) {
    if (combo === "Enter") {
      save();
      return true;
    }
    return false;
  }
</script>

<Sheet id={ID} title="Background commands after" width="small" {help} {onKey}>
  <p class="hint">A command an agent runs that is still going after this many seconds is moved to the background, so the agent can carry on while it finishes.</p>
  <Field bind:value={text} prompt="Seconds: " label="Seconds before a command is moved to the background" {error} />
  <div class="actions">
    <Buttons buttons={[{ id: "save", label: "Save" }, { id: "cancel", label: "Cancel" }]} selected="save" onPress={(id) => (id === "save" ? save() : closeDialog(ID))} />
  </div>
</Sheet>

<style>
  .hint {
    margin: 0 0 calc(12 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
  }
  /* Save and Cancel to click, beside their keys (owner, 2026-10-20). */
  .actions {
    margin-top: calc(18 * var(--px));
  }
</style>
