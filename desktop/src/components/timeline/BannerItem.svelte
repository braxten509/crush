<script>
  // How a step ended (CHAT-025, CHAT-027 to CHAT-029; desktop-design 4.4).
  //   error    a calc(2 * var(--px)) --error-ink rule on the left; "ERROR" in
  //            --t-label --error-ink, the title in --t-body-small ink on the
  //            same line, cut with "…"; the details wrap below in --t-small
  //            slate, never cut (assistant_render.go:233-252)
  //   refused  the same with "REFUSED", rule and tag in --amber-ink (a warning
  //            color, 4.11), the provider's text or the terminal's defaults
  //   canceled "Canceled" in italic --t-small slate, no rule
  //   compacted, compaction_canceled
  //            "Conversation compacted" / "Compaction canceled" in --t-small
  //            slate (the terminal's muted footer title, assistant.go:228-242)
  // Placed by Timeline.
  let { item } = $props();

  const alert = $derived(item.banner === "error" || item.banner === "refused");
</script>

{#if alert}
  <div class="alert {item.banner}" data-banner={item.banner}>
    <p class="head"><span class="tag">{item.banner === "refused" ? "REFUSED" : "ERROR"}</span><span class="title">{item.title}</span></p>
    {#if item.details}<p class="details">{item.details}</p>{/if}
  </div>
{:else}
  <p class="line {item.banner}" data-banner={item.banner}>{item.title}</p>
{/if}

<style>
  .alert {
    min-width: 0;
    padding-left: calc(14 * var(--px));
    border-left: calc(2 * var(--px)) solid var(--error-ink);
  }
  .alert.refused {
    border-left-color: var(--amber-ink);
  }
  .head {
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    min-width: 0;
  }
  .tag {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    color: var(--error-ink);
  }
  .refused .tag {
    color: var(--amber-ink);
  }
  .title {
    min-width: 0;
    font: var(--t-body-small);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .details {
    margin-top: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .line {
    font: var(--t-small);
    color: var(--slate);
  }
  .line.canceled {
    font-style: italic;
  }
</style>
