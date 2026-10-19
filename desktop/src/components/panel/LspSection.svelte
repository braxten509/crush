<script>
  // LSP (SIDE-009): one row per language server, sorted by name (rows.js, a
  // port of lsp.go:96-201): its state mark and name, then the state's words
  // or, when ready, the error, warning, hint and info counts that are not
  // zero (errors with the cross, warnings with the triangle, 4.13; hints and
  // infos keep the terminal's letters H and I).
  import Icon from "../Icon.svelte";
  import PanelSection from "./PanelSection.svelte";
  import StatusIcon from "./StatusIcon.svelte";

  let { list, label = "LSP" } = $props();
</script>

<PanelSection {label} name="lsp" rows={list.rows} more={list.more > 0 ? `…and ${list.more} more` : ""}>
  {#snippet row(item)}
    <StatusIcon status={item.status} />
    <span class="name">{item.name}</span>
    {#if item.text}<span class="state">{item.text}</span>{/if}
    {#if item.counts}
      <span class="counts">
        {#if item.counts.error > 0}<span class="count error"><Icon name="cross" size={12} />{item.counts.error}<span class="visually-hidden"> errors</span></span>{/if}
        {#if item.counts.warning > 0}<span class="count warning"><Icon name="warning" size={12} />{item.counts.warning}<span class="visually-hidden"> warnings</span></span>{/if}
        {#if item.counts.hint > 0}<span class="count letter">H{item.counts.hint}</span>{/if}
        {#if item.counts.information > 0}<span class="count letter">I{item.counts.information}</span>{/if}
      </span>
    {/if}
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
  .state {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--slate);
  }
  .counts {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    margin-left: auto;
    flex: none;
  }
  .count {
    display: flex;
    align-items: center;
    gap: calc(4 * var(--px));
  }
  .error {
    color: var(--error-ink);
  }
  .warning {
    color: var(--amber-ink);
  }
  .letter {
    color: var(--slate);
  }
</style>
