<script>
  // The landing page (START-003, CHAT-001; the terminal's landingView,
  // internal/ui/model/landing.go:25-63): the folder and its Git branch, the
  // model line, and three columns of LSPs, MCPs and skills in the panel's row
  // parts, each cut to the rows that fit with "…and N more". The wordmark is
  // in the head (ChatHead); the composer stays below and the panel stays.
  import Icon from "../Icon.svelte";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import StartLayer from "./StartLayer.svelte";
  import { screen, start } from "./start.svelte.js";
  import { fitRows, lspRows, mcpRows, modelLine, pathLine, providerName, skillRows } from "./startmodel.js";
  import { shown } from "./shown.js";

  const active = $derived(screen() === "landing");
  const model = $derived(modelLine(ws.agent, providerName(ws.agent?.model_cfg?.provider, start.providerNames, ws.providers)));
  const where = $derived(pathLine(conn.project.path, ws.branch));
  const columns = $derived([
    { key: "lsps", title: "LSP", rows: lspRows(ws.lsps) },
    { key: "mcps", title: "MCP", rows: mcpRows(ws.mcps) },
    { key: "skills", title: "Skills", rows: skillRows(ws.skills) },
  ]);

  // Rows are calc(28 * var(--px)) tall and a column's label takes 32 more,
  // so the rows that fit follow from the columns' height (the sizer is one row).
  let area = $state(0);
  let rowHeight = $state(0);
  const room = $derived(rowHeight > 0 ? Math.floor((area - (rowHeight * 32) / 28) / rowHeight) : 1);
  const fitted = $derived(columns.map((c) => ({ ...c, fit: fitRows(c.rows, room) })));

  $effect(() => {
    shown.landing = active
      ? {
          path_line: where,
          model: model ? [model.name, model.via, model.reasoning].filter(Boolean).join(" · ") : "",
          room,
          columns: Object.fromEntries(
            fitted.map((c) => [c.key, { title: c.title, rows: c.fit.shown.map((r) => [r.name, r.text].filter(Boolean).join(" ")), more: c.fit.more, none: c.rows.length === 0 }]),
          ),
        }
      : null;
    return () => {
      shown.landing = null;
    };
  });
</script>

{#if active}
  <StartLayer kind="landing" label="Start">
    <div class="block">
      <p class="where" title={conn.project.path}>{where}</p>
      {#if model}
        <p class="model">
          <span class="name">{model.name}</span>
          {#if model.via}<span class="via">{model.via}</span>{/if}
        </p>
        {#if model.reasoning}
          <p class="reasoning">{model.reasoning}</p>
        {/if}
      {/if}
      <div class="columns" bind:clientHeight={area}>
        <span class="sizer" aria-hidden="true" bind:clientHeight={rowHeight}></span>
        {#each fitted as column (column.key)}
          <section class="column {column.key}" data-column={column.key} aria-label={column.title}>
            <h3 class="label">{column.title}</h3>
            {#if column.rows.length === 0}
              <p class="none">None</p>
            {:else}
              <ul class="rows">
                {#each column.fit.shown as row}
                  <li class="item">
                    <Icon name={row.icon} size={16} class="icon {row.tone ?? ''}" />
                    <span class="name">{row.name}</span>
                    {#if row.text}<span class="text">{row.text}</span>{/if}
                    {#if row.counts}
                      {#if row.counts.error > 0}<span class="count error"><Icon name="cross" size={12} />{row.counts.error}</span>{/if}
                      {#if row.counts.warning > 0}<span class="count warn"><Icon name="warning" size={12} />{row.counts.warning}</span>{/if}
                      {#if row.counts.hint > 0}<span class="count">{row.counts.hint} hints</span>{/if}
                      {#if row.counts.information > 0}<span class="count">{row.counts.information} info</span>{/if}
                    {/if}
                  </li>
                {/each}
                {#if column.fit.more > 0}
                  <li class="more">…and {column.fit.more} more</li>
                {/if}
              </ul>
            {/if}
          </section>
        {/each}
      </div>
    </div>
  </StartLayer>
{/if}

<style>
  /* CHAT-001: centred, its content at most 720 picture px wide and at least
     60 from each edge of the column. */
  .block {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    box-sizing: border-box;
    width: 100%;
    max-width: calc(840 * var(--px));
    margin: 0 auto;
    padding: calc(30 * var(--px)) calc(60 * var(--px)) calc(24 * var(--px));
  }

  .where {
    font: var(--t-stamp);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .model {
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    margin-top: calc(18 * var(--px));
    min-width: 0;
    white-space: nowrap;
  }
  .model .name {
    font: var(--t-heading);
    color: var(--ink);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .model .via {
    flex: none;
    font: var(--t-meta);
    color: var(--slate);
  }
  .reasoning {
    margin-top: calc(4 * var(--px));
    font: var(--t-meta);
    color: var(--slate);
  }

  .columns {
    position: relative;
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: calc(24 * var(--px));
    flex: 1;
    min-height: 0;
    margin-top: calc(30 * var(--px));
    overflow: hidden;
  }
  .sizer {
    position: absolute;
    visibility: hidden;
    height: calc(28 * var(--px));
    width: 1px;
  }
  .column {
    min-width: 0;
  }
  .label {
    height: calc(20 * var(--px));
    margin-bottom: calc(12 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .none,
  .more {
    font: var(--t-small);
    line-height: calc(28 * var(--px));
    color: var(--slate);
  }
  .item {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    height: calc(28 * var(--px));
    min-width: 0;
    font: var(--t-small);
    white-space: nowrap;
  }
  .item :global(.icon) {
    color: var(--ink);
  }
  .item :global(.icon.error),
  .count.error {
    color: var(--error-ink);
  }
  .item :global(.icon.warn) {
    color: var(--amber-ink);
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--ink);
  }
  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--slate);
  }
  .count {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: calc(3 * var(--px));
    color: var(--slate);
  }
</style>
