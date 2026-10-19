<script>
  // The engine gate (desktop-design 2.9, 4.12). Until the window has joined
  // the project's engine once, the middle column shows this instead of the
  // conversation: nothing for the join's first 400 ms, then "Starting Crush…"
  // with the activity mark, or what went wrong (the error's own message) with
  // one "Try again", which joins again (connection.svelte.js retry: before the
  // first join it is engine_open again). Once joined, the conversation stays
  // through any later reconnect; SIDE-030's notices speak for it then.
  import { probeSection } from "../../lib/probe.js";
  import { conn, retry } from "../../lib/stores/connection.svelte.js";
  import ActivityDots from "./ActivityDots.svelte";
  import { gateMarks } from "./marks.js";
  import { watchStart } from "./start.svelte.js";
  import { gateView, STARTING, TRY_AGAIN } from "./startmodel.js";

  let { children } = $props();

  watchStart();

  const view = $derived(gateView(conn));
  let button = $state(null);
  let trying = $state(false);

  async function tryAgain() {
    if (trying) return;
    trying = true;
    try {
      await retry();
    } finally {
      trying = false;
    }
  }

  function rectOf(el) {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  // 1.9: the gate as drawn; starting is true exactly while "Starting Crush…" shows.
  probeSection(
    "gate",
    () => {
      const v = gateView(conn);
      return {
        state: conn.state,
        shown: v !== null,
        title: v?.title ?? "",
        body: v?.body ?? "",
        fix: v?.fix ?? "",
        starting: v?.kind === "starting",
        button: v?.kind === "failed" ? { label: TRY_AGAIN } : null,
      };
    },
    () => ({ button: button?.isConnected ? { rect: rectOf(button) } : null, align: gateMarks() }),
  );
</script>

{#if view === null}
  {@render children()}
{:else}
  <main class="gate" aria-label="Crush">
    {#if view.kind === "starting"}
      <p class="starting" role="status"><ActivityDots /><span class="label">{STARTING}</span></p>
    {:else if view.kind === "failed"}
      <div class="block" role="alert">
        <h2 class="title">{view.title}</h2>
        <p class="body">{view.body}</p>
        {#if view.fix}
          <p class="fix">{view.fix}</p>
        {/if}
        <button class="try" type="button" bind:this={button} onclick={tryAgain} disabled={trying}>
          <span class="label">{TRY_AGAIN}</span>
        </button>
      </div>
    {/if}
  </main>
{/if}

<style>
  .gate {
    display: flex;
    flex-direction: column;
    justify-content: center;
    min-width: 0;
    min-height: 0;
    padding: 0 calc(60 * var(--px));
    overflow: hidden;
  }

  .starting {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: calc(12 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }

  .block {
    box-sizing: border-box;
    width: 100%;
    max-width: calc(560 * var(--px));
    margin: 0 auto;
  }
  .title {
    font: var(--t-figure);
    color: var(--ink);
  }
  .body {
    margin-top: calc(14 * var(--px));
    font: var(--t-reading);
    color: var(--ink);
    overflow-wrap: anywhere;
  }
  .fix {
    margin-top: calc(10 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
  }

  /* The New chat button's look (ChatList.svelte). */
  .try {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    height: calc(52 * var(--px));
    margin-top: calc(28 * var(--px));
    padding: 0 calc(22 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    color: var(--ink);
  }
  .try .label {
    /* As on New chat: Spectral's capitals sit high; one picture pixel down
       centres them in the button (measured in the real window). */
    position: relative;
    top: calc(1 * var(--px));
    font: var(--t-button);
  }
  .try:hover .label {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .try:disabled {
    color: var(--slate);
  }
</style>
