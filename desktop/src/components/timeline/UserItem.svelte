<script>
  // Your message (CHAT-002 to CHAT-007): the byline (YOU and the time it was
  // sent), then your text and its chips in the picture's box, both on the
  // right (UserBox). Placed by Timeline.
  import { clockTime } from "../../lib/timefmt.js";
  import Byline from "./Byline.svelte";
  import UserBox from "./UserBox.svelte";

  let { item } = $props();
</script>

<div class="user">
  <Byline author="You" time={clockTime(item.time ?? 0)} />
  <UserBox text={item.text} chips={item.chips} messageId={item.messageId} />
</div>

<style>
  .user {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
  }
</style>
