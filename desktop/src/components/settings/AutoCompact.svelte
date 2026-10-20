<script>
  // Fallback compaction (desktop-design-5-6 2.12; DLG-264 to DLG-266):
  // internal/ui/dialog/autocompact.go. One field, prompt "Tokens: ",
  // prefilled with the saved value or 400,000; commas are ignored.
  import { untrack } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { DEFAULT_AUTOCOMPACT, options, parseTokens, saveAutoCompact } from "../../lib/settings/store.svelte.js";

  const ID = "autocompact";
  const saved = untrack(() => Number(options().auto_compact_token_limit) || DEFAULT_AUTOCOMPACT);

  let text = $state(saved.toLocaleString("en-US"));
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
    const parsed = parseTokens(text);
    if (parsed.error) {
      error = parsed.error;
      return;
    }
    if (await saveAutoCompact(parsed.value)) closeDialog(ID);
  }

  function onKey(event, combo) {
    if (combo === "Enter") {
      save();
      return true;
    }
    return false;
  }
</script>

<Sheet id={ID} title="Fallback compaction" width="small" {help} {onKey}>
  <p class="hint">When a chat reaches this many tokens, Crush sums it up so the chat can go on. Agents that shorten their own chats are not affected.</p>
  <Field bind:value={text} prompt="Tokens: " label="Fallback compaction tokens" {error} />
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
