<script module>
  // The shared sheet frame (desktop-design-5-6 2.1, DLG-001 to DLG-003,
  // DLG-007, DLG-008; look 5.1). Every sheet draws itself in one: the title
  // line (title, and title-side controls on the right that hide whole when
  // they cannot sit beside the title, internal/ui/dialog/common.go:106-118),
  // a rule, the body, and the help footer. The frame is centered
  // in the window and never comes closer than 24 to its edges; the body
  // scrolls inside, and a resize re-centers and re-fits it in the same frame
  // (dialog.go:263-274). An onboarding sheet sits at the bottom left of the
  // main column, 24 from its edges (dialog.go:281-299, common.go:151-153); a
  // docked one (the Themes sheet and editor) runs down the window's right
  // edge.
  //
  // Opening moves the focus to the first field, else the list; Tab stays
  // inside (lib/kit/sheetkeys.js); a sheet that comes back to the front takes
  // the focus back where it was; closing gives the focus back to where it was
  // before the sheet opened, the typing box by default (2.0 rule 9). Only the
  // front sheet hears keys: onKey(event, combo) first, then its kit parts,
  // then Esc and Alt+Esc close it.
  //   id, title, label? (the screen-reader name when untitled), width: "small" | "standard" | "wide" (520, 760, 1040),
  //   maxHeight? (picture px), controls? (a snippet beside the title; buttons
  //   in it get the small-caps label look, the active one marked with
  //   aria-pressed="true"), help: [{key, text, disabled?}], onKey?, docked?,
  //   onboarding?, children
  // `section(label)`, exported, draws a section header: a small-caps label
  // followed by a rule (DLG-020).
  import Rule from "../Rule.svelte";

  export { section };
</script>

<script>
  import { onMount, tick } from "svelte";
  import { closeDialog, dialogs, frontDialog } from "../../lib/dialogs.js";
  import { focusables, setSheet } from "../../lib/kit/sheetkeys.js";
  import { focusField } from "../../lib/stores/composer.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import Icon from "../Icon.svelte";
  import NoticeLine from "../NoticeLine.svelte";
  import HelpFooter from "./HelpFooter.svelte";
  import Scrollbar from "./Scrollbar.svelte";

  const WIDTHS = { small: 520, standard: 760, wide: 1040 };
  const EDGE = 24; // picture px a sheet keeps from the window's edges
  const CONTROLS_GAP = 24; // the least room between the title and its controls
  const CLOSE_ROOM = 38; // the close cross (20) and its gap (18)

  let {
    id,
    title,
    label = null, // the sheet's name for screen readers when it has no title
    width = "standard",
    maxHeight = null,
    controls = null,
    help = [],
    onKey = null,
    docked = false,
    onboarding = false,
    children,
  } = $props();

  const uid = $props.id();
  let frame = $state(null);
  let line = $state(null);
  let heading = $state(null);
  let side = $state(null);
  let body = $state(null);
  let controlsFit = $state(true);
  let spot = $state(null); // the onboarding place: {left, bottom, width, height} in CSS px
  let fontsLoaded = $state(0);

  const isFront = $derived(frontDialog() === id);
  const barMode = $derived(ws.config?.options?.tui?.scrollbar ?? "default");
  const pictureWidth = $derived(WIDTHS[width] ?? WIDTHS.standard);

  function scale() {
    return parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1;
  }

  // -- Focus --

  let opener = null;
  let lastFocus = null;

  function focusInside() {
    if (!frame) return;
    const keep = lastFocus && lastFocus.isConnected && frame.contains(lastFocus) && !lastFocus.disabled ? lastFocus : null;
    const target =
      keep ??
      frame.querySelector("input:not([disabled]), textarea:not([disabled])") ??
      frame.querySelector("[data-kit-list] [role='listbox']") ??
      focusables(frame)[0] ??
      frame;
    target.focus({ preventScroll: true });
  }

  function giveBack(back) {
    const usable = back?.isConnected && !back.closest("[inert]");
    if (dialogs.length > 0) {
      // The sheet now in front takes its own focus back; the opener is in it.
      if (usable) back.focus({ preventScroll: true });
      return;
    }
    if (usable) back.focus({ preventScroll: true });
    else focusField();
  }

  onMount(() => {
    const active = document.activeElement;
    opener = active instanceof HTMLElement && active !== document.body && !frame.contains(active) ? active : null;
    focusInside();
    const off = setSheet(id, { onKey: () => onKey, root: () => frame, report, heavy });
    return () => {
      off();
      const back = opener;
      queueMicrotask(() => giveBack(back));
    };
  });

  // Back in front (the sheet over it closed, or it was opened again).
  $effect(() => {
    if (!isFront || !frame) return;
    tick().then(() => {
      if (frame && !frame.contains(document.activeElement)) focusInside();
    });
  });

  // The close cross does what Esc does: the sheet's own Esc first (a form
  // may cancel its edit), else the sheet closes (owner, 2026-10-20: every
  // shortcut also has something to click).
  function closeClick() {
    const event = new KeyboardEvent("keydown", { key: "Escape" });
    if (typeof onKey === "function" && onKey(event, "Escape") === true) return;
    closeDialog(id);
  }

  // -- Title-side controls: beside the title with room to spare, else on a
  //    line of their own under it (owner, 2026-10-20: they never vanish) --

  function fitControls() {
    if (!line || !heading || !side) {
      controlsFit = true;
      return;
    }
    controlsFit = heading.scrollWidth + CONTROLS_GAP * scale() + side.scrollWidth + CLOSE_ROOM * scale() <= line.clientWidth;
  }

  $effect(() => {
    void title;
    void fontsLoaded;
    if (!line) return;
    fitControls();
    const observer = new ResizeObserver(fitControls);
    observer.observe(line);
    if (side) observer.observe(side);
    return () => observer.disconnect();
  });

  $effect(() => {
    const fonts = document.fonts;
    if (!fonts) return;
    const loaded = () => fontsLoaded++;
    fonts.addEventListener?.("loadingdone", loaded);
    fonts.ready?.then(loaded);
    return () => fonts.removeEventListener?.("loadingdone", loaded);
  });

  // -- Onboarding place: the main column's bottom left --

  function placeOnboarding() {
    const main = document.querySelector("main.conversation");
    const r = main?.getBoundingClientRect() ?? { left: 0, top: 0, width: innerWidth, height: innerHeight, bottom: innerHeight };
    const edge = EDGE * scale();
    spot = { left: r.left + edge, bottom: innerHeight - r.bottom + edge, width: Math.max(0, r.width - 2 * edge), height: Math.max(0, r.height - 2 * edge) };
  }

  $effect(() => {
    if (!onboarding) {
      spot = null;
      return;
    }
    placeOnboarding();
    const main = document.querySelector("main.conversation");
    const observer = new ResizeObserver(placeOnboarding);
    if (main) observer.observe(main);
    window.addEventListener("resize", placeOnboarding);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", placeOnboarding);
    };
  });

  // -- The probe (plugins/kit.js reads these through lib/kit/sheetkeys.js) --

  function report() {
    return {
      title,
      width,
      docked: Boolean(docked),
      onboarding: Boolean(onboarding),
      max_height: maxHeight,
      controls: controls ? (controlsFit ? "shown" : "wrapped") : null,
      focus: focusName(),
    };
  }

  function focusName() {
    const active = document.activeElement;
    if (!frame || !active || !frame.contains(active)) return null;
    if (active.closest("[data-kit-field]")) return "field";
    if (active.closest("[data-kit-area]")) return "area";
    if (active.closest("[data-kit-list]")) return "list";
    if (active.dataset?.button) return `button:${active.dataset.button}`;
    return active.tagName.toLowerCase();
  }

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  function heavy() {
    const titleRange = document.createRange();
    if (heading) titleRange.selectNodeContents(heading);
    const text = heading ? titleRange.getBoundingClientRect() : null;
    return {
      frame: rectOf(frame),
      title: rectOf(heading),
      title_text: text ? { x: text.x, y: text.y, w: Math.min(text.width, heading.clientWidth), h: text.height } : null,
      title_cut: heading ? heading.scrollWidth > heading.clientWidth + 0.5 : false,
      title_line: rectOf(line),
      controls: side ? { rect: rectOf(side), visible: getComputedStyle(side).visibility !== "hidden", wrapped: !controlsFit } : null,
      rule: rectOf(frame?.querySelector(".kit-title-rule")),
      body: rectOf(body),
      body_scroll: body ? { scroll_height: body.scrollHeight, client_height: body.clientHeight } : null,
      help: rectOf(frame?.querySelector("[data-kit-help]")),
      edges: [...(frame?.querySelectorAll(":scope > .edge") ?? [])].map(rectOf),
      background: frame ? getComputedStyle(frame).backgroundColor : null,
      window: { w: innerWidth, h: innerHeight, px: scale() },
    };
  }
</script>

{#snippet section(label)}
  <div class="kit-section">
    <h3 class="kit-section-label">{label}</h3>
    <Rule class="kit-section-rule" />
  </div>
{/snippet}

<div
  class="place"
  class:docked
  class:onboarding={Boolean(onboarding && spot)}
  style:left={onboarding && spot ? `${spot.left}px` : null}
  style:bottom={onboarding && spot ? `${spot.bottom}px` : null}
  style:width={onboarding && spot ? `${spot.width}px` : null}
  style:height={onboarding && spot ? `${spot.height}px` : null}
>
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="frame"
    class:capped={maxHeight !== null && maxHeight !== undefined}
    role="dialog"
    aria-modal="true"
    aria-labelledby={title ? uid : null}
    aria-label={title ? null : label}
    tabindex="-1"
    data-sheet={id}
    data-kit-sheet
    data-front={isFront ? "true" : "false"}
    style:--sheet-w={pictureWidth}
    style:--sheet-h={maxHeight}
    bind:this={frame}
    onfocusin={(event) => (lastFocus = event.target)}
  >
    {#if !docked}
      <span class="edge top" aria-hidden="true"></span>
      <span class="edge bottom" aria-hidden="true"></span>
      <span class="edge left" aria-hidden="true"></span>
      <span class="edge right" aria-hidden="true"></span>
    {/if}

    <!-- A sheet with no title and no controls (Quit) has no title band. -->
    <div class="title-line" class:untitled={!title && !controls} class:wrapped={!controlsFit} bind:this={line} data-kit-title>
      <h2 class="title" id={uid} bind:this={heading}>{title}</h2>
      {#if controls}
        <div class="controls" bind:this={side} data-kit-controls>
          {@render controls()}
        </div>
      {/if}
      <button class="close" type="button" aria-label="Close" title="Close" data-kit-close onclick={closeClick}><Icon name="close" size={12} /></button>
    </div>
    {#if title || controls}
      <Rule marks="both" class="kit-title-rule" />
    {/if}

    <div class="body" bind:this={body} data-kit-body>
      {@render children?.()}
    </div>
    <Scrollbar target={body} mode={barMode} />
    {#if isFront}
      <NoticeLine inline />
    {/if}

    {#if help && help.length > 0}
      <div class="foot">
        <HelpFooter pairs={help} />
      </div>
    {/if}
  </div>
</div>

<style>
  /* The room a sheet may take: the window less 24 on each side. */
  .place {
    position: absolute;
    inset: calc(24 * var(--px));
    display: flex;
    align-items: center;
    justify-content: center;
    pointer-events: none;
  }
  .place.docked {
    inset: 0;
    align-items: stretch;
    justify-content: flex-end;
  }
  .place.onboarding {
    inset: auto;
    align-items: flex-end;
    justify-content: flex-start;
  }

  .frame {
    position: relative;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    width: min(calc(var(--sheet-w) * var(--px)), 100%);
    max-height: 100%;
    padding: calc(22 * var(--px)) 0 calc(20 * var(--px));
    background: var(--paper);
    color: var(--ink);
    pointer-events: auto;
    outline: none;
    /* The sheet unrolls from its top edge (app.css, the paper motion). */
    animation: paper-unfold var(--motion) var(--ease-paper);
  }
  .frame.capped {
    max-height: min(calc(var(--sheet-h) * var(--px)), 100%);
  }
  .docked .frame {
    height: 100%;
    max-height: none;
    border-left: 1px solid var(--rule-frame);
  }

  /* The page frame's drawing (Frame.svelte): hairlines that meet at the
     corners (owner, 2026-10-20: the outer box is closed). */
  .edge {
    position: absolute;
    background: var(--rule-frame);
    pointer-events: none;
  }
  .edge.top,
  .edge.bottom {
    left: 0;
    right: 0;
    height: 1px;
  }
  .edge.top {
    top: 0;
  }
  .edge.bottom {
    bottom: 0;
  }
  .edge.left,
  .edge.right {
    top: 0;
    bottom: 0;
    width: 1px;
  }
  .edge.left {
    left: 0;
  }
  .edge.right {
    right: 0;
  }

  /* Title line: the title, and its controls on the right on its baseline. */
  .title-line {
    position: relative;
    display: flex;
    flex: none;
    align-items: baseline;
    min-height: calc(38 * var(--px));
    padding: 0 calc(26 * var(--px));
  }
  .title-line.untitled {
    display: none;
  }
  .title {
    flex: 1 1 auto;
    min-width: 0;
    margin: 0;
    font: var(--t-project);
    color: var(--ink);
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .controls {
    display: flex;
    flex: none;
    align-items: baseline;
    gap: calc(18 * var(--px));
    margin-left: calc(24 * var(--px));
  }
  /* Too narrow for both: the controls take a line of their own under the
     title; the close cross stays beside the title. */
  .title-line.wrapped {
    flex-wrap: wrap;
  }
  .title-line.wrapped .controls {
    order: 3;
    flex-basis: 100%;
    margin: calc(10 * var(--px)) 0 0;
  }
  /* The close cross: 12 to see, at least 24 real pixels to hit. */
  .close {
    order: 2;
    position: relative;
    flex: none;
    align-self: center;
    display: grid;
    place-items: center;
    width: calc(20 * var(--px));
    height: calc(20 * var(--px));
    margin-left: calc(18 * var(--px));
    padding: 0;
    border: 0;
    background: none;
    color: var(--slate);
  }
  .close::after {
    content: "";
    position: absolute;
    left: 50%;
    top: 50%;
    width: max(24px, 100%);
    height: max(24px, 100%);
    transform: translate(-50%, -50%);
  }
  .close:hover {
    color: var(--ink);
  }
  .controls :global(button) {
    padding: 0;
    border: 0;
    background: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
    white-space: nowrap;
  }
  .controls :global(button[aria-pressed="true"]),
  .controls :global(button[aria-current="true"]) {
    color: var(--ink);
    text-decoration-line: underline;
    text-decoration-thickness: calc(2 * var(--px));
    text-decoration-color: var(--ink);
    text-underline-offset: calc(5 * var(--px));
  }
  .frame :global(.rule.kit-title-rule) {
    margin-top: calc(18 * var(--px));
  }

  .body {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    min-height: 0;
    padding: calc(17 * var(--px)) calc(26 * var(--px)) 0;
    overflow-x: hidden;
    overflow-y: auto;
  }
  .foot {
    flex: none;
    margin-top: calc(20 * var(--px));
    padding: 0 calc(26 * var(--px));
  }

  /* A section header: a small-caps label followed by a rule (DLG-020). Global,
     so a sheet may also write the markup itself. */
  :global(.kit-section) {
    display: flex;
    flex: none;
    align-items: center;
    gap: calc(14 * var(--px));
    margin: calc(14 * var(--px)) 0 calc(13 * var(--px));
  }
  :global(.kit-section-label) {
    flex: none;
    margin: 0;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  :global(.kit-section > .rule.kit-section-rule) {
    flex: 1 1 auto;
  }
</style>
