<script>
  // Memory Limit (desktop-design-5-6 2.12; DLG-267 to DLG-271):
  // internal/ui/dialog/memory_limit.go, with the numbers from GET /v1/memory.
  // Empty or 0 means no limit; otherwise at least 2 GB. The engine saves and
  // applies it (PUT /v1/memory/limit) and returns the notice to show.
  import { onMount } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { gigabytes, parseGigabytes } from "../../lib/settings/store.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "memory_limit";

  let text = $state("");
  let error = $state("");
  let status = $state(null);
  let busy = false;
  let loaded = false; // Enter waits for the saved limit
  let touched = false; // after a failed read, only a typed value is saved
  let shown = "";

  const help = [
    { key: "enter", text: "save" },
    { key: "esc", text: "cancel" },
  ];
  const usage = $derived.by(() => {
    let line = "Empty = no limit";
    if (status?.box_used_bytes) line += ` · now ${gigabytes(status.box_used_bytes)} GB`;
    if (status?.total_ram_bytes) line += ` of ${gigabytes(status.total_ram_bytes)} GB`;
    return line;
  });

  $effect(() => {
    void text;
    error = "";
    if (loaded && text !== shown) touched = true;
  });

  async function save() {
    if (busy || !loaded) return;
    if (!status && !touched && text === shown) return;
    const parsed = parseGigabytes(text);
    if (parsed.error) {
      error = parsed.error;
      return;
    }
    busy = true;
    try {
      const answer = await api.setMemoryLimit(parsed.value);
      if (answer?.applied === false && answer?.notice) notify("error", answer.notice);
      else if (answer?.notice) notify("info", answer.notice);
      closeDialog(ID);
    } catch (e) {
      notify("error", e?.message ?? String(e));
    } finally {
      busy = false;
    }
  }

  function onKey(event, combo) {
    if (combo === "Enter") {
      save();
      return true;
    }
    return false;
  }

  onMount(async () => {
    try {
      status = await api.memory();
      if (status?.limit_bytes) text = String(Number(gigabytes(status.limit_bytes)));
    } catch {
      // The field still works once typed in; the usage line shows less.
      status = null;
    }
    shown = text;
    loaded = true;
  });
</script>

<Sheet id={ID} title="Memory limit" width="small" {help} {onKey}>
  <p class="hint">Most memory all Crush windows and everything they start may use. At the limit, the biggest program inside is stopped.</p>
  <Field bind:value={text} prompt="GB: " label="Memory limit in GB" {error} />
  <p class="usage">{usage}</p>
  <div class="actions">
    <Buttons buttons={[{ id: "save", label: "Save" }, { id: "cancel", label: "Cancel" }]} selected="save" onPress={(id) => (id === "save" ? save() : closeDialog(ID))} />
  </div>
</Sheet>

<style>
  /* Save and Cancel to click, beside their keys (owner, 2026-10-20). */
  .actions {
    margin-top: calc(18 * var(--px));
  }

  p {
    margin: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .hint {
    margin-bottom: calc(12 * var(--px));
    font: var(--t-body-small);
  }
  .usage {
    margin-top: calc(8 * var(--px));
  }
</style>
