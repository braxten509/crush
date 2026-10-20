<script>
  // Toggle skills (desktop-design-5-6 2.8, look 5.9; DLG-216 to DLG-222):
  // internal/ui/dialog/kept_skills.go. Every skill, by name, in the groups
  // "On", "Toggleable" and "Off" from the shown chat's kept skills. Enter
  // cycles Off → Toggleable → On → Off; the selection follows the skill to
  // its new group. Before the chat exists, it is made first. The sheet
  // follows the chat's record, so the agent's own changes show at once.
  import { tick } from "svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Icon from "../Icon.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { api } from "../../lib/engine/api.js";
  import { GROUP, GROUP_ORDER, keptState, nextKept } from "../../lib/extensions/skills.js";
  import { ensureSession, sessions, upsertSession } from "../../lib/stores/sessions.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";

  const ID = "kept_skills";
  const MAX_ROWS = 12;
  let below = $state(0); // filtered rows under the list's view (kept_skills.go:329)

  let query = $state("");
  let selected = $state(0);
  let busy = false;

  const kept = $derived(sessions.byId[sessions.currentId]?.kept_skills ?? []);
  const items = $derived.by(() => {
    const all = (ws.skills ?? []).slice().sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
    const rows = all.map((skill) => ({ ...skill, state: keptState(skill, kept) }));
    return GROUP_ORDER.flatMap((state) => rows.filter((row) => row.state === state));
  });
  const help = [
    { key: "↑/↓", text: "choose" },
    { key: "enter", text: "toggle" },
    { key: "esc", text: "close" },
  ];

  async function cycle(row) {
    if (!row || busy) return;
    busy = true;
    try {
      const sid = await ensureSession();
      const { on, toggleable } = nextKept(row.state);
      const chat = await api.setKeptSkill(sid, { id: row.id, name: row.name, on, toggleable });
      if (chat?.id) upsertSession(chat, { fromEvent: true });
      await tick();
      const at = items.findIndex((item) => item.id === row.id);
      if (at >= 0) selected = at;
    } catch (e) {
      notify("error", e?.message ?? String(e));
    } finally {
      busy = false;
    }
  }

  let list = $state(null);

  // ↑/↓ stop at the ends (kept_skills.go).
  function onKey(event, combo) {
    if (combo === "ArrowUp" || combo === "ArrowDown") {
      list?.move(combo === "ArrowUp" ? -1 : 1, { wrap: false });
      return true;
    }
    return false;
  }
</script>

<Sheet id={ID} title="Toggle skills" width="small" {help} {onKey}>
  <p class="note">On skills are reminded every message and come back after compression.</p>
  <Field bind:value={query} icon="search" placeholder="Type to filter" label="Filter skills" />
  <FilterList
    bind:this={list}
    {items}
    filterText={(row) => row.name}
    groups={(row) => GROUP[row.state]}
    {query}
    bind:selected
    row={keptRow}
    onChoose={(row) => cycle(row)}
    empty={query ? "No skills match." : "No skills found."}
    label="Skills"
    maxRows={MAX_ROWS}
    bind:below
  />
  {#if below > 0}
    <p class="more"><Icon name="arrow-down" size={12} /> {below} more</p>
  {/if}
</Sheet>

{#snippet keptRow(row)}
  <span class="line">
    <span class="square {row.state}" aria-hidden="true"></span>
    <span class="name">{row.name}</span>
  </span>
{/snippet}

<style>
  .note {
    margin: 0 0 calc(12 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
  }
  .line {
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    min-width: 0;
  }
  .square {
    flex: none;
    box-sizing: border-box;
    width: calc(14 * var(--px));
    height: calc(14 * var(--px));
    border: 1px solid var(--rule-control);
  }
  .square.on {
    background: var(--plan);
    border-color: var(--plan);
  }
  .square.toggleable {
    background: linear-gradient(to right, var(--plan) 50%, transparent 50%);
    border-color: var(--plan);
  }
  .name {
    font: var(--t-item);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .more {
    display: flex;
    align-items: center;
    gap: calc(4 * var(--px));
    margin: calc(6 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
</style>
