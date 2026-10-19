<script module>
  // An attachment or a kept skill as a chip (desktop-design 4.9; CHAT-006,
  // TYPE-036 to TYPE-038; the terminal's internal/ui/attachments). An icon by
  // kind, the file's own name cut to 15 characters with "…" (attachments.go:
  // maxFilename), and at the right the ✕ of a removable chip or, in delete
  // mode, the digit that removes it, in the same place so the chips do not
  // move (attachments.go Render). A kept skill shows SKILL in small caps
  // before its name. A click (press and release in place) on a picture opens
  // the viewer. Placed by UserItem, PendingItem (P3.5) and ChipRow (P3.7);
  // ChipRow passes onremove and, in delete mode, number.
  const MAX_NAME = 15;

  // The name a chip shows: the last part of a path, cut to MAX_NAME
  // characters, the last of them "…".
  export function chipName(name) {
    const base = String(name ?? "").split(/[\\/]/).filter(Boolean).pop() ?? "";
    const chars = Array.from(base);
    return chars.length > MAX_NAME ? `${chars.slice(0, MAX_NAME - 1).join("")}…` : base;
  }
</script>

<script>
  import Icon from "../Icon.svelte";
  import { openViewer } from "../../lib/stores/ui.svelte.js";

  let { chip, removable = false, number = null, onremove = null } = $props();

  const ICONS = { picture: "image", file: "file", text: "text", skill: "skill" };
  // A press that moves farther than this before the release is a drag.
  const DRAG_SLOP = 4;

  const deleting = $derived(number !== null && number !== undefined);
  const viewable = $derived(chip.kind === "picture" && chip.viewable && Boolean(chip.src));
  let press = null;

  function down(event) {
    press = event.button === 0 ? { x: event.clientX, y: event.clientY } : null;
  }

  function open(event) {
    if (!viewable || deleting || event.button !== 0) return;
    if (event.detail !== 0 && (!press || Math.hypot(event.clientX - press.x, event.clientY - press.y) > DRAG_SLOP)) return;
    openViewer({ src: chip.src, name: chip.name, mime: chip.mime });
  }

  function key(event) {
    if (viewable && !deleting && (event.key === "Enter" || event.key === " ")) {
      event.preventDefault();
      openViewer({ src: chip.src, name: chip.name, mime: chip.mime });
    }
  }
</script>

{#snippet label()}
  <Icon name={ICONS[chip.kind] ?? "file"} size={16} />
  {#if chip.kind === "skill"}<span class="skill">Skill</span>{/if}
  <span class="name">{chipName(chip.name)}</span>
{/snippet}

<span class="chip" data-kind={chip.kind}>
  {#if viewable}
    <span class="label viewable" role="button" tabindex="0" onmousedown={down} onclick={open} onkeydown={key}>{@render label()}</span>
  {:else}
    <span class="label">{@render label()}</span>
  {/if}
  {#if deleting}
    <span class="trail number">{number}</span>
  {:else if removable}
    <button type="button" class="trail remove" aria-label="Remove {chip.name}" onclick={() => onremove?.(chip)}><Icon name="close" size={16} /></button>
  {/if}
</span>

<style>
  .chip {
    display: inline-flex;
    align-items: center;
    gap: calc(8 * var(--px));
    box-sizing: border-box;
    height: calc(30 * var(--px));
    max-width: 100%;
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    font: var(--t-small);
    color: var(--ink);
    white-space: nowrap;
    user-select: none;
  }
  .label {
    display: inline-flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-width: 0;
  }
  .viewable {
    cursor: pointer;
  }
  .viewable:hover .name {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .chip :global(.icon) {
    flex: none;
  }
  .skill {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* The ✕ and the delete-mode digit take the same 16 px place. */
  .trail {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
  }
  .remove {
    color: var(--slate);
  }
  .remove:hover {
    color: var(--ink);
  }
  /* The digit's line is exactly the place's height: a taller line box overflows
     the 16 px place, and WebKit then aligns it to the top, not the middle. */
  .number {
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    line-height: calc(16 * var(--px));
  }
</style>
