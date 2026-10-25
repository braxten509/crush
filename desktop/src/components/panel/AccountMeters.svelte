<script>
  // Every sign-in's usage (owner, 2026-10-21, the compact pick): under the
  // meters, one heading row naming the columns every tool shares (5h, Week,
  // Month; owner, 2026-10-25), then one line per account of every tool that
  // reports usage: its name (Claude, Claude 2, Codex, ...; a dot on the one
  // in use), each span's part left as a small bar and a percent, amber under
  // 30 % and rose under 10 % as the meters above. Shown
  // only while Settings > Appearance > "Show every account's usage" is on;
  // lib/stores/accountmeters.svelte.js reads them.
  import { limitKind } from "../../lib/footer.js";
  import { fallback } from "../../lib/fallback/store.svelte.js";
  import { SPANS, accountGroups, accountMeters } from "../../lib/stores/accountmeters.svelte.js";
  import { limits } from "../../lib/stores/limits.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { agentNow } from "./panel.svelte.js";

  const groups = $derived(
    accountMeters.on
      ? accountGroups({
          accounts: fallback.accounts,
          byKind: limits.byKind,
          // The shown chat's agent: a sub-agent's chat marks its own account.
          inUse: { kind: limitKind(agentNow(), ws.providers) ?? "", account: agentNow()?.model_cfg?.account ?? "" },
        })
      : [],
  );

  const NOTE = { checking: "checking…", "signed-out": "signed out", error: "can't read" };
</script>

{#if groups.length > 0}
  <section class="panel-section accounts" data-section="accounts" aria-label="Accounts" style:--spans={SPANS.length}>
    <h2 class="head">
      <span class="label"></span>
      {#each SPANS as span (span)}<span class="col">{span}</span>{/each}
    </h2>
    {#each groups as group (group.kind)}
      <div class="group" data-kind={group.kind}>
        {#each group.rows as row (row.id)}
          <div class="row" class:idle={row.state === "idle"} data-account={row.id}>
            <span class="name" title={row.state === "idle" ? `${row.email}: last numbers; this login is idle until it runs again` : row.email}>
              <span class="text">{row.name}</span>{#if row.inUse}<span class="dot" aria-label="in use"></span>{/if}{#if row.state === "idle"}<span class="note">idle</span>{/if}
            </span>
            {#if NOTE[row.state]}
              <span class="wide">{NOTE[row.state]}</span>
            {:else}
              {#each row.parts as part, at (at)}
                <span class="part">
                  {#if part}
                    <span class="bar"><span class="fill {part.level}" style:width="{part.left}%"></span></span>
                    <span class="pct {part.level}"><span class="room" aria-hidden="true">100%</span><span>{part.left}%</span></span>
                  {:else}
                    <span class="pct none">–</span>
                  {/if}
                </span>
              {/each}
            {/if}
          </div>
        {/each}
      </div>
    {/each}
  </section>
{/if}

<style>
  .accounts {
    margin-bottom: calc(22 * var(--px));
  }
  .head,
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) repeat(var(--spans), calc(78 * var(--px)));
    column-gap: calc(10 * var(--px));
    align-items: center;
  }
  .head {
    min-height: calc(24 * var(--px));
  }
  .label,
  .col {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .col {
    color: var(--slate);
  }
  .row {
    min-height: calc(24 * var(--px));
    font: var(--t-small);
    font-variant-numeric: tabular-nums;
  }
  .name {
    display: flex;
    align-items: center;
    gap: calc(6 * var(--px));
    min-width: 0;
  }
  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dot {
    flex: none;
    width: calc(6 * var(--px));
    height: calc(6 * var(--px));
    border-radius: 50%;
    background: var(--vermilion);
  }
  .note,
  .wide {
    color: var(--slate);
    white-space: nowrap;
  }
  .wide {
    grid-column: 2 / -1;
  }
  .part {
    display: flex;
    align-items: center;
    gap: calc(6 * var(--px));
    min-width: 0;
  }
  .bar {
    position: relative;
    flex: 1;
    box-sizing: border-box;
    height: calc(7 * var(--px));
    box-shadow: inset 0 0 0 1px var(--meter-edge);
    background: var(--meter-track);
  }
  .fill {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    background: var(--meter);
    transition: width 400ms var(--ease-paper);
  }
  .fill.low {
    background: var(--amber);
  }
  .fill.out {
    background: var(--error-ink);
  }
  /* Always as wide as "100%", so every bar is the same width whatever the
     number (owner, 2026-10-25): the hidden "100%" holds the room. */
  .pct {
    flex: none;
    display: inline-grid;
    justify-items: end;
  }
  .pct > * {
    grid-area: 1 / 1;
  }
  .room {
    visibility: hidden;
  }
  .pct.low {
    color: var(--amber-ink);
  }
  .pct.out {
    color: var(--error-ink);
  }
  .pct.none {
    color: var(--slate);
  }
  /* An idle login's numbers are old: drawn quiet. */
  .idle .pct {
    color: var(--slate);
  }
  .idle .fill {
    background: var(--rule-control);
  }
</style>
