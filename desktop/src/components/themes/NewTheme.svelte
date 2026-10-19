<script>
  // New Theme (desktop-design-5-6 2.14, 3.3, look 5.14; THEME-018 to
  // THEME-020): internal/ui/dialog/theme/new.go. One field, based on the
  // desktop's theme (scholars-edition when none is chosen). The engine checks
  // the name with the terminal's own rules and texts; the empty and pattern
  // checks run here first so the error shows at once. On success the new
  // theme becomes the desktop's and the editor opens on it.
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog, openDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { chooseTheme, chosenTheme } from "../../lib/themes/current.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "theme_new";
  const NAME = /^[a-z0-9]+([-_][a-z0-9]+)*$/;

  let { onCreated = null } = $props();

  let name = $state("");
  let error = $state("");
  let busy = false;

  const help = [
    { key: "enter", text: "create" },
    { key: "esc", text: "cancel" },
  ];

  async function create() {
    if (busy) return;
    const wanted = name.trim();
    if (wanted === "") {
      error = "theme name cannot be empty";
      return;
    }
    if (!NAME.test(wanted)) {
      error = "use lowercase letters, numbers, hyphens, and underscores only";
      return;
    }
    busy = true;
    try {
      await api.createTheme({ name: wanted, base: chosenTheme() });
      await chooseTheme(wanted);
      notify("info", `Created new theme: ${wanted}`);
      closeDialog(ID);
      onCreated?.(wanted);
      openDialog("theme_editor", { name: wanted, onSaved: onCreated ? () => onCreated(wanted) : null }, { grace: true });
    } catch (e) {
      error = e?.message ?? String(e);
    } finally {
      busy = false;
    }
  }

  function onKey(event, combo) {
    if (combo === "Enter") {
      create();
      return true;
    }
    return false;
  }

  $effect(() => {
    void name;
    error = "";
  });
</script>

<Sheet id={ID} title="New Theme" width="small" {help} {onKey}>
  <Field bind:value={name} placeholder="my-theme" label="Theme name" {error} />
  {#if !error}<p class="reserve" aria-hidden="true"></p>{/if}
</Sheet>

<style>
  .reserve {
    height: calc(24 * var(--px));
    margin: 0;
  }
</style>
