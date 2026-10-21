<script>
  // The middle column (desktop-design 2.1): the chat's head, the timeline on
  // its rail (or a start screen in its place), the notice line over the
  // timeline's bottom, the tray, the composer, and the strip under it while
  // the right panel is hidden (the meters) or the phone shares the shown chat
  // (REMOTE). Each part is drawn by its own piece; this shell only places
  // them.
  import ChatHead from "./ChatHead.svelte";
  import Composer from "./composer/Composer.svelte";
  import Tray from "./composer/Tray.svelte";
  import NoticeLine from "./NoticeLine.svelte";
  import MeterStrip from "./panel/MeterStrip.svelte";
  import Rule from "./Rule.svelte";
  import InitPrompt from "./start/InitPrompt.svelte";
  import Landing from "./start/Landing.svelte";
  import Onboarding from "./start/Onboarding.svelte";
  import Opening from "./start/Opening.svelte";
  import Timeline from "./timeline/Timeline.svelte";
  import { shownShare } from "../lib/remote/store.svelte.js";
  import { sessions } from "../lib/stores/sessions.svelte.js";
  import { ui } from "../lib/stores/ui.svelte.js";
  import { screen } from "./start/start.svelte.js";

  const sessionId = $derived(sessions.currentId);
  const remote = $derived(Boolean(shownShare()));
  const session = $derived(sessions.byId[sessions.currentId] ?? null);
  // The head and its rule show only with something in them: a chat, a chat
  // opening, or the wordmark screens. The blank start screen has no empty
  // bar at the top (owner, 2026-10-19).
  const headed = $derived(["chat", "opening", "init", "onboarding"].includes(screen()));
  // A chat has no head of its own any more (its title, folder and branch are
  // in the top bar; owner, 2026-10-20): only the wordmark screens draw one.
  const branded = $derived(["init", "onboarding"].includes(screen()));
</script>

<main class="conversation" aria-label={session?.title || "Conversation"}>
  {#if headed}
    <ChatHead {session} />
    {#if branded}<Rule marks="both" class="under-head" />{/if}
  {/if}

  <!-- The start screens show themselves in the timeline's place when they apply (P3.9). -->
  <Onboarding />
  <InitPrompt />
  <Landing />
  <Opening />
  <Timeline {sessionId} />

  <NoticeLine />
  <Tray />

  <Rule marks="both" class="above-composer" />

  <Composer {sessionId} />
  {#if ui.panelHidden || remote}<MeterStrip meters={ui.panelHidden} {remote} />{/if}
</main>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
  .conversation {
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  .conversation :global(.rule.under-head),
  .conversation :global(.rule.above-composer) {
    flex: none;
    z-index: 1;
  }
</style>
