<script>
  // The Skills sheet (desktop-design-5-6 2.8, look 5.9; DLG-202 to DLG-215):
  // internal/ui/dialog/skillbrowser/skills.go. Two tabs on the title line,
  // "Installed" and "Browse skills.sh", each with its own filter. Installed:
  // Enter turns the selected skill on or off. Browse: Enter searches
  // skills.sh (two characters at least), then Enter on a result installs it.
  // Ctrl+R reloads the installed list; Esc closes and cancels a running
  // request. ↑/↓ stop at the ends.
  import { onMount, tick } from "svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Rule from "../Rule.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { api } from "../../lib/engine/api.js";
  import { onEvent } from "../../lib/engine/events.js";
  import { labelOf, TEXT } from "../../lib/extensions/skills.js";
  import { readCommands } from "../../lib/palette/store.svelte.js";

  const ID = "skills";

  let tab = $state("installed");
  let installedQuery = $state("");
  let browseQuery = $state("");
  let installed = $state([]);
  let results = $state([]);
  let searchedFor = $state(""); // the query the shown results answer
  let selected = $state(0);
  let status = $state("");
  let error = $state(false);
  let unavailable = $state(false);
  let running = $state(false);
  let list = $state(null);
  let alive = true;
  let seq = 0;

  const items = $derived(tab === "installed" ? installed : results);
  const current = $derived(items[selected] ?? null);
  const installedNames = $derived(new Set(installed.map((s) => s.name)));
  const detail = $derived.by(() => {
    if (!current) return "";
    if (tab === "installed") return current.description ?? "";
    return `${current.source} · https://skills.sh/${current.id}`;
  });
  const help = [
    { key: "tab", text: "switch" },
    { key: "↑/↓", text: "choose" },
    { key: "enter", text: "confirm" },
    { key: "ctrl+r", text: "refresh" },
    { key: "esc", text: "close" },
  ];

  function say(text, isError = false) {
    status = text;
    error = isError;
  }

  async function loadInstalled(keepName = current?.name) {
    const mine = ++seq;
    running = true;
    say(TEXT.loading);
    try {
      const list = await api.installedSkills();
      if (!alive || mine !== seq) return;
      installed = (Array.isArray(list) ? list : []).slice().sort((a, b) => (a.name < b.name ? -1 : 1));
      unavailable = false;
      say("");
      if (tab === "installed") {
        await tick();
        const at = installed.findIndex((s) => s.name === keepName);
        selected = at >= 0 ? at : 0;
      }
    } catch (e) {
      if (!alive || mine !== seq) return;
      if (e?.code === "not_found") {
        unavailable = true;
        say(TEXT.unavailable);
      } else say(`Error: ${e?.message ?? e}`, true);
    } finally {
      if (mine === seq) running = false;
    }
  }

  async function toggle(skill) {
    const mine = ++seq;
    running = true;
    say(TEXT.saving);
    try {
      await api.manageSkill({ action: "toggle", name: skill.name, enabled: !skill.enabled });
      if (!alive || mine !== seq) return;
      say(TEXT.applied);
      readCommands().catch(() => {});
    } catch (e) {
      if (!alive || mine !== seq) return;
      say(`Error: ${e?.message ?? e}`, true);
    } finally {
      if (mine === seq) running = false;
    }
    // A reload failure after a change still refreshes the list (DLG-214).
    const keep = skill.name;
    const savedStatus = status;
    await loadInstalled(keep);
    if (!error) say(savedStatus, savedStatus.startsWith("Error"));
  }

  async function search() {
    const q = browseQuery.trim();
    if (q.length < 2) {
      say(TEXT.short);
      return;
    }
    const mine = ++seq;
    running = true;
    say(TEXT.searching);
    try {
      const list = await api.searchSkills(q);
      if (!alive || mine !== seq) return;
      results = Array.isArray(list) ? list : [];
      searchedFor = q;
      selected = 0;
      say(`${results.length} results from skills.sh`);
    } catch (e) {
      if (!alive || mine !== seq) return;
      say(`Error: ${e?.message ?? e}`, true);
    } finally {
      if (mine === seq) running = false;
    }
  }

  async function install(skill) {
    if (installedNames.has(skill.name)) {
      say(TEXT.already);
      return;
    }
    const mine = ++seq;
    running = true;
    say(TEXT.installing);
    try {
      await api.manageSkill({ action: "install", skill: { id: skill.id, name: skill.name, source: skill.source, skillId: skill.skillId, installs: skill.installs } });
      if (!alive || mine !== seq) return;
      say(TEXT.installed);
      readCommands().catch(() => {});
      loadInstalled().then(() => say(TEXT.installed));
    } catch (e) {
      if (!alive || mine !== seq) return;
      say(`Error: ${e?.message ?? e}`, true);
    } finally {
      if (mine === seq) running = false;
    }
  }

  function switchTab(next) {
    if (next === tab) return;
    tab = next;
    selected = 0;
    if (!running) say(tab === "browse" && results.length > 0 ? `${results.length} results from skills.sh` : "");
  }

  function onKey(event, combo) {
    switch (combo) {
      case "Tab":
      case "Shift+Tab":
        switchTab(tab === "installed" ? "browse" : "installed");
        return true;
      case "ArrowUp":
      case "Ctrl+P":
        list?.move(-1, { wrap: false });
        return true;
      case "ArrowDown":
      case "Ctrl+N":
        list?.move(1, { wrap: false });
        return true;
      case "Ctrl+R":
        if (!running) loadInstalled();
        return true;
      case "Enter": {
        if (running || unavailable) return true;
        if (tab === "installed") {
          const skill = list?.chosen();
          if (skill) toggle(skill);
        } else if (browseQuery.trim() !== searchedFor || results.length === 0) search();
        else {
          const skill = list?.chosen();
          if (skill) install(skill);
        }
        return true;
      }
      default:
        return false;
    }
  }

  onMount(() => {
    loadInstalled();
    const off = onEvent("skills_event", () => {
      if (!running) loadInstalled();
    });
    return () => {
      alive = false;
      seq += 1;
      off();
    };
  });
</script>

<Sheet id={ID} title="Skills" width="standard" maxHeight={680} {help} {onKey} {controls}>
  {#if tab === "installed"}
    <Field bind:value={installedQuery} icon="search" placeholder="Filter installed skills" label="Filter installed skills" />
  {:else}
    <Field bind:value={browseQuery} icon="search" placeholder="Search skills.sh (Enter to search)" label="Search skills.sh" />
  {/if}
  {#if !unavailable}
    <FilterList
      bind:this={list}
      {items}
      filterText={tab === "installed" ? (s) => `${s.name} ${s.description ?? ""}` : (s) => s.name}
      query={tab === "installed" ? installedQuery : ""}
      bind:selected
      row={tab === "installed" ? installedRow : browseRow}
      empty={tab === "installed" ? TEXT.emptyInstalled : searchedFor ? TEXT.noResults : TEXT.emptyBrowse}
      label="Skills"
    />
    <Rule />
    <p class="detail">{detail}</p>
    <Rule />
  {/if}
  {#if status}<p class="status" class:error>{status}</p>{/if}
</Sheet>

{#snippet controls()}
  <button type="button" aria-pressed={tab === "installed"} onclick={() => switchTab("installed")}>Installed</button>
  <button type="button" aria-pressed={tab === "browse"} onclick={() => switchTab("browse")}>Browse skills.sh</button>
{/snippet}

{#snippet installedRow(skill)}
  <span class="line" class:off={!skill.enabled}>
    {#if labelOf(skill)}<span class="label">{labelOf(skill)}</span>{/if}
    <span class="name">{skill.name}</span>
    <span class="info">{skill.enabled ? "Enabled" : "Disabled"}</span>
  </span>
{/snippet}

{#snippet browseRow(skill)}
  <span class="line">
    <span class="name">{skill.name}</span>
    <span class="info">{installedNames.has(skill.name) ? "Installed" : `${skill.installs ?? 0} installs`}</span>
  </span>
{/snippet}

<style>
  .line {
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    flex: 1;
    min-width: 0;
  }
  .label {
    font: var(--t-agent-mono);
    color: var(--slate);
  }
  .name {
    flex: 1;
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .info {
    font: var(--t-small);
    color: var(--ink);
  }
  .off .name,
  .off .info {
    color: var(--slate);
  }
  .detail {
    min-height: calc(52 * var(--px));
    margin: calc(6 * var(--px)) 0;
    font: var(--t-body-small);
    color: var(--slate);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .status {
    margin: calc(8 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--ink);
  }
  .status.error {
    color: var(--error-ink);
  }
</style>
