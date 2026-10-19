<script>
  // Toggle MCPs (desktop-design-5-6 2.9, look 5.10; DLG-232 to DLG-238):
  // internal/ui/dialog/mcpservers/toggles.go. "Local" (this repository's
  // overrides) and "Global" (the config's `disabled`) on the title line, Tab
  // switches. Rows by name with the status mark and text; Enter or Space
  // toggles and the row changes at once, then follows mcp_event. ↑/K and
  // ↓/J stop at the ends. Esc closes without reverting.
  import { onMount } from "svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import StatusIcon from "../panel/StatusIcon.svelte";
  import { api } from "../../lib/engine/api.js";
  import { onEvent } from "../../lib/engine/events.js";
  import { listOf, markOf, statusText } from "../../lib/extensions/mcp.js";
  import { notify } from "../../lib/stores/ui.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";

  const ID = "mcp_toggles";

  let scope = $state("local");
  let states = $state({});
  let disabledHere = $state(new Set());
  let enabledHere = $state(new Set());
  let starting = $state(new Set()); // locally enabled, waiting for the first state
  let selected = $state(0);
  let list = $state(null);
  let seq = 0;

  const names = $derived(Object.keys(ws.config?.mcp ?? {}).sort());
  const rows = $derived(
    names.map((name) => {
      const globallyDisabled = ws.config?.mcp?.[name]?.disabled === true;
      // The effective local state: a globally disabled server stays off here
      // unless this repository turned it on (toggles.go:38-40).
      const locallyDisabled = disabledHere.has(name) || (globallyDisabled && !enabledHere.has(name));
      const off = scope === "local" ? locallyDisabled : globallyDisabled;
      // Global scope labels the config flag: a server this repository turned
      // off is still "enabled" there (toggles.go:312-328).
      let text = statusText(states[name], { locallyDisabled: scope === "local" ? locallyDisabled : globallyDisabled });
      if (scope === "global" && !globallyDisabled && locallyDisabled) text = "enabled";
      if (starting.has(name) && !off) text = "starting";
      return { name, off, text };
    }),
  );
  const help = [
    { key: "tab", text: "switch" },
    { key: "↑/↓", text: "choose" },
    { key: "enter", text: "toggle" },
    { key: "esc", text: "close" },
  ];

  async function read() {
    const mine = ++seq;
    const [s, d, e] = await Promise.allSettled([api.mcpStates(), api.mcpDisabled(), api.mcpEnabled()]);
    if (mine !== seq) return;
    if (s.status === "fulfilled") states = s.value ?? {};
    if (d.status === "fulfilled") disabledHere = new Set(listOf(d.value));
    if (e.status === "fulfilled") enabledHere = new Set(listOf(e.value));
  }

  async function toggle(row) {
    if (!row) return;
    const disabled = !row.off;
    try {
      if (scope === "local") {
        await api.setMcpDisabled({ name: row.name, disabled });
        const next = new Set(disabledHere);
        if (disabled) next.add(row.name);
        else next.delete(row.name);
        disabledHere = next;
        if (!disabled) {
          const nextEnabled = new Set(enabledHere);
          nextEnabled.add(row.name);
          enabledHere = nextEnabled;
          starting = new Set([...starting, row.name]);
        }
        notify("info", `MCP "${row.name}" ${disabled ? "disabled" : "enabled"} for this repository`);
      } else {
        await api.setMcpConfigDisabled({ name: row.name, disabled });
        if (!disabled) starting = new Set([...starting, row.name]);
        notify("info", `MCP "${row.name}" ${disabled ? "disabled" : "enabled"} globally`);
      }
    } catch (e) {
      notify("error", e?.message ?? String(e));
      read();
    }
  }

  function onKey(event, combo) {
    switch (combo) {
      case "Tab":
      case "Shift+Tab":
        scope = scope === "local" ? "global" : "local";
        return true;
      case "ArrowUp":
      case "k":
        list?.move(-1, { wrap: false });
        return true;
      case "ArrowDown":
      case "j":
        list?.move(1, { wrap: false });
        return true;
      case "Enter":
      case "Space":
        toggle(list?.chosen());
        return true;
      default:
        return false;
    }
  }

  onMount(() => {
    read();
    const offMcp = onEvent("mcp_event", (_kind, event) => {
      const name = event?.name;
      if (name && starting.has(name) && event?.state && event.state !== "starting") {
        const next = new Set(starting);
        next.delete(name);
        starting = next;
      }
      read();
    });
    const offConfig = onEvent("config_changed", () => read());
    return () => {
      seq += 1;
      offMcp();
      offConfig();
    };
  });
</script>

<Sheet id={ID} title="Toggle MCPs" width="small" {help} {onKey} {controls}>
  <FilterList
    bind:this={list}
    items={rows}
    filterText={(row) => row.name}
    bind:selected
    row={mcpRow}
    onChoose={(row) => toggle(row)}
    empty="No MCP servers configured."
    label="MCP servers"
  />
</Sheet>

{#snippet controls()}
  <button type="button" aria-pressed={scope === "local"} onclick={() => (scope = "local")}>Local</button>
  <button type="button" aria-pressed={scope === "global"} onclick={() => (scope = "global")}>Global</button>
{/snippet}

{#snippet mcpRow(row)}
  <span class="line">
    <StatusIcon status={markOf(row.text)} />
    <span class="name">{row.name}</span>
    <span class="info" class:error={row.text.startsWith("error")}>{row.text}</span>
  </span>
{/snippet}

<style>
  .line {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    flex: 1;
    min-width: 0;
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
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 50%;
  }
  .info.error {
    color: var(--error-ink);
  }
</style>
