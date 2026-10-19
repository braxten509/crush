<script>
  // Fallback compaction (desktop-design-5-6 2.12; DLG-264 to DLG-266):
  // internal/ui/dialog/autocompact.go. One field, prompt "Tokens: ",
  // prefilled with the saved value or 400,000; commas are ignored.
  import { untrack } from "svelte";
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
  <Field bind:value={text} prompt="Tokens: " label="Fallback compaction tokens" {error} />
</Sheet>
