<script>
  // The API key sheet (desktop-design-5-6 2.4, look 5.6; DLG-046 to DLG-053):
  // a port of internal/ui/dialog/auth/api_key_input.go. The key is typed in a
  // plain field (as the terminal), verified through G-5's verify-key route
  // with a spinner of at least 750 ms, during which every key is ignored;
  // once validated, Enter/Ctrl+Y or Esc saves it to the global config, once.
  // Both calls go through apiSecret, so the key never reaches the generic
  // api command, a store, the probe, a notice or a log; the field is emptied
  // when the sheet closes. Esc before validation cancels.
  //   providerId, providerName?, onboarding?, onDone(result)
  import { onDestroy } from "svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { apiSecret } from "../../lib/engine/commands.js";
  import { modelData } from "../../lib/models/store.svelte.js";
  import { MIN_VERIFY_MS } from "../../lib/signin/flows.js";
  import { conn } from "../../lib/stores/connection.svelte.js";

  const ID = "api_key_input";

  let props = $props();

  let key = $state("");
  let state = $state("initial"); // "initial" | "verifying" | "verified" | "error"
  let saveError = $state("");
  let saving = false;
  let saved = false;

  const name = $derived(props.providerName || modelData.catalog.find((p) => p.id === props.providerId)?.name || props.providerId);
  const title = $derived(
    state === "verifying" ? `Verifying your ${name} Key...` : state === "verified" ? `${name} Key validated.` : state === "error" ? `Invalid ${name} Key. Try again?` : `Enter your ${name} Key.`,
  );
  const help = [
    { key: "enter", text: "submit" },
    { key: "esc", text: "exit" },
  ];
  const configPath = $derived(conn.workspace?.config_path ?? "");

  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

  async function verify() {
    state = "verifying";
    document.querySelector(`[data-sheet="${ID}"] input`)?.blur();
    const started = Date.now();
    let ok = false;
    try {
      const answer = await apiSecret("auth/verify-key", { provider_id: props.providerId, api_key: key });
      ok = answer?.ok === true;
    } catch {
      ok = false;
    }
    await sleep(Math.max(0, MIN_VERIFY_MS - (Date.now() - started)));
    state = ok ? "verified" : "error";
    if (!ok) queueMicrotask(() => document.querySelector(`[data-sheet="${ID}"] input`)?.focus());
  }

  async function save() {
    if (saving || saved) return;
    saving = true;
    saveError = "";
    try {
      await apiSecret("config/provider-key", { scope: 0, provider_id: props.providerId, kind: "string", api_key: key });
      saved = true;
      key = "";
      closeDialog(ID);
      props.onDone?.("done");
    } catch (error) {
      saveError = `failed to save API key: ${error?.message ?? error}`;
    } finally {
      saving = false;
    }
  }

  function cancel() {
    key = "";
    closeDialog(ID);
    props.onDone?.("cancelled");
  }

  function onKey(event, combo) {
    if (state === "verifying") return true; // DLG-048: every key waits
    const enter = combo === "Enter" || combo === "Ctrl+Y";
    const esc = combo === "Escape" || combo === "Alt+Escape";
    if (state === "verified" && (enter || esc)) {
      save();
      return true;
    }
    if (enter) {
      if (key.trim() !== "") verify();
      return true;
    }
    if (esc) {
      cancel();
      return true;
    }
    return false;
  }

  onDestroy(() => {
    key = "";
  });
</script>

<Sheet id={ID} {title} width="standard" {help} {onKey} onboarding={props.onboarding === true}>
  <div class="body" class:busy={state === "verifying"}>
    <Field bind:value={key} prompt={state === "verifying" ? "… " : "> "} placeholder="Enter your API key..." label="API key" error={saveError} />
    <p class="note">This will be written in your global configuration:</p>
    <p class="path">{configPath}</p>
  </div>
</Sheet>

<style>
  .body.busy :global(input) {
    pointer-events: none;
    color: var(--mark);
  }
  .note {
    margin: calc(12 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .path {
    margin: calc(2 * var(--px)) 0 0;
    font: var(--t-agent-mono);
    color: var(--ink);
    overflow-wrap: anywhere;
  }
</style>
