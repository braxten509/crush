<script>
  // Sub-agent Instructions (desktop-design-5-6 2.12; DLG-272, DLG-273):
  // internal/ui/dialog/subagent_instructions.go. A multiline editor, 3-14
  // rows; Enter is a newline, Ctrl+S saves, Esc cancels. The file sits
  // beside the global crush.json and the engine reads it on every agent call.
  import { onMount } from "svelte";
  import Area from "../kit/Area.svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { subagentInstructionsRead, subagentInstructionsWrite } from "../../lib/engine/commands.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "subagent_instructions";

  let text = $state("");
  let loaded = $state(false);
  let readOk = false; // never save over a file that could not be read

  const help = [
    { key: "ctrl+s", text: "save" },
    { key: "esc", text: "cancel" },
  ];

  async function save() {
    if (!readOk) return;
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
      readOk = true;
    } catch (e) {
      notify("error", e?.message ?? String(e));
    }
    loaded = true;
  });
</script>

<Sheet id={ID} title="Sub-agent instructions" width="standard" {help} {onKey}>
  <p class="hint">Every chat's agent follows these when it starts sub-agents.</p>
  {#if loaded}
    <Area bind:value={text} minLines={3} maxLines={14} placeholder="Which models to use for which work, when to ask first…" label="Sub-agent instructions" />
    <div class="actions">
      <Buttons buttons={[{ id: "save", label: "Save" }, { id: "cancel", label: "Cancel" }]} selected="save" onPress={(id) => (id === "save" ? save() : closeDialog(ID))} />
    </div>
  {/if}
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
