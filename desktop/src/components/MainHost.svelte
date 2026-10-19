<script>
  // The middle column's other view (desktop-design-5-6 6.4; scaffold S5,
  // complete): mainView.current, a helper's chat (LIVE) or a terminal tab
  // (TERM), drawn in the conversation's place. App keeps the conversation
  // mounted underneath, hidden, while it shows.
  import { mainView } from "../lib/mainview.js";

  const view = $derived(mainView.current);
</script>

{#if view}
  <div class="main-view" data-main-view={view.kind}>
    {#key view.key ?? view.kind}
      <view.component {...view.props} />
    {/key}
  </div>
{/if}

<style>
  .main-view {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .main-view > :global(*) {
    flex: 1;
  }
</style>
