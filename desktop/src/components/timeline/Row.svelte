<script>
  // One timeline row: the item's frame (key, focus) around its
  // kind's component (desktop-design 1.5.6). The virtual list mounts one Row
  // per rendered item and keeps it while the item is in its cache, so
  // scrolling back never draws a reply again (1.6 item 3).
  // Placed by VirtualList.
  import ShellItem from "../tools/ShellItem.svelte";
  import ToolGroup from "../tools/ToolGroup.svelte";
  import ToolItem from "../tools/ToolItem.svelte";
  import BannerItem from "./BannerItem.svelte";
  import FooterItem from "./FooterItem.svelte";
  import ItemFrame from "./ItemFrame.svelte";
  import NoticeItem from "./NoticeItem.svelte";
  import PendingItem from "./PendingItem.svelte";
  import ReplyItem from "./ReplyItem.svelte";
  import StatusLine from "./StatusLine.svelte";
  import UserItem from "./UserItem.svelte";

  let { item, selected = false, focused = false } = $props();
</script>

<ItemFrame {item} {selected} {focused}>
  {#if item.kind === "user"}
    <UserItem {item} />
  {:else if item.kind === "pending"}
    <PendingItem {item} />
  {:else if item.kind === "reply"}
    <ReplyItem {item} />
  {:else if item.kind === "notice"}
    <NoticeItem {item} />
  {:else if item.kind === "banner"}
    <BannerItem {item} />
  {:else if item.kind === "footer"}
    <FooterItem {item} />
  {:else if item.kind === "status"}
    <StatusLine {item} />
  {:else if item.kind === "tools"}
    <ToolGroup {item} />
  {:else if item.kind === "tool"}
    <ToolItem {item} />
  {:else if item.kind === "shell"}
    <ShellItem {item} />
  {/if}
</ItemFrame>
