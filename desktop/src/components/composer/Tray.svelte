<script>
  // The tray above the composer rule (Q7; 4.9; SIDE-031 to SIDE-034,
  // TYPE-044, TYPE-045, TYPE-050): the visible sections of lib/tray.js in
  // order under a faint rule, only while a chat is open and nothing holds
  // the slot. It is at most 40% of the timeline's height, then it scrolls.
  // The to-dos open by themselves once per chat in a tall column (SIDE-032).
  import { focusTray, traySections, traySelected, trayShown } from "../../lib/tray.js";
  import { autoOpen } from "../../lib/stores/todos.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { ui } from "../../lib/stores/ui.svelte.js";

  const SHARE = 0.4; // of the timeline's height

  let root = $state(null);
  let limit = $state(0);
  let columnHeight = $state(0); // picture px

  const shown = $derived(trayShown());

  // The space between the head's rule and the composer's rule holds the
  // timeline and the tray: tray ≤ 0.4 × timeline means tray ≤ 0.4/1.4 of it.
  $effect(() => {
    if (!root) return;
    const column = root.closest(".conversation");
    if (!column) return;
    const head = column.querySelector(".rule.under-head");
    const foot = column.querySelector(".rule.above-composer");
    const measure = () => {
      const unit = (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16) / 16;
      columnHeight = column.getBoundingClientRect().height / unit;
      if (head && foot) limit = Math.max(0, ((foot.getBoundingClientRect().top - head.getBoundingClientRect().bottom) * SHARE) / (1 + SHARE));
    };
    const observer = new ResizeObserver(measure);
    observer.observe(column);
    const composerBox = column.querySelector('[data-probe="composer"]');
    if (composerBox) observer.observe(composerBox);
    measure();
    return () => observer.disconnect();
  });

  $effect(() => {
    void sessions.currentId;
    if (shown && columnHeight > 0) autoOpen(columnHeight);
  });

  function enter(event) {
    if (ui.focus !== "tray" && !event.target.closest("button")) focusTray();
  }
</script>

{#if shown}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <section class="tray" data-probe="tray" aria-label="Tray" bind:this={root} style:max-height={limit ? `${limit}px` : null} onpointerdown={enter}>
    {#each traySections as section (section.id)}
      <div class="section" class:selected={ui.focus === "tray" && traySelected() === section.id} data-section={section.id}>
        <section.component />
      </div>
    {/each}
  </section>
{:else}
  <span class="anchor" bind:this={root} aria-hidden="true"></span>
{/if}

<style>
  .tray {
    position: relative;
    z-index: 1;
    flex: none;
    min-height: 0;
    overflow-y: auto;
    border-top: 1px solid var(--rule-faint);
    background: var(--paper);
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .tray::-webkit-scrollbar {
    width: calc(6 * var(--px));
  }
  .tray::-webkit-scrollbar-thumb {
    background: var(--rule-control);
  }
  .tray::-webkit-scrollbar-track {
    background: transparent;
  }
  .section {
    padding: calc(6 * var(--px)) calc(20 * var(--px));
  }
  .section + .section {
    border-top: 1px solid var(--rule-faint);
  }
  /* The section ← and → act on, while the tray has the keys (SIDE-033). */
  .section.selected {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(-1 * var(--px));
  }
  .anchor {
    display: none;
  }
</style>
