<script>
  // The landing page (START-003, CHAT-001): blank by the owner's call
  // (2026-10-19). A new chat shows nothing in the timeline's place until the
  // first message; the composer stays below and the panel stays.
  import StartLayer from "./StartLayer.svelte";
  import { screen } from "./start.svelte.js";
  import { shown } from "./shown.js";

  const active = $derived(screen() === "landing");

  $effect(() => {
    shown.landing = active ? { blank: true } : null;
    return () => {
      shown.landing = null;
    };
  });
</script>

{#if active}
  <StartLayer kind="landing" label="Start" />
{/if}
