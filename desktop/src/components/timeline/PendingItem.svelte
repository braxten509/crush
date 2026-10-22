<script>
  // A prompt on its way (TYPE-042, TYPE-048; desktop-design 4.4): your box
  // with "sending…" in place of the time. The saved message takes over its
  // key, so it swaps in place without a jump. When the send's answer was lost
  // (a transport error), the box stays with "This message may not have been
  // sent" under it, in the warning colors of 4.11.
  // Placed by Timeline.
  import Icon from "../Icon.svelte";
  import Byline from "./Byline.svelte";
  import UserBox from "./UserBox.svelte";

  let { item } = $props();

  const uncertain = $derived(item.state === "uncertain");
</script>

<div class="pending">
  <Byline author="You" note={uncertain ? "" : "sending…"} end />
  <UserBox text={item.text} chips={item.chips} />
  {#if uncertain}
    <p class="warn"><Icon name="warning" size={16} /><span>This message may not have been sent</span></p>
  {/if}
</div>

<style>
  /* On the right, as your saved messages (UserItem). */
  .pending {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
  }
  .warn {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    margin-top: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--amber-ink);
  }
</style>
