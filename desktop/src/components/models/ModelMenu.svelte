<script>
  // The model menu (owner, 2026-10-19: picker "B"), opened by the model
  // button and drawn by OverlayHost while ui.menu is {kind: "model", anchor}.
  // It unrolls up from the button: the agents and providers on the left, the
  // chosen one's models on the right with a search line over them, and the
  // coder model's effort levels at the bottom. A click on a model chooses it
  // (lib/models/choose.js) and closes the menu; a click on a level saves it.
  // Esc, a click outside or the button again close it. While it is open the
  // key router asks only the "menu" scope, so no other shortcut fires.
  // The full sheet (Large/Small Task, sign-in edits) stays on Ctrl+P.
  // Under the effort, a Claude Code or Codex model with two or more accounts
  // gets an Account drop-down the width of the row (owner, 2026-10-21, pick
  // A as one drop-down); a new model of the same tool keeps the account.
  // Under OpenRouter's saved models, "+ Add model" opens the catalog sheet
  // (OpenRouterAdd.svelte, owner 2026-10-22, pick B); a right-click on a
  // saved one offers Remove. A model is chosen with the effort last used
  // with it (lib/models/groups.js selectedModelOf).
  import { onMount, untrack } from "svelte";
  import Icon from "../Icon.svelte";
  import { bindKey } from "../../lib/keys.js";
  import { chooseModel } from "../../lib/models/choose.js";
  import { buildGroups, levelTitle, selectedModelOf } from "../../lib/models/groups.js";
  import { isCurrent, menuGroups, RECENT, searchRows, startGroup } from "../../lib/models/menu.js";
  import { coderModel, modelData, refreshModels, watchModels } from "../../lib/models/store.svelte.js";
  import { addableProviders, savedWithout } from "../../lib/models/openrouter.js";
  import { openDialog } from "../../lib/dialogs.js";
  import { api, SCOPE } from "../../lib/engine/api.js";
  import { setAccount, setEffort } from "../../lib/models/toggles.js";
  import { fallback, loadFallback } from "../../lib/fallback/store.svelte.js";
  import { hasAccounts } from "../../lib/fallback/blocks.js";
  import { unfold } from "../../lib/motion.js";
  import { focusField } from "../../lib/stores/composer.svelte.js";
  import { closeMenu, notify } from "../../lib/stores/ui.svelte.js";

  let { menu } = $props();

  const GAP = 6; // picture px between the menu and its button
  const MARGIN = 8; // picture px kept from the window's edges

  let box = $state(null);
  let search = $state(null);
  let place = $state(null);
  let query = $state("");
  let chosenGroup = $state(untrack(() => menu.group ?? ""));
  let removing = $state(null); // {item, left, top}: the right-click's Remove
  let level = $state(""); // the level just clicked, shown until the config says so
  let accountsOpen = $state(false);
  let account = $state(null); // the account just picked, shown until the config says so

  $effect(() => watchModels());

  const coder = $derived(coderModel(modelData));
  const type = $derived(coder.type ?? "large");
  const current = $derived(coder.selected);
  const built = $derived(modelData.cfg ? buildGroups({ catalog: modelData.catalog, cfg: modelData.cfg, type }) : { items: [] });
  const groups = $derived(menuGroups(built.items, addableProviders(modelData.cfg)));
  const shownGroup = $derived(groups.find((g) => g.name === chosenGroup) ?? groups.find((g) => g.name === startGroup(groups, current)) ?? null);
  const searching = $derived(query.trim() !== "");
  const rows = $derived(searching ? searchRows(built.items, query) : (shownGroup?.items ?? []));
  const levels = $derived(coder.info?.reasoning_levels ?? []);
  const levelNow = $derived(level || current?.reasoning_effort || coder.info?.default_reasoning_effort || "");
  const kind = $derived(kindOf(current?.provider));
  const accounts = $derived(hasAccounts(kind) ? fallback.accounts.filter((a) => a.kind === kind) : []);
  const accountNow = $derived(account ?? current?.account ?? "");
  const accountShown = $derived(accounts.find((a) => a.id === accountNow) ?? accounts[0] ?? null);
  // The account belongs to the model in use, so it shows only on that
  // model's own provider, not while another one is browsed (owner,
  // 2026-10-21).
  const onCurrentGroup = $derived(!searching && shownGroup?.name === startGroup(groups, current));

  // The tool behind a configured provider id (its type), or "".
  function kindOf(provider) {
    for (const [id, p] of modelData.cfg?.providers ?? []) if (id === provider) return p?.type ?? "";
    return "";
  }

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

  function pick(item) {
    if (!item) return;
    close();
    const model = selectedModelOf(item, modelData.cfg?.model_efforts);
    if (accountNow && kindOf(item.provider.id) === kind) model.account = accountNow;
    chooseModel({ provider: item.provider, model, modelType: type });
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

  async function pickAccount(next) {
    accountsOpen = false;
    if (next.id === accountNow) return;
    account = next.id;
    try {
      await setAccount(next.id, next.label || next.name);
    } catch (error) {
      account = null;
      notify("error", error?.message ?? String(error));
    }
  }

  // "+ Add model": the catalog sheet, which opens this menu again on the
  // group once the picked models are saved.
  function addModels(group) {
    const anchor = menu.anchor;
    close({ refocus: false });
    openDialog("openrouter_add", { providerId: group.addTo, providerName: group.name, anchor });
  }

  function offerRemove(event, item) {
    if (!shownGroup?.addTo || item.provider.id !== shownGroup.addTo || searching) return;
    event.preventDefault();
    const at = box.getBoundingClientRect();
    removing = { item, left: event.clientX - at.left, top: event.clientY - at.top };
  }

  async function remove(item) {
    removing = null;
    if (isCurrent(item, current)) {
      notify("warn", "This model is in use; choose another one first");
      return;
    }
    try {
      // The saved entries as the engine holds them, prices included.
      const raw = await api.config();
      const id = item.provider.id;
      await api.setConfig(SCOPE.global, `providers.${id}.models`, savedWithout(raw?.providers?.[id]?.models, item.model.id));
      await refreshModels();
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }

  // Arrow keys move between the rows of a column, ← → between the columns.
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

  onMount(() => {
    measure();
    search?.focus({ preventScroll: true });
    if (!fallback.loaded) loadFallback();
    const unbind = [
      bindKey("menu", "Escape", () => (removing ? (removing = null) : close(), true)),
      bindKey("menu", "ArrowDown", () => step(1)),
      bindKey("menu", "ArrowUp", () => step(-1)),
      bindKey("menu", "ArrowLeft", () => (columnOf(document.activeElement) === "search" ? false : across("groups"))),
      bindKey("menu", "ArrowRight", () => (columnOf(document.activeElement) === "search" ? false : across("models"))),
      bindKey("menu", "Enter", () => {
        if (columnOf(document.activeElement) === "search") {
          pick(rows[0]);
          return true;
        }
        return false; // a focused button clicks itself
      }),
    ];
    // A press outside closes; the model button's own click toggles it.
    const down = (event) => {
      if (removing && !event.target?.closest?.(".remove-menu")) removing = null;
      if (box?.contains(event.target) || menu.anchor?.contains(event.target)) return;
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
        <button type="button" class="model" role="option" data-stop aria-selected={isCurrent(item, current)} onclick={() => pick(item)} oncontextmenu={(event) => offerRemove(event, item)}>
          <span class="name">{item.model.name || item.model.id}</span>
          {#if searching}<span class="info">{item.group}</span>{/if}
          {#if isCurrent(item, current)}<span class="check" aria-hidden="true"><Icon name="check" size={14} /></span>{/if}
        </button>
      {:else}
        <p class="empty">{searching ? "No models match." : shownGroup?.addTo ? "No models saved yet." : modelData.cfg ? "No models here." : "Reading models…"}</p>
      {/each}
      {#if shownGroup?.addTo && !searching}
        <button type="button" class="model add" data-stop onclick={() => addModels(shownGroup)}>
          <Icon name="plus" size={14} />
          <span class="name">Add model</span>
        </button>
      {/if}
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
    {#if removing}
      <div class="remove-menu" role="menu" style:left="{removing.left}px" style:top="{removing.top}px">
        <button type="button" class="option" role="menuitem" onclick={() => remove(removing.item)}>Remove</button>
      </div>
    {/if}
    {#if accounts.length > 1 && onCurrentGroup}
      <div class="account" data-column="account">
        <h3 class="label">Account</h3>
        <div class="pick">
          {#if accountsOpen}
            <div class="accounts" role="listbox" aria-label="Accounts">
              {#each accounts as a (a.id)}
                <button type="button" class="option" role="option" data-stop aria-selected={a.id === accountShown?.id} onclick={() => pickAccount(a)}>
                  <span class="name">{a.label || a.name}</span>
                  {#if a.id === accountShown?.id}<span class="check" aria-hidden="true"><Icon name="check" size={14} /></span>{/if}
                </button>
              {/each}
            </div>
          {/if}
          <button type="button" class="drop" data-stop aria-haspopup="listbox" aria-expanded={accountsOpen} onclick={() => (accountsOpen = !accountsOpen)}>
            <span class="name">{accountShown ? accountShown.label || accountShown.name : ""}</span>
            <Icon name="caret" size={10} />
          </button>
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
    background: var(--pick);
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
    background: var(--pick);
  }
  .model.add {
    color: var(--slate);
  }
  .model.add :global(svg) {
    flex: none;
  }
  .model.add:hover {
    color: var(--ink);
  }
  /* The right-click's Remove, at the pointer, over the rows. */
  .remove-menu {
    position: absolute;
    z-index: 2;
    min-width: calc(140 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
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
  .account {
    margin-top: calc(12 * var(--px));
  }
  .pick {
    position: relative;
    margin: 0 calc(10 * var(--px));
  }
  .drop,
  .option {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    box-sizing: border-box;
    width: 100%;
    height: calc(36 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 0;
    background: none;
    font: var(--t-small);
    color: var(--ink);
    text-align: left;
    cursor: pointer;
  }
  .drop {
    border: 1px solid var(--rule-control);
  }
  .drop :global(svg) {
    flex: none;
    color: var(--slate);
  }
  .drop[aria-expanded="true"] :global(svg) {
    transform: rotate(180deg);
  }
  /* The list opens up over the models, so the menu keeps its size. */
  .accounts {
    position: absolute;
    left: 0;
    right: 0;
    bottom: calc(100% - 1px);
    z-index: 1;
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
  }
  .drop:hover,
  .option:hover {
    background: var(--control-hover);
  }
  .option[aria-selected="true"] {
    background: var(--pick);
  }
  .drop:focus-visible,
  .option:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: -1px;
  }
  /* The chosen level: the band inside an ink hairline, as a selected button. */
  .level[aria-checked="true"] {
    background: var(--band);
    box-shadow: inset 0 0 0 1px var(--ink);
    color: var(--ink);
  }
</style>
