<script>
  // Sub-agent Instructions (desktop-design-5-6 2.12; DLG-272, DLG-273):
  // internal/ui/dialog/subagent_instructions.go. A multiline editor, 3-14
  // rows; Enter is a newline, Ctrl+S saves, Esc cancels. The file sits
  // beside the global crush.json and the engine reads it on every agent call.
  import { onMount } from "svelte";
  import Area from "../kit/Area.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { subagentInstructionsRead, subagentInstructionsWrite } from "../../lib/engine/commands.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "subagent_instructions";

  let text = $state("");
  let loaded = $state(false);

  const help = [
    { key: "ctrl+s", text: "save" },
    { key: "esc", text: "cancel" },
  ];

  async function save() {
    try {
      await subagentInstructionsWrite(text);
      notify("info", "Sub-agent instructions saved");
      closeDialog(ID);
    } catch (e) {
      notify("error", e?.message ?? String(e));
    }
  }

  function onKey(event, combo) {
    if (combo === "Ctrl+S") {
      save();
      return true;
    }
    return false;
  }

  onMount(async () => {
    try {
      text = await subagentInstructionsRead();
    } catch (e) {
      notify("error", e?.message ?? String(e));
    }
    loaded = true;
  });
</script>

<Sheet id={ID} title="Sub-agent Instructions" width="standard" {help} {onKey}>
  <p class="hint">Every chat's agent follows these when it starts sub-agents.</p>
  {#if loaded}
    <Area bind:value={text} minLines={3} maxLines={14} placeholder="Which models to use for which work, when to ask first…" label="Sub-agent instructions" />
  {/if}
</Sheet>

<style>
  .hint {
    margin: 0 0 calc(12 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
</style>
