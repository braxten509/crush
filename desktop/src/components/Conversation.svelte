<script>
  // The middle column (desktop-design 2.1): the chat's head, the timeline on
  // its rail (or a start screen in its place), the notice line over the
  // timeline's bottom, the tray, the composer, and the meter strip while the
  // right panel is hidden. Each part is drawn by its own piece; this shell
  // only places them.
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
  import { sessions } from "../lib/stores/sessions.svelte.js";
  import { ui } from "../lib/stores/ui.svelte.js";

  const sessionId = $derived(sessions.currentId);
  const session = $derived(sessions.byId[sessions.currentId] ?? null);
</script>

<main class="conversation" aria-label={session?.title || "Conversation"}>
  <ChatHead {session} />

  <Rule marks="both" class="under-head" />

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
  {#if ui.panelHidden}<MeterStrip />{/if}
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
