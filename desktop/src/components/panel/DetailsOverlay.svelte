<script>
  // The details overlay (SIDE-003, 4.14): while the right panel is hidden,
  // Ctrl+D drops the panel's facts from under the chat head over the
  // timeline, as the terminal's compact-mode details (internal/ui/model/
  // draw.go:331-380): a header line with the chat's title, the model info
  // (sidebar.go:25-86 and its "Usage left" limits) and the version at the
  // right, then Modified Files, LSPs, MCPs and Skills side by side, at most
  // 20 rows each; a ruled bottom edge. Typing in the field closes
  // it (keypress.go:401-405); Ctrl+D toggles it (lib/plugins/panel.js). It is
  // drawn only while the panel is hidden and a chat is shown (draw.go:155,
  // :333). Placed by OverlayHost.
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import { ui } from "../../lib/stores/ui.svelte.js";
  import Rule from "../Rule.svelte";
  import FilesSection from "./FilesSection.svelte";
  import LspSection from "./LspSection.svelte";
  import McpSection from "./McpSection.svelte";
  import { closeDetails, modelNow, panel, sectionRows } from "./panel.svelte.js";
  import { timelineBox, trackBox } from "./place.js";
  import SkillsSection from "./SkillsSection.svelte";

  // The terminal's maxItemsPerSection for its 20-row overlay (layout.go:22).
  const MAX_ROWS = 20;

  const open = $derived(panel.detailsOpen && ui.panelHidden && Boolean(sessions.currentId));
  const session = $derived(sessions.byId[sessions.currentId] ?? null);
  const lists = $derived(open ? sectionRows({ max: MAX_ROWS }) : null);

  // Limit readings age, so the "resets …" words follow the clock.
  let now = $state(Date.now());
  $effect(() => {
    if (!open) return;
    now = Date.now();
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });
  const info = $derived(open ? modelNow(now) : null);

  let box = $state(null);
  $effect(() => {
    if (!open) return;
    return trackBox(() => (box = timelineBox()));
  });

  // A key the field types closes it: one no key binding took, from the
  // conversation's typing box, that is not a command key, Tab or Esc (those
  // never reach the terminal's textarea either).
  function typed(event) {
    if (event.defaultPrevented || event.isComposing || event.ctrlKey || event.altKey || event.metaKey) return;
    if (["Tab", "Escape", "Shift", "Control", "Alt", "Meta", "CapsLock"].includes(event.key)) return;
    const target = event.target;
    if (!target?.closest?.("main.conversation")) return;
    if (!target.closest("textarea, input, [contenteditable]:not([contenteditable='false'])")) return;
    closeDetails();
  }
  $effect(() => {
    if (!open) return;
    document.addEventListener("keydown", typed);
    return () => document.removeEventListener("keydown", typed);
  });
</script>

{#if open && box && info}
  <div
    class="details"
    role="region"
    aria-label="Chat details"
    data-details-overlay
    style:left="{box.left}px"
    style:width="{box.width}px"
    style:top="{box.top}px"
    style:max-height="{Math.max(0, box.bottom - box.top)}px"
  >
    <div class="inner">
      <header class="head">
        <h2 class="title" title={session?.title ?? ""}>{session?.title ?? ""}</h2>
        <p class="version">{conn.version?.version ?? ""}</p>
      </header>
      <p class="model">
        {#if info.name}
          <span class="name">{info.name}</span>
          {#if info.via}<span class="via">{info.via}</span>{/if}
        {:else}
          <span class="name">Unknown</span>
        {/if}
        {#if info.reasoning}<span class="part">{info.reasoning}</span>{/if}
        {#if info.usage}
          <span class="part usage {info.usage.level}">
            {#if info.usage.level}<span class="visually-hidden">Context nearly full:</span>{/if}
            {info.usage.percent} <span class="slate">{info.usage.tokens}</span> {info.usage.cost}
          </span>
        {/if}
      </p>
      {#if info.limits.length > 0}
        <p class="limits">
          <span class="slate">Usage left</span>
          {#each info.limits as limit, i (i)}
            <span class="part {limit.level}">{limit.name} {limit.left}{#if limit.resets}<span class="slate"> {limit.resets}</span>{/if}</span>
          {/each}
        </p>
      {/if}
      <div class="columns">
        <FilesSection list={lists.files} label="Modified files" />
        <LspSection list={lists.lsp} label="LSPs" />
        <McpSection list={lists.mcp} label="MCPs" />
        <SkillsSection list={lists.skills} label="Skills" />
      </div>
    </div>
    <Rule marks="both" class="details-rule" />
  </div>
{/if}

<style>
  .details {
    position: fixed;
    z-index: 5;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    background: var(--paper-raised);
  }
  .details :global(.rule.details-rule) {
    flex: none;
  }
  /* The timeline's text column: 66 in from the left, 23 from the right. */
  .inner {
    min-height: 0;
    overflow-y: auto;
    padding: calc(18 * var(--px)) calc(23 * var(--px)) calc(22 * var(--px)) calc(66 * var(--px));
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .head {
    display: flex;
    align-items: baseline;
    gap: calc(16 * var(--px));
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-item);
  }
  .version {
    flex: none;
    font: var(--t-small);
    color: var(--slate);
  }
  .model,
  .limits {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    column-gap: calc(12 * var(--px));
    margin-top: calc(4 * var(--px));
    font: var(--t-small);
    font-variant-numeric: tabular-nums;
  }
  .via,
  .slate {
    color: var(--slate);
  }
  .part {
    display: inline-flex;
    align-items: center;
    gap: calc(4 * var(--px));
    white-space: nowrap;
  }
  /* Amber under 30 % left, rose under 10 % (footer.js meterLevel). */
  .low {
    color: var(--amber-ink);
  }
  .out {
    color: var(--error-ink);
  }
  .columns {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    column-gap: calc(24 * var(--px));
    margin-top: calc(18 * var(--px));
  }
</style>
