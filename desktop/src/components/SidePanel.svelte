<script>
  // The right column: the project, the three usage meters (context, 5-hour and
  // week limits), and the visuals the agent drew for this chat: a flow and a table.
  // Scaffold S3: the data comes from sample.js until piece P3.8 reads it from the
  // stores (the panel takes no props).
  import { sample } from "../lib/sample.js";
  import Icon from "./Icon.svelte";
  import Rule from "./Rule.svelte";

  const { project, usage, visuals } = sample;

  // Meters share the width, with a one-pixel line in each gap. The usual three
  // keep the approved drawing's exact proportions; any other count shares evenly.
  const usageColumns = $derived(
    usage.length === 3
      ? "116fr 32fr 115fr 31fr 114fr"
      : Array.from({ length: usage.length }, () => "115fr").join(" 31fr "),
  );

  // Short labels never end on a lone word: the last two words stay together
  // ("Check / session cookie", not "Check session / cookie").
  function keepLastWords(label) {
    return label.split(" ").length > 2 ? label.replace(/ (\S+)$/, "\u00a0$1") : label;
  }
</script>

<aside class="panel" aria-label="Project and visuals">
  <section class="project" aria-labelledby="project-label">
    <h2 class="label" id="project-label">Project</h2>
    <p class="project-name">{project}</p>
  </section>

  <Rule caret={26} class="caret-rule" />

  <section class="usage" aria-label="Usage" style:grid-template-columns={usageColumns}>
    {#each usage as meter, i (meter.id)}
      {#if i > 0}<span class="separator" aria-hidden="true"></span>{/if}
      <div class="cell">
        <h3 class="label">{meter.label}</h3>
        <p class="value">
          <span>{meter.percent}%</span>
          {#if meter.warn}
            <Icon name="warning" />
            <span class="visually-hidden">near the limit</span>
          {/if}
        </p>
        <div
          class="meter"
          role="meter"
          aria-label="{meter.label} used"
          aria-valuemin="0"
          aria-valuemax="100"
          aria-valuenow={meter.percent}
        >
          <span class="fill" style:width="{meter.percent}%"></span>
        </div>
      </div>
    {/each}
  </section>

  <Rule marks="both" class="under-usage" />

  <section class="visuals" aria-labelledby="visuals-label">
    <h2 class="label" id="visuals-label">Visuals</h2>

    <figure class="flow-figure">
      <figcaption class="heading">{visuals.flow.title}</figcaption>
      <ol class="flow">
        {#each visuals.flow.steps as step, i (i)}
          <li>
            {#if i > 0}
              <span class="arrow" aria-hidden="true"><span class="shaft"></span><Icon name="arrowhead" /></span>
            {/if}
            <span class="node">{keepLastWords(step)}</span>
          </li>
        {/each}
      </ol>
    </figure>

    <Rule caret={26} class="caret-rule after-flow" />

    <figure class="table-figure">
      <figcaption class="heading">{visuals.table.title}</figcaption>
      <table>
        <colgroup>
          <col class="first" />
          <col class="second" />
          <col />
        </colgroup>
        <thead>
          <tr>
            {#each visuals.table.columns as column (column)}
              <th scope="col">{column}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each visuals.table.rows as row, r (r)}
            <tr>
              {#each row as cell, c (c)}
                <td>{cell}</td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </figure>
  </section>
</aside>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
  .panel {
    position: relative;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    padding: calc(26 * var(--px)) calc(19 * var(--px)) 0 calc(26 * var(--px));
  }

  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }

  .project-name {
    font: var(--t-project);
    letter-spacing: -0.02em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .panel :global(.rule.caret-rule) {
    margin: calc(19 * var(--px)) 0 0 calc(-1 * var(--px));
  }

  .usage {
    display: grid;
    height: calc(110 * var(--px));
    margin-top: calc(21 * var(--px));
  }
  .cell {
    position: relative;
    padding-top: calc(7 * var(--px));
  }
  .separator {
    width: 1px;
    margin-left: calc(15 * var(--px));
  }
  .separator:nth-of-type(2) {
    margin-left: calc(14 * var(--px));
  }
  .separator {
    background: var(--rule-frame);
  }
  .value {
    display: flex;
    align-items: flex-start;
    margin-top: calc(10 * var(--px));
    font: var(--t-figure);
  }
  .value :global(.icon) {
    margin: calc(2 * var(--px)) 0 0 calc(16 * var(--px));
  }
  .meter {
    position: relative;
    box-sizing: border-box;
    height: calc(13 * var(--px));
    margin-top: calc(9 * var(--px));
    border: 1px solid var(--meter-edge);
    background: var(--meter-track);
  }
  .fill {
    position: absolute;
    left: -1px;
    top: -1px;
    bottom: -1px;
    background: var(--meter);
  }

  .panel :global(.rule.under-usage) {
    margin: calc(22 * var(--px)) calc(-19 * var(--px)) 0 calc(-26 * var(--px));
  }

  .visuals > .label {
    margin-top: calc(22 * var(--px));
  }
  .heading {
    font: var(--t-heading);
  }
  .flow-figure,
  .table-figure {
    margin: 0;
  }
  .flow-figure .heading {
    margin-top: calc(29 * var(--px));
  }

  /* The flow is drawn from data: boxes in ink, arrows that take the space between. */
  .flow {
    display: flex;
    align-items: stretch;
    height: calc(85 * var(--px));
    margin-top: calc(16 * var(--px));
  }
  .flow li {
    display: contents;
  }
  .node {
    flex: none;
    box-sizing: border-box;
    min-width: calc(98 * var(--px));
    max-width: calc(142 * var(--px));
    padding: calc(3 * var(--px)) calc(7 * var(--px)) 0;
    border: calc(2 * var(--px)) solid var(--figure);
    display: grid;
    place-items: center;
    text-align: center;
    text-wrap: balance;
    font: var(--t-body-small);
    line-height: calc(24 * var(--px));
  }
  .arrow {
    flex: 1 1 0;
    min-width: calc(28 * var(--px));
    display: flex;
    align-items: center;
    padding-right: calc(4 * var(--px));
    color: var(--figure);
  }
  .shaft {
    flex: 1;
    height: max(1px, calc(1.6 * var(--px)));
    background: currentColor;
  }

  .panel :global(.rule.after-flow) {
    margin-top: calc(37 * var(--px));
  }
  .table-figure .heading {
    margin-top: calc(19 * var(--px));
  }

  /* Collapsed borders: a cell's border beats the table's, so the outer edge is set on the edge cells. */
  table {
    width: 100%;
    margin-top: calc(17 * var(--px));
    table-layout: fixed;
    border-collapse: collapse;
  }
  col.first {
    width: calc(141 * var(--px));
  }
  col.second {
    width: calc(136 * var(--px));
  }
  th,
  td {
    text-align: left;
    padding-left: calc(17 * var(--px));
    border: 1px solid var(--rule-table-inner);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  th {
    padding-top: calc(15 * var(--px));
    padding-bottom: calc(10 * var(--px));
    font: var(--t-label);
    letter-spacing: 0.02em;
    text-transform: uppercase;
  }
  td {
    padding-top: calc(12 * var(--px));
    padding-bottom: calc(9 * var(--px));
    font: var(--t-body-small);
  }
  thead th {
    border-top-color: var(--rule-table);
  }
  tr > :first-child {
    border-left-color: var(--rule-table);
  }
  tr > :last-child {
    border-right-color: var(--rule-table);
  }
  tbody tr:last-child td {
    border-bottom-color: var(--rule-table);
  }
</style>
