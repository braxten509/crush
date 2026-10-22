<script>
  // The model menu (owner, 2026-10-19: picker "B"), opened by the model
  // button and drawn by OverlayHost while ui.menu is {kind: "model", anchor}.
  // It unrolls up from the button: the agents and providers on the left, the
  // chosen one's models on the right with a search line over them, and the
  // coder model's effort levels at the bottom. A click on a model chooses it
  // (lib/models/choose.js) and keeps the menu open, so its effort level can be
  // picked next; a click on a level saves it. Esc, a click outside or the
  // button again close it. While it is open the key router asks only the
  // "menu" scope, so no other shortcut fires.
  // The full sheet (Large/Small Task, sign-in edits) stays on Ctrl+P.
  import { onMount, untrack } from "svelte";
  import Icon from "../Icon.svelte";
  import { bindKey } from "../../lib/keys.js";
  import { chooseModel } from "../../lib/models/choose.js";
  import { buildGroups, levelTitle, selectedModelOf } from "../../lib/models/groups.js";
  import { isCurrent, menuGroups, RECENT, searchRows, startGroup } from "../../lib/models/menu.js";
  import { coderModel, modelData, watchModels } from "../../lib/models/store.svelte.js";
  import { setEffort } from "../../lib/models/toggles.js";
  import { unfold } from "../../lib/motion.js";
  import { dialogs } from "../../lib/dialogs.js";
  import { focusField } from "../../lib/stores/composer.svelte.js";
  import { closeMenu, notify } from "../../lib/stores/ui.svelte.js";

  let { menu } = $props();

  const GAP = 6; // picture px between the menu and its button
  const MARGIN = 8; // picture px kept from the window's edges

  let box = $state(null);
  let search = $state(null);
  let place = $state(null);
  let query = $state("");
  let chosenGroup = $state("");
  let level = $state(""); // the level just clicked, shown until the config says so

  $effect(() => watchModels());

  const coder = $derived(coderModel(modelData));
  const type = $derived(coder.type ?? "large");
  const current = $derived(coder.selected);
  const built = $derived(modelData.cfg ? buildGroups({ catalog: modelData.catalog, cfg: modelData.cfg, type }) : { items: [] });
  const groups = $derived(menuGroups(built.items));
  const shownGroup = $derived(groups.find((g) => g.name === chosenGroup) ?? groups.find((g) => g.name === startGroup(groups, current)) ?? null);
  const searching = $derived(query.trim() !== "");
  const rows = $derived(searching ? searchRows(built.items, query) : (shownGroup?.items ?? []));
  const levels = $derived(coder.info?.reasoning_levels ?? []);
  const levelNow = $derived(level || current?.reasoning_effort || coder.info?.default_reasoning_effort || "");

  // The first group: the current model's, once the models are read.
  $effect(() => {
    const start = startGroup(groups, current);
    untrack(() => {
      if (!chosenGroup && start) chosenGroup = start;
    });
  });

  function scale() {
    return (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16) / 16;
  }

  // Above the button, its right edge on the button's; below it when there is
  // no room above.
  function measure() {
    const anchor = menu.anchor;
    if (!anchor?.isConnected || !box) return;
    const unit = scale();
    const a = anchor.getBoundingClientRect();
    const width = box.offsetWidth;
    const height = box.offsetHeight;
    let top = a.top - GAP * unit - height;
    const below = top < MARGIN * unit;
    if (below) top = Math.min(a.bottom + GAP * unit, window.innerHeight - MARGIN * unit - height);
    const left = Math.max(MARGIN * unit, Math.min(a.right - width, window.innerWidth - MARGIN * unit - width));
    place = { left, top: Math.max(MARGIN * unit, top), from: below ? "top" : "bottom" };
  }

  $effect(() => {
    void rows.length;
    void groups.length;
    measure();
  });

  function close({ refocus = true } = {}) {
    closeMenu();
    if (refocus) focusField();
  }

  // A choice keeps the menu open (owner, 2026-10-22): the effort levels below
  // follow the model that was just chosen, so a model and its effort can be
  // picked in one visit. The button again, Esc or a click outside closes it.
  // A choice that goes to a sign-in sheet hands the window over instead.
  async function pick(item) {
    if (!item) return;
    const sheetsBefore = dialogs.length;
    await chooseModel({ provider: item.provider, model: selectedModelOf(item), modelType: type });
    if (dialogs.length > sheetsBefore) close({ refocus: false });
  }

  async function pickLevel(next) {
    if (next === levelNow) return;
    level = next;
    try {
      await setEffort(next);
    } catch (error) {
      level = "";
      notify("error", error?.message ?? String(error));
    }
  }

  // Arrow keys move between the rows of a column (the groups, the models and
  // the effort levels), ← → between those columns in that order.
  const COLUMNS = ["groups", "models", "effort"];
  const nextColumn = (from, delta) => COLUMNS[(COLUMNS.indexOf(from) + delta + COLUMNS.length) % COLUMNS.length];
  function columnOf(element) {
    return element?.closest?.("[data-column]")?.dataset.column ?? "";
  }
  function stops(column) {
    return [...(box?.querySelectorAll(`[data-column="${column}"] [data-stop]`) ?? [])];
  }
  function step(delta) {
    const column = columnOf(document.activeElement) || "models";
    const list = stops(column === "search" ? "models" : column);
    if (list.length === 0) return true;
    const at = list.indexOf(document.activeElement);
    const next = at < 0 ? (delta > 0 ? 0 : list.length - 1) : Math.max(0, Math.min(list.length - 1, at + delta));
    list[next].focus();
    return true;
  }
  function across(to) {
    const list = stops(to);
    const target = list.find((el) => el.getAttribute("aria-current") === "true" || el.getAttribute("aria-checked") === "true") ?? list[0];
    target?.focus();
    return true;
  }

  // ← → step to the neighbouring column that has rows, in the columns' order,
  // wrapping at the ends: the effort column shows only with levels.
  function moveAcross(delta) {
    const from = columnOf(document.activeElement) || "models";
    for (let step = 0; step < COLUMNS.length; step += 1) {
      const column = nextColumn(from, delta * (step + 1));
      if (stops(column).length > 0) {
        across(column);
        return true;
      }
    }
    return true;
  }

  onMount(() => {
    measure();
    search?.focus({ preventScroll: true });
    const unbind = [
      bindKey("menu", "Escape", () => (close(), true)),
      bindKey("menu", "ArrowDown", () => step(1)),
      bindKey("menu", "ArrowUp", () => step(-1)),
      bindKey("menu", "ArrowLeft", () => (columnOf(document.activeElement) === "search" ? false : moveAcross(-1))),
      bindKey("menu", "ArrowRight", () => (columnOf(document.activeElement) === "search" ? false : moveAcross(1))),
      bindKey("menu", "Enter", () => {
        if (columnOf(document.activeElement) === "search") {
          pick(rows[0]);
          return true;
        }
        return false; // a focused button clicks itself
      }),
    ];
    // A press outside closes; the model button's own click toggles it. A click in
    // a sheet over the menu is the sheet's own.
    const down = (event) => {
      if (box?.contains(event.target) || menu.anchor?.contains(event.target)) return;
      if (dialogs.length > 0) return;
      close({ refocus: false });
    };
    window.addEventListener("pointerdown", down, true);
    window.addEventListener("resize", measure);
    return () => {
      for (const off of unbind) off();
      window.removeEventListener("pointerdown", down, true);
      window.removeEventListener("resize", measure);
    };
  });
</script>

<div
  class="model-menu"
  role="dialog"
  aria-label="Choose a model"
  data-menu="model"
  bind:this={box}
  style:left={place ? `${place.left}px` : null}
  style:top={place ? `${place.top}px` : null}
  style:visibility={place ? null : "hidden"}
  transition:unfold|global={{ from: place?.from ?? "bottom" }}
>
  <div class="groups" data-column="groups" role="listbox" aria-label="Agents and providers">
    <h3 class="label">Agent</h3>
    {#each groups as group (group.name)}
      <button
        type="button"
        class="group"
        class:recent={group.name === RECENT}
        role="option"
        data-stop
        aria-selected={!searching && shownGroup?.name === group.name}
        aria-current={group.items.some((item) => isCurrent(item, current)) && group.name !== RECENT ? "true" : undefined}
        onclick={() => {
          chosenGroup = group.name;
          query = "";
        }}
      >
        <span class="name">{group.name}</span>
        <Icon name="disclosure" size={9} />
      </button>
    {/each}
  </div>

  <div class="models" data-column="models">
    <label class="search" data-column="search">
      <Icon name="search" size={16} />
      <input bind:this={search} bind:value={query} type="text" placeholder="Search models" aria-label="Search models" spellcheck="false" />
    </label>
    <div class="rows" role="listbox" aria-label="Models">
      {#each rows as item (item.id)}
        <button type="button" class="model" role="option" data-stop aria-selected={isCurrent(item, current)} onclick={() => pick(item)}>
          <span class="name">{item.model.name || item.model.id}</span>
          {#if searching}<span class="info">{item.group}</span>{/if}
          {#if isCurrent(item, current)}<span class="check" aria-hidden="true"><Icon name="check" size={14} /></span>{/if}
        </button>
      {:else}
        <p class="empty">{searching ? "No models match." : modelData.cfg ? "No models here." : "Reading models…"}</p>
      {/each}
    </div>
    {#if levels.length > 0}
      <div class="effort" data-column="effort">
        <h3 class="label">Effort</h3>
        <div class="levels" role="radiogroup" aria-label="Effort">
          {#each levels as l (l)}
            <button type="button" class="level" role="radio" data-stop aria-checked={l === levelNow} onclick={() => pickLevel(l)}>{levelTitle(l)}</button>
          {/each}
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  /* The paper and hairlines of the app's other popups (Popup.svelte), on the
     menu layer the chat's context menu uses; no shadow, as every other menu. */
  .model-menu {
    position: fixed;
    z-index: 30;
    display: grid;
    grid-template-columns: calc(220 * var(--px)) calc(400 * var(--px));
    max-width: calc(100vw - 16 * var(--px));
    max-height: calc(100vh - 16 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    color: var(--ink);
    font: var(--t-item);
    user-select: none;
  }
  .label {
    margin: 0 0 calc(6 * var(--px));
    padding: 0 calc(10 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
  }
  .groups {
    padding: calc(14 * var(--px)) calc(8 * var(--px));
    border-right: 1px solid var(--rule-control);
    overflow-y: auto;
    max-height: calc(480 * var(--px));
  }
  .group,
  .model,
  .level {
    font: inherit;
    color: inherit;
    background: none;
    border: 0;
    cursor: pointer;
  }
  .group,
  .model {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    width: 100%;
    height: calc(38 * var(--px));
    padding: 0 calc(10 * var(--px));
    text-align: left;
  }
  .group :global(svg) {
    flex: none;
    color: var(--slate);
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .group.recent .name {
    font-style: italic;
  }
  .group:hover,
  .model:hover {
    background: var(--control-hover);
  }
  /* Keyboard focus: the app's vermilion hairline, drawn inside the row so the
     scrolling list never cuts it off. */
  .group:focus-visible,
  .model:focus-visible,
  .level:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: -1px;
  }
  .group[aria-selected="true"] {
    background: var(--band);
    box-shadow: inset calc(4 * var(--px)) 0 0 var(--vermilion);
  }
  .models {
    display: flex;
    flex-direction: column;
    min-height: 0;
    padding: calc(10 * var(--px)) calc(8 * var(--px)) calc(12 * var(--px));
  }
  .search {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    height: calc(40 * var(--px));
    margin: 0 0 calc(6 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    color: var(--slate);
  }
  .search:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: 0;
    outline: none;
    background: none;
    font: inherit;
    color: var(--ink);
  }
  .rows {
    flex: 1;
    min-height: 0;
    max-height: calc(340 * var(--px));
    overflow-y: auto;
  }
  .info {
    flex: none;
    color: var(--slate);
  }
  .check {
    flex: none;
    color: var(--vermilion);
  }
  .model[aria-selected="true"] {
    background: var(--band);
  }
  .empty {
    margin: calc(10 * var(--px)) calc(10 * var(--px));
    color: var(--slate);
  }
  .effort {
    margin-top: calc(12 * var(--px));
    padding-top: calc(12 * var(--px));
    border-top: 1px solid var(--rule-control);
  }
  .levels {
    display: flex;
    margin: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
  }
  .level {
    flex: 1;
    height: calc(36 * var(--px));
    padding: 0 calc(6 * var(--px));
    font: var(--t-small);
    text-align: center;
    white-space: nowrap;
  }
  .level + .level {
    border-left: 1px solid var(--rule-control);
  }
  .level:hover {
    background: var(--control-hover);
  }
  /* The chosen level: the band inside an ink hairline, as a selected button. */
  .level[aria-checked="true"] {
    background: var(--band);
    box-shadow: inset 0 0 0 1px var(--ink);
    color: var(--ink);
  }
</style>
