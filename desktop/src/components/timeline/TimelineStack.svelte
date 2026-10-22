<script>
  // Every chat on the left stays drawn (owner, 2026-10-22: "all chats should
  // stay open with all drawing and functionality, even when they are not the
  // currently selected one"): one timeline per listed chat whose messages are
  // loaded, plus the shown chat. Only the shown one is seen; the others sit
  // under it, hidden and inert, keeping their scroll spot, their selection
  // and their drawn rows, and their events keep them current. Each slot is
  // keyed by its chat and never moves, so picking a chat only swaps which
  // slot is seen.
  import { messages } from "../../lib/stores/messages.svelte.js";
  import { openChats } from "../../lib/stores/openchats.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { stackedChats } from "../../lib/timelinestack.js";
  import Timeline from "./Timeline.svelte";

  let { sessionId } = $props();

  const ids = $derived(
    stackedChats(
      sessionId,
      openChats.list.map((c) => c.id),
      (id) => messages.bySession[id]?.status === "ready",
    ),
  );
</script>

<div class="timelines" data-timelines>
  {#each ids as sid (sid)}
    {@const active = sid === sessionId}
    <!-- While another chat opens, the opening screen takes the shown one's place. -->
    {@const seen = active && !sessions.opening}
    <div class="slot" class:parked={!seen} data-timeline-slot={sid} inert={seen ? undefined : true} aria-hidden={seen ? undefined : "true"}>
      <Timeline sessionId={sid} {active} />
    </div>
  {/each}
</div>

<style>
  /* Every slot fills the same box, so a chat coming back has the size it was
     drawn at; only visibility changes, which keeps each one's layout (and
     its scroll spot) as it was. */
  .timelines {
    position: relative;
    flex: 1;
    min-height: 0;
  }
  .slot {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
  }
  .slot.parked {
    visibility: hidden;
    pointer-events: none;
  }
</style>
