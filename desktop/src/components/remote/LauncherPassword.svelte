<script>
  // Launcher password (desktop-design-5-6 2.27 REM-016, look 5.20): two
  // masked fields; saving runs `crush remote password --stdin` through
  // remote_password_set, which writes both on the child's standard input.
  // The fields are cleared right after the call. Texts come from the CLI.
  import { onDestroy } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { remotePasswordSet } from "../../lib/engine/commands.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "launcher_password";

  let first = $state("");
  let second = $state("");
  let error = $state("");
  let selected = $state("save");
  let busy = false;

  const help = [
    { key: "tab", text: "next" },
    { key: "enter", text: "save" },
    { key: "esc", text: "cancel" },
  ];

  function clear() {
    first = "";
    second = "";
  }

  async function save() {
    if (busy) return;
    error = "";
    if (first !== second) {
      error = "the two passwords don't match; nothing changed";
      clear();
      return;
    }
    busy = true;
    try {
      const answer = await remotePasswordSet(first, second);
      clear();
      if (answer?.ok) {
        notify("info", answer.message);
        closeDialog(ID);
      } else error = answer?.message ?? "the password could not be set";
    } catch (e) {
      clear();
      error = e?.message ?? String(e);
    } finally {
      busy = false;
    }
  }

  function onKey(event, combo) {
    if (combo === "Enter") {
      // A focused button presses itself; elsewhere Enter presses the chosen one.
      if (event.target?.tagName === "BUTTON") return false;
      if (selected === "cancel") closeDialog(ID);
      else save();
      return true;
    }
    return false;
  }

  onDestroy(clear);
</script>

<Sheet id={ID} title="Launcher password" width="small" {help} {onKey}>
  <label class="label" for="launcher-first">New password</label>
  <Field bind:value={first} masked label="New password" />
  <label class="label" for="launcher-second">Type it again</label>
  <Field bind:value={second} masked label="Type it again" {error} />
  <div class="buttons">
    <Buttons
      buttons={[
        { id: "save", label: "Save", key: "s" },
        { id: "cancel", label: "Cancel", key: "c" },
      ]}
      bind:selected
      onPress={(id) => (id === "save" ? save() : closeDialog(ID))}
    />
  </div>
</Sheet>

<style>
  .label {
    display: block;
    margin: calc(10 * var(--px)) 0 calc(6 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .buttons {
    margin-top: calc(18 * var(--px));
  }
</style>
