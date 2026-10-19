<script>
  // MCP (SIDE-010): the configured servers that have a state, sorted by name
  // (rows.js, a port of mcp.go:158-262): the state mark and name ("Docker
  // MCP" for docker), then the state's words, or when connected the tool,
  // prompt and resource counts that are not zero and "channel". A failed read
  // keeps the old rows (workspace.svelte.js keeps ws.mcps).
  import PanelSection from "./PanelSection.svelte";
  import { mcpCountWords } from "./rows.js";
  import StatusIcon from "./StatusIcon.svelte";

  let { list, label = "MCP" } = $props();
</script>

<PanelSection {label} name="mcp" rows={list.rows} more={list.more > 0 ? `…and ${list.more} more` : ""}>
  {#snippet row(item)}
    <StatusIcon status={item.status} />
    <span class="name">{item.name}</span>
    {#if item.text}<span class="state" class:warn={item.status === "auth"}>{item.text}</span>{/if}
    {#if item.counts}<span class="counts">{mcpCountWords(item.counts)}</span>{/if}
  {/snippet}
</PanelSection>

<style>
  .name {
    flex: none;
    max-width: 60%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state,
  .counts {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--slate);
  }
  .counts {
    margin-left: auto;
  }
  .warn {
    color: var(--amber-ink);
  }
</style>
