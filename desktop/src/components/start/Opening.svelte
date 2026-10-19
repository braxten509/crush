<script>
  // LIST-034: while a chat opens (sessions.opening), the line "Opening
  // <title>…" (or "Opening conversation…") with the activity mark, and the
  // seconds once a second has passed, in place of the chat (the terminal's
  // sessionLoadingView, internal/ui/model/session_load.go:580-594, which also
  // replaces the transcript). The seconds take the status line's look (4.10).
  // A newer switch replaces the line; sessions.openChat decides which wins.
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import ActivityDots from "./ActivityDots.svelte";
  import StartLayer from "./StartLayer.svelte";
  import { screen } from "./start.svelte.js";
  import { openingElapsed, openingText } from "./startmodel.js";
  import { shown } from "./shown.js";

  const active = $derived(screen() === "opening");
  const label = $derived(openingText(sessions.opening?.title));

  let now = $state(Date.now());
  $effect(() => {
    if (!active) return;
    now = Date.now();
    const timer = setInterval(() => (now = Date.now()), 250);
    return () => clearInterval(timer);
  });
  const elapsed = $derived(sessions.opening ? openingElapsed(sessions.opening.startedAt, now) : "");

  $effect(() => {
    shown.opening = active ? { id: sessions.opening?.id ?? "", label, elapsed } : null;
    return () => {
      shown.opening = null;
    };
  });
</script>

{#if active}
  <StartLayer kind="opening" label="Opening">
    <p class="line" role="status">
      <ActivityDots />
      <span class="text">
        <span class="label">{label}</span>
        {#if elapsed}<span class="elapsed">&nbsp;· {elapsed}</span>{/if}
      </span>
    </p>
  </StartLayer>
{/if}

<style>
  /* Where the chat's first item would be: the timeline's padding (24 down,
     the text 66 in, Timeline.svelte). */
  .line {
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    min-width: 0;
    padding: calc(24 * var(--px)) calc(23 * var(--px)) 0 calc(66 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .text {
    display: flex;
    align-items: baseline;
    min-width: 0;
  }
  .label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .elapsed {
    flex: none;
    font: var(--t-stamp);
  }
</style>
