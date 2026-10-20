<script>
  // The right column (SIDE-005, D12; owner, 2026-10-19): the usage meters
  // (context, then the agent's usage windows or the chat's cost), then a
  // scrolling body with the selected visual. No project, files, LSP, MCP or
  // skills: the chat head has the folder and branch, the model button the
  // model. Everything comes from the stores (design 1.5.3); the panel takes
  // no props. App.svelte hides it (D11, or its own switch).
  //
  // Focus (SIDE-012): → or l from the timeline, or a click, focuses the body
  // (lib/plugins/panel.js has the keys); the body's scrollbar shows while it
  // has focus or for 2 s after a wheel turn. With composer_focus_only a click
  // leaves focus in the field and the wheel still scrolls the body on hover.
  import { sessions } from "../lib/stores/sessions.svelte.js";
  import { setFocus, ui } from "../lib/stores/ui.svelte.js";
  import { ws } from "../lib/stores/workspace.svelte.js";
  import Meters from "./panel/Meters.svelte";
  import { panel, setPanelBody, toField } from "./panel/panel.svelte.js";
  import VisualsSection from "./panel/VisualsSection.svelte";
  import Rule from "./Rule.svelte";

  const focusOnly = $derived(ws.config?.options?.tui?.composer_focus_only === true);

  let aside = $state();
  let body = $state();
  $effect(() => {
    setPanelBody(body);
    return () => setPanelBody(null);
  });

  // The scrollbar: while focus is in the panel, or 2 s after a wheel turn
  // (the terminal's sidebar scrollbar, chat_scroll.go:37 scrollbarHideDuration).
  let wheelTimer = null;
  function wheel() {
    panel.wheeled = true;
    clearTimeout(wheelTimer);
    wheelTimer = setTimeout(() => (panel.wheeled = false), 2000);
  }
  // Hidden with focus inside (SIDE-002): focus goes to the field. App asks
  // the composer first; this covers a field that did not take it.
  $effect(() => () => {
    clearTimeout(wheelTimer);
    panel.wheeled = false;
    panel.focused = false;
    if (ui.focus === "panel") toField(aside);
  });

  function focusIn() {
    panel.focused = true;
    setFocus("panel");
  }
  function focusOut(event) {
    panel.focused = aside.contains(event.relatedTarget);
  }

  // A click on the panel's words (not a control) focuses the body. Inside the
  // body the browser already does, as the body is click-focusable.
  function click(event) {
    if (focusOnly || !body || event.button !== 0) return;
    if (event.target.closest?.("button, a, input, textarea, [tabindex]")) return;
    if (String(globalThis.getSelection?.() ?? "")) return;
    body.focus({ preventScroll: true });
  }

  // composer_focus_only: a press never takes focus from the field; clicks
  // still work.
  function down(event) {
    if (focusOnly) event.preventDefault();
  }

  // The reveal scroll (4.3): the figure comes into view inside the body.
  function scale() {
    return parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
  }
  function scrollToFigure(element) {
    if (!body) return;
    const margin = 12 * scale();
    const view = body.getBoundingClientRect();
    const box = element.getBoundingClientRect();
    if (box.top < view.top + margin || box.height > view.height - 2 * margin) body.scrollTop += box.top - view.top - margin;
    else if (box.bottom > view.bottom - margin) body.scrollTop += box.bottom - view.bottom + margin;
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<aside
  class="panel"
  aria-label="Usage and visuals"
  data-probe="panel"
  bind:this={aside}
  onfocusin={focusIn}
  onfocusout={focusOut}
  onclick={click}
  onmousedown={down}
>
  <Meters />

  <Rule marks="both" class="under-usage" />

  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="body"
    class:show-bar={panel.focused || panel.wheeled}
    tabindex="-1"
    role="region"
    aria-label="Visuals"
    bind:this={body}
    onwheel={wheel}
  >
    <VisualsSection sessionId={sessions.currentId} scrollTo={scrollToFigure} />
  </div>
</aside>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
  /* Coming back, the panel settles in from the right (the paper motion). */
  .panel {
    animation: paper-slide-left var(--motion) var(--ease-paper);
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    padding: calc(26 * var(--px)) calc(19 * var(--px)) 0 calc(26 * var(--px));
  }

  /* The meters lead the column now that PROJECT is gone. */
  .panel :global(.usage-wrap) {
    margin-top: 0;
  }

  .panel :global(.rule.under-usage) {
    margin: calc(22 * var(--px)) calc(-19 * var(--px)) 0 calc(-26 * var(--px));
  }

  /* The body runs to the panel's edges, so its scrollbar sits in the right
     gutter, outside the rows, and the reveal mark has room in the left one. */
  .body {
    flex: 1;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    margin: 0 calc(-19 * var(--px)) 0 calc(-26 * var(--px));
    padding: calc(22 * var(--px)) calc(19 * var(--px)) calc(26 * var(--px)) calc(26 * var(--px));
    scrollbar-width: none;
  }
  .body::-webkit-scrollbar {
    width: 0;
    height: 0;
  }
  .body.show-bar {
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .body.show-bar::-webkit-scrollbar {
    width: calc(6 * var(--px));
  }
  .body.show-bar::-webkit-scrollbar-thumb {
    background: var(--rule-control);
  }
  .body:focus-visible {
    outline-offset: -1px;
  }

</style>
