<script>
  // Settings > Fallback order (owner, 2026-10-21; look A, a stacked list):
  // the order chats move through when an account runs out. An on/off switch,
  // then the blocks top to bottom with the way the chat leaves each under it,
  // then "+ Add block". A block opens to its options when clicked; its handle
  // drags it (Alt+Up/Down move it from the keyboard); × removes it. Every
  // change is saved at once (lib/fallback/store.svelte.js).
  import { onMount } from "svelte";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { fallback, loadFallback, saveOrder } from "../../lib/fallback/store.svelte.js";
  import { accountsOf, blockKind, blockText, changeBlock, CODEX, hasAccounts, leaveText, moveBlock, newBlock } from "../../lib/fallback/blocks.js";

  // The model choices: the agent CLIs (accounts live there), others after.
  const providers = $derived((Array.isArray(ws.providers) ? ws.providers : []).filter((p) => p?.id && Array.isArray(p.models) && p.models.length > 0));
  const cliProviders = $derived(providers.filter((p) => /cli$|^claude-code$|^codex$/u.test(p.type ?? "")));
  const ctx = $derived({ accounts: fallback.accounts, providers });

  let open = $state(-1); // the block showing its options
  let adding = $state(false);
  let drag = $state(null); // {from, to}

  const blocks = $derived(drag ? moveBlock(fallback.order.blocks, drag.from, drag.to) : fallback.order.blocks);

  onMount(() => {
    loadFallback();
  });

  function save(nextBlocks, on = fallback.order.on) {
    saveOrder({ on, blocks: nextBlocks });
  }

  function change(i, fields) {
    const next = [...fallback.order.blocks];
    next[i] = changeBlock(next[i], fields);
    save(next);
  }

  function add(type) {
    adding = false;
    const next = [...fallback.order.blocks, newBlock(type, { providers: cliProviders.length ? cliProviders : providers })];
    open = next.length - 1;
    save(next);
  }

  function remove(i) {
    if (open === i) open = -1;
    save(fallback.order.blocks.filter((_, n) => n !== i));
  }

  function move(i, to) {
    if (to < 0 || to >= fallback.order.blocks.length) return;
    if (open === i) open = to;
    save(moveBlock(fallback.order.blocks, i, to));
  }

  // The handle: the block follows the pointer over the others' middles.
  let list = $state(null);
  function grab(event, i) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag = { from: i, to: i };
  }
  function pull(event) {
    if (!drag || !list) return;
    const rows = [...list.querySelectorAll("[data-block]")];
    let to = drag.to;
    rows.forEach((row, n) => {
      const r = row.getBoundingClientRect();
      if (event.clientY >= r.top && event.clientY < r.bottom) to = n;
    });
    if (rows.length && event.clientY < rows[0].getBoundingClientRect().top) to = 0;
    if (rows.length && event.clientY >= rows[rows.length - 1].getBoundingClientRect().bottom) to = rows.length - 1;
    if (to !== drag.to) drag = { ...drag, to };
  }
  function drop() {
    if (!drag) return;
    const { from, to } = drag;
    drag = null;
    if (from !== to) move(from, to);
  }
  function handleKey(event, i) {
    if (event.altKey && event.key === "ArrowUp") {
      event.preventDefault();
      move(i, i - 1);
    } else if (event.altKey && event.key === "ArrowDown") {
      event.preventDefault();
      move(i, i + 1);
    }
  }

  const providerOf = (block) => providers.find((p) => p.id === block.provider);
  const effortsOf = (block) => providerOf(block)?.models?.find((m) => m.id === block.model)?.reasoning_levels ?? [];
  const numberField = (event) => Math.max(0, Math.round(Number(event.currentTarget.value) || 0));
</script>

<div class="fallback" data-probe="fallback-order">
  <div class="row head">
    <span class="name">Use the fallback order</span>
    <button type="button" class="switch" role="switch" aria-checked={fallback.order.on} aria-label="Use the fallback order" onclick={() => save(fallback.order.blocks, !fallback.order.on)}></button>
  </div>
  <p class="quiet">When a chat's account runs out, it moves down this list. Chats on a model that isn't in the list stay where they are.</p>
  {#if fallback.error}<p class="error" role="alert">{fallback.error}</p>{/if}

  <ol class="blocks" bind:this={list} class:off={!fallback.order.on} aria-label="Fallback order">
    {#each blocks as block, i (block)}
      {@const kind = blockKind(block, providers)}
      {@const real = drag ? (i === drag.to ? drag.from : i) : i}
      <li class="item" data-block={i}>
        <div class="block" class:wait={block.type === "wait"} class:open={open === real} class:dragging={drag && i === drag.to}>
          <button type="button" class="handle" aria-label="Move block {i + 1}" title="Drag to move (Alt+Up/Down)" onpointerdown={(e) => grab(e, i)} onpointermove={pull} onpointerup={drop} onpointercancel={drop} onkeydown={(e) => handleKey(e, i)}>⠿</button>
          <button type="button" class="summary" aria-expanded={open === real} onclick={() => (open = open === real ? -1 : real)}>
            <span class="number">{i + 1}</span>
            <span class="text">{blockText(block, ctx)}</span>
          </button>
          <button type="button" class="remove" aria-label="Remove block {i + 1}" onclick={() => remove(real)}>×</button>
        </div>
        {#if open === real && !drag}
          <div class="options" data-probe="block-options">
            {#if block.type === "use"}
              <div class="opt">
                <span class="label">Tool</span>
                <span class="choices">
                  {#each cliProviders.length ? cliProviders : providers as p (p.id)}
                    <button type="button" class="choice" aria-pressed={block.provider === p.id} onclick={() => change(real, { provider: p.id, model: p.models[0]?.id ?? "", account: "", effort: "" })}>{p.name}</button>
                  {/each}
                </span>
              </div>
              {#if hasAccounts(kind)}
                <div class="opt">
                  <span class="label">Account</span>
                  <span class="choices">
                    {#each accountsOf(fallback.accounts, kind) as a (a.id)}
                      <button type="button" class="choice" aria-pressed={(block.account ?? "") === a.id} onclick={() => change(real, { account: a.id })}>{a.name}</button>
                    {/each}
                  </span>
                </div>
              {/if}
              <div class="opt">
                <span class="label">Model</span>
                <span class="choices">
                  {#each providerOf(block)?.models ?? [] as m (m.id)}
                    <button type="button" class="choice" aria-pressed={block.model === m.id} onclick={() => change(real, { model: m.id, effort: "" })}>{m.name || m.id}</button>
                  {/each}
                </span>
              </div>
              {#if effortsOf(block).length}
                <div class="opt">
                  <span class="label">Effort</span>
                  <span class="choices">
                    <button type="button" class="choice" aria-pressed={!block.effort} onclick={() => change(real, { effort: "" })}>Default</button>
                    {#each effortsOf(block) as level (level)}
                      <button type="button" class="choice" aria-pressed={block.effort === level} onclick={() => change(real, { effort: level })}>{level}</button>
                    {/each}
                  </span>
                </div>
              {/if}
              <div class="opt">
                <span class="label">Move on</span>
                <span class="choices">
                  <button type="button" class="choice" aria-pressed={!(Number(block.move_at_percent) > 0)} onclick={() => change(real, { move_at_percent: 0 })}>When it runs out</button>
                  <button type="button" class="choice" aria-pressed={Number(block.move_at_percent) > 0} onclick={() => change(real, { move_at_percent: Number(block.move_at_percent) > 0 ? block.move_at_percent : 95 })}>At</button>
                  <input type="number" min="1" max="100" class="small" aria-label="Move on at percent used" value={Number(block.move_at_percent) > 0 ? block.move_at_percent : 95} onchange={(e) => change(real, { move_at_percent: Math.min(100, numberField(e)) })} />
                  <span class="unit">% used</span>
                </span>
              </div>
              {#if kind === CODEX}
                <label class="check"><input type="checkbox" checked={Boolean(block.refill_first)} onchange={(e) => change(real, { refill_first: e.currentTarget.checked })} /> Use a free refill before moving on</label>
              {/if}
              <label class="check"><input type="checkbox" checked={Boolean(block.come_back)} onchange={(e) => change(real, { come_back: e.currentTarget.checked })} /> Come back here when it refills</label>
            {:else}
              <div class="opt">
                <span class="label">Account</span>
                <span class="choices">
                  {#each fallback.accounts.filter((a) => block.type !== "refill" || a.kind === CODEX) as a (`${a.kind}/${a.id}`)}
                    <button type="button" class="choice" aria-pressed={block.kind === a.kind && (block.account ?? "") === a.id} onclick={() => change(real, { kind: a.kind, account: a.id })}>{a.name}</button>
                  {/each}
                </span>
              </div>
              {#if block.type === "wait"}
                <div class="opt">
                  <span class="label">At most</span>
                  <span class="choices">
                    <button type="button" class="choice" aria-pressed={!(Number(block.max_wait_minutes) > 0)} onclick={() => change(real, { max_wait_minutes: 0 })}>No limit</button>
                    <button type="button" class="choice" aria-pressed={Number(block.max_wait_minutes) > 0} onclick={() => change(real, { max_wait_minutes: Number(block.max_wait_minutes) > 0 ? block.max_wait_minutes : 60 })}>Up to</button>
                    <input type="number" min="1" class="small" aria-label="Wait at most minutes" value={Number(block.max_wait_minutes) > 0 ? block.max_wait_minutes : 60} onchange={(e) => change(real, { max_wait_minutes: numberField(e) })} />
                    <span class="unit">minutes, else skip ahead</span>
                  </span>
                </div>
                <div class="opt">
                  <span class="label">While waiting</span>
                  <span class="choices">
                    <button type="button" class="choice" aria-pressed={block.meanwhile !== "next"} onclick={() => change(real, { meanwhile: "" })}>Just wait</button>
                    <button type="button" class="choice" aria-pressed={block.meanwhile === "next"} onclick={() => change(real, { meanwhile: "next" })}>Use the next blocks, come back after</button>
                  </span>
                </div>
              {/if}
            {/if}
          </div>
        {/if}
        {#if i < blocks.length - 1}
          <div class="arrow" aria-hidden="true">↓ {leaveText(block) || "then"}</div>
        {:else if leaveText(block) && leaveText(block) !== leaveText({ type: "use" })}
          <!-- The last block has nowhere to go, but its own settings still show. -->
          <div class="arrow" aria-hidden="true">{leaveText(block)}</div>
        {/if}
      </li>
    {/each}
  </ol>

  <div class="adder">
    {#if adding}
      <span class="choices" role="group" aria-label="Add a block">
        <button type="button" class="choice" onclick={() => add("use")}>Use an account</button>
        <button type="button" class="choice" onclick={() => add("wait")}>Wait for a refill</button>
        <button type="button" class="choice" onclick={() => add("refill")}>Use a free refill</button>
        <button type="button" class="choice" onclick={() => (adding = false)}>Cancel</button>
      </span>
    {:else}
      <button type="button" class="add" onclick={() => (adding = true)}>+ Add block</button>
    {/if}
  </div>
</div>

<style>
  .fallback {
    max-width: calc(840 * var(--px));
  }
  .row {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    min-height: calc(56 * var(--px));
    border-top: 1px solid var(--rule-control);
    border-bottom: 1px solid var(--rule-control);
  }
  .name {
    flex: 1;
    font: var(--t-item);
    color: var(--ink);
  }
  .quiet {
    margin: calc(12 * var(--px)) 0 calc(16 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .error {
    margin: 0 0 calc(12 * var(--px));
    font: var(--t-small);
    color: var(--rose, var(--vermilion));
  }
  .blocks {
    list-style: none;
    margin: 0;
    padding: 0;
    width: calc(560 * var(--px));
    max-width: 100%;
  }
  .blocks.off {
    opacity: 0.6;
  }
  .block {
    display: flex;
    align-items: center;
    min-height: calc(44 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
  }
  .block.wait {
    border-style: dashed;
  }
  .block.open {
    border-color: var(--ink);
  }
  .block.dragging {
    background: var(--band);
  }
  .handle,
  .remove {
    flex: none;
    width: calc(36 * var(--px));
    align-self: stretch;
    border: 0;
    background: none;
    font: var(--t-item);
    color: var(--slate);
    cursor: grab;
    touch-action: none;
  }
  .remove {
    cursor: pointer;
  }
  .handle:hover,
  .remove:hover {
    color: var(--ink);
  }
  .summary {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    align-self: stretch;
    padding: 0 calc(4 * var(--px));
    border: 0;
    background: none;
    text-align: left;
    font: var(--t-item);
    color: var(--ink);
    cursor: pointer;
    align-items: center;
  }
  .number {
    font: var(--t-small);
    color: var(--slate);
  }
  .text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .options {
    display: flex;
    flex-direction: column;
    gap: calc(10 * var(--px));
    padding: calc(12 * var(--px)) calc(14 * var(--px)) calc(14 * var(--px)) calc(36 * var(--px));
    border: 1px solid var(--ink);
    border-top: 0;
    background: var(--paper);
  }
  .opt {
    display: flex;
    align-items: flex-start;
    gap: calc(12 * var(--px));
  }
  .label {
    flex: none;
    width: calc(96 * var(--px));
    padding-top: calc(7 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .choices {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: calc(6 * var(--px));
  }
  .choice,
  .add {
    height: calc(32 * var(--px));
    padding: 0 calc(12 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .choice:hover,
  .add:hover {
    background: var(--control-hover);
  }
  .choice[aria-pressed="true"] {
    background: var(--band);
    border-color: var(--ink);
  }
  .add {
    border-style: dashed;
    color: var(--slate);
  }
  .small {
    width: calc(64 * var(--px));
    height: calc(32 * var(--px));
    padding: 0 calc(8 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper);
    font: var(--t-field);
    color: var(--ink);
  }
  .unit {
    font: var(--t-small);
    color: var(--slate);
  }
  .check {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    padding-left: calc(108 * var(--px));
    font: var(--t-small);
    color: var(--ink);
  }
  .arrow {
    padding: calc(4 * var(--px)) 0 calc(4 * var(--px)) calc(44 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .adder {
    margin-top: calc(10 * var(--px));
    width: calc(560 * var(--px));
    max-width: 100%;
  }
  .adder .add {
    width: 100%;
    text-align: left;
  }
  .switch {
    position: relative;
    flex: none;
    width: calc(44 * var(--px));
    height: calc(24 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    cursor: pointer;
  }
  .switch::after {
    content: "";
    position: absolute;
    top: calc(3 * var(--px));
    left: calc(3 * var(--px));
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    background: var(--rule-control);
  }
  .switch[aria-checked="true"] {
    border-color: var(--ink);
  }
  .switch[aria-checked="true"]::after {
    left: calc(23 * var(--px));
    background: var(--choice);
  }
  button:focus-visible,
  input:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
