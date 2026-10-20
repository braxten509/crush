<script module>
  // Each notice kind's icon (SIDE-028; the terminal's indicator per InfoType,
  // internal/ui/model/status.go:182-205, as drawn line icons). Errors and
  // warnings carry their colours (4.11); the rest are ink.
  export const NOTICE_ICON = {
    info: "info",
    success: "check",
    warn: "warning",
    error: "cross",
    update: "update",
    plan: "diamond",
    yolo: "half-circle",
  };
</script>

<script>
  // The notice line (SIDE-028, SIDE-030, 4.14): one line laid over the
  // bottom of the timeline, sitting on the composer rule, so nothing moves.
  // A newer notice replaces the older one and each clears itself after its
  // time (ui.notify); a notice with an action has a text button at the right
  // ("Try again", SIDE-030). The text is one line, cut with "…" (the
  // terminal joins a notice's lines with spaces, status.go:220). Placed by
  // Conversation, between the timeline and the tray.
  // inline: drawn inside the front sheet (kit Sheet), in the flow above its
  // help line. The page's line steps aside while a sheet is open, so a
  // message shows where the person is working (owner, 2026-10-20).
  import { dialogs } from "../lib/dialogs.js";
  import { clearNotice, ui } from "../lib/stores/ui.svelte.js";
  import Icon from "./Icon.svelte";

  let { inline = false } = $props();

  const notice = $derived(inline || dialogs.length === 0 ? ui.notice : null);

  function act() {
    const action = notice?.action;
    if (action?.run) Promise.resolve(action.run()).catch((error) => console.error("notice action failed", error));
  }
</script>

<div class="notice-anchor" class:inline data-notice-anchor>
  {#if notice}
    <div class="notice {notice.kind}" role="status" aria-live="polite" data-notice-kind={notice.kind}>
      <span class="mark"><Icon name={NOTICE_ICON[notice.kind] ?? "info"} size={16} /></span>
      <span class="text">{notice.text}</span>
      {#if notice.action?.label}
        <button type="button" class="action" data-notice-action onclick={act}>{notice.action.label}</button>
      {/if}
      {#if notice.sticky}
        <button type="button" class="dismiss" aria-label="Dismiss" title="Dismiss" data-notice-dismiss onclick={() => clearNotice()}><Icon name="cross" size={12} /></button>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* Takes no room: the line hangs from the timeline's bottom edge. */
  .notice-anchor {
    position: relative;
    flex: none;
    height: 0;
    z-index: 2;
  }
  .notice {
    animation: paper-settle var(--motion) var(--ease-paper);
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    box-sizing: border-box;
    height: calc(40 * var(--px));
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    padding: 0 calc(23 * var(--px)) 0 calc(66 * var(--px));
    background: var(--paper-raised);
    border-top: 1px solid var(--rule-faint);
    color: var(--ink);
  }
  .mark {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
  }
  .text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-small);
  }
  .error {
    color: var(--error-ink);
  }
  /* Inside a sheet: in the flow, on the sheet's gutter. */
  .inline {
    height: auto;
  }
  .inline .notice {
    position: static;
    padding: 0 calc(26 * var(--px));
    background: none;
    animation: none;
  }
  /* The close cross: small to see, 24 real pixels to hit. */
  .dismiss {
    flex: none;
    position: relative;
    display: grid;
    place-items: center;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    color: var(--slate);
  }
  .dismiss::after {
    content: "";
    position: absolute;
    left: 50%;
    top: 50%;
    width: max(24px, 100%);
    height: max(24px, 100%);
    transform: translate(-50%, -50%);
  }
  .dismiss:hover {
    color: var(--ink);
  }
  .warn .text {
    color: var(--amber-ink);
  }
  .action {
    flex: none;
    font: var(--t-small);
    color: var(--link);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
</style>
