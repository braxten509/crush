<script>
  // The theme editor (desktop-design-5-6 2.14, 3.2, 3.3, look 5.14;
  // THEME-021 to THEME-082): internal/ui/dialog/theme/editor.go, docked at
  // the right edge with no veil. One row per palette role in the terminal's
  // order, filled with the theme's values; the selected row's value is the
  // field. ↓/Ctrl+N/Tab and ↑/Ctrl+P/Shift+Tab move and wrap. Typing previews
  // through POST /v1/themes/resolve, newest answer first; a value the engine
  // refuses keeps the old color, shows the swatch in the error color and
  // blocks saving; an empty value keeps the role's current color. Enter or
  // Ctrl+S saves the palette with its base (PUT /v1/themes/{name}); Esc
  // leaves without saving and reverts the preview.
  //   name, onSaved?()
  import { onMount, tick, untrack } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { chosenTheme, endPreview, loadCurrent, previewView } from "../../lib/themes/current.svelte.js";
  import { ROLES } from "../../lib/themes/tokens.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "theme_editor";

  let { name, onSaved = null } = $props();
  const themeName = untrack(() => name);

  let base = $state("");
  let start = $state({}); // the values the editor opened with
  let values = $state({}); // role → the typed value
  let swatches = $state({}); // role → #rrggbb from the last good resolve
  let invalid = $state(new Set()); // roles the engine refused
  let selected = $state(0);
  let loaded = $state(false);
  let saved = false;
  let seq = 0;
  let timer = null;
  let listBox = $state(null);
  let button = $state("save");

  const blocked = $derived(invalid.size > 0);
  const help = [
    { key: "↓", text: "next" },
    { key: "↑", text: "previous" },
    { key: "enter", text: "save" },
    { key: "esc", text: "cancel" },
  ];

  // An empty value keeps the role's current color (editor.go:294-298).
  const paletteOut = () => Object.fromEntries(ROLES.map((role) => [role, String(values[role] ?? "").trim() || start[role] || ""]).filter(([, v]) => v !== ""));

  // The roles a 422 "invalid palette colors: …" names; the edited row when it names none.
  function refused(message, role) {
    const named = ROLES.filter((r) => new RegExp(`\\b${r}\\b`).test(message));
    return new Set(named.length > 0 ? named : [role]);
  }

  async function resolve(role) {
    const mine = ++seq;
    try {
      const view = await api.resolveTheme({ base, palette: paletteOut() });
      if (mine !== seq) return;
      invalid = new Set();
      swatches = { ...(view?.resolved ?? {}) };
      previewView({ ...view, name: themeName });
    } catch (error) {
      if (mine !== seq) return;
      invalid = refused(String(error?.message ?? ""), role);
    }
  }

  function edited(role) {
    clearTimeout(timer);
    timer = setTimeout(() => resolve(role), 120);
  }

  async function move(delta) {
    clearTimeout(timer);
    if (!blocked) await resolve(ROLES[selected]);
    selected = (selected + delta + ROLES.length) % ROLES.length;
    await tick();
    listBox?.querySelector(`[data-role="${ROLES[selected]}"]`)?.scrollIntoView({ block: "nearest" });
    focusField();
  }

  async function focusField() {
    await tick();
    listBox?.querySelector(".row.selected input")?.focus();
  }

  async function save() {
    clearTimeout(timer);
    await resolve(ROLES[selected]);
    if (blocked) return;
    try {
      await api.saveTheme(themeName, { base, palette: paletteOut() });
      saved = true;
      if (themeName === chosenTheme()) await loadCurrent({ force: true });
      else endPreview();
      notify("info", "Theme saved");
      closeDialog(ID);
      onSaved?.();
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowDown":
      case "Ctrl+N":
      case "Tab":
        move(1);
        return true;
      case "ArrowUp":
      case "Ctrl+P":
      case "Shift+Tab":
        move(-1);
        return true;
      case "Enter":
      case "Ctrl+S":
        save();
        return true;
      default:
        return false;
    }
  }

  // Typing in the selected row previews it.
  let lastValue = {};
  $effect(() => {
    if (!loaded) return;
    const role = ROLES[selected];
    const value = values[role];
    if (!(role in lastValue)) lastValue[role] = value;
    if (value === lastValue[role]) return;
    lastValue[role] = value;
    untrack(() => edited(role));
  });

  onMount(() => {
    (async () => {
      try {
        const view = await api.theme(themeName);
        base = view?.base || view?.root || "";
        start = { ...(view?.palette ?? {}) };
        values = { ...start };
        lastValue = { ...start };
        swatches = { ...(view?.resolved ?? {}) };
        loaded = true;
        focusField();
      } catch (error) {
        notify("error", error?.message ?? String(error));
        closeDialog(ID);
      }
    })();
    return () => {
      clearTimeout(timer);
      seq += 1;
      if (!saved) endPreview();
    };
  });
</script>

<Sheet id={ID} title={`Edit Theme: ${themeName}`} width="standard" docked {help} {onKey}>
  {#if loaded}
    <div class="rows" bind:this={listBox} role="list">
      {#each ROLES as role, i (role)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
        <div class="row" class:selected={i === selected} data-role={role} role="listitem" onclick={() => i !== selected && move(i - selected)}>
          <span class="role">{role}</span>
          <span class="swatch" class:bad={invalid.has(role)} style:background={invalid.has(role) ? "var(--error-ink)" : swatches[role] || "transparent"}></span>
          {#if i === selected}
            <span class="value-field"><Field bind:value={values[role]} label={`${role} color`} placeholder={start[role] ?? ""} /></span>
          {:else}
            <span class="value">{values[role] ?? ""}</span>
          {/if}
        </div>
      {/each}
    </div>
    <Buttons
      buttons={[
        { id: "save", label: "Save", key: "s", disabled: blocked },
        { id: "cancel", label: "Cancel", key: "c" },
      ]}
      bind:selected={button}
      onPress={(id) => (id === "save" ? save() : closeDialog(ID))}
    />
  {/if}
</Sheet>

<style>
  .rows {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    margin: 0 calc(-26 * var(--px)) calc(16 * var(--px));
  }
  .row {
    position: relative;
    display: flex;
    align-items: center;
    height: calc(36 * var(--px));
    padding: 0 calc(26 * var(--px));
    cursor: pointer;
  }
  .row:hover {
    background: var(--control-hover);
  }
  .row.selected {
    background: var(--band);
  }
  .role {
    width: calc(234 * var(--px));
    flex: none;
    font: var(--t-agent-mono);
    color: var(--ink);
  }
  .swatch {
    flex: none;
    box-sizing: border-box;
    width: calc(20 * var(--px));
    height: calc(20 * var(--px));
    margin-right: calc(14 * var(--px));
    border: 1px solid var(--rule-control);
  }
  .value {
    font: var(--t-agent-mono);
    color: var(--slate);
  }
  .value-field {
    flex: 1;
    min-width: 0;
  }
  .value-field :global(.field) {
    height: calc(32 * var(--px));
  }
  .value-field :global(input) {
    font: var(--t-agent-mono);
  }
</style>
