<script>
  // A reply as rich Markdown (CHAT-014, CHAT-017, CHAT-018; desktop-design
  // 1.7, 4.1, 4.3). lib/render/markdown.js draws the text into this element
  // and keeps a streaming reply's stable prefix, so an update redraws only the
  // tail. Links are guarded (links.js: they open only after a real click or
  // Enter). Each visual is a VisualChip where its block was, or the figure
  // itself (FigureView, inline) while the right panel is hidden. Marks other
  // features lay over the text (lib/decorations.js) are drawn as spans and a
  // click on one goes to its kind's handler. Placed by ReplyItem (P3.5).
  import { mount, unmount, untrack } from "svelte";
  import { decorationHandler, decorationsFor } from "../../lib/decorations.js";
  import { linkGuard } from "../../lib/render/links.js";
  import { createMarkdown } from "../../lib/render/markdown.js";
  import { ui } from "../../lib/stores/ui.svelte.js";
  import FigureView from "./FigureView.svelte";
  import VisualChip from "./VisualChip.svelte";

  let { text, messageId, sessionId, finished = true, plan = "none" } = $props();

  let root;
  let renderer = null;
  let rendererFor = null;
  // Every visual on the page: its host element → {info, component, asFigure}.
  const visuals = new Map();

  // A web picture stays a link chip: its figure could only be the same chip.
  const drawsAsFigure = (info, hidden) => hidden && !info.href;

  function mountVisual(host, entry, asFigure) {
    const { info } = entry;
    entry.asFigure = asFigure;
    entry.component = asFigure
      ? mount(FigureView, { target: host, props: { figure: info.figure, inline: true, streaming: !info.figureId } })
      : mount(VisualChip, {
          target: host,
          props: { figureId: info.figureId, kind: info.kind, title: info.title, href: info.href ?? null, host: info.host ?? null, inline: info.inline },
        });
  }

  // The renderer asks for one node per visual; the component mounts inside it.
  function visual(info) {
    const host = root.ownerDocument.createElement(info.inline ? "span" : "div");
    host.className = info.inline ? "md-visual md-visual-inline" : "md-visual";
    const entry = { info, component: null, asFigure: false };
    visuals.set(host, entry);
    untrack(() => mountVisual(host, entry, drawsAsFigure(info, ui.panelHidden)));
    return host;
  }

  function release(host) {
    const entry = visuals.get(host);
    if (!entry) return;
    visuals.delete(host);
    unmount(entry.component);
  }

  $effect(() => {
    // A different message gets a renderer of its own.
    const key = `${sessionId}\u0000${messageId}`;
    const stored = text;
    const done = finished;
    const decorations = decorationsFor(messageId);
    untrack(() => {
      if (rendererFor !== key) {
        renderer?.destroy();
        renderer = createMarkdown(root, { messageId, sessionId, visual, release });
        rendererFor = key;
      }
      renderer.update(stored, { finished: done, decorations });
    });
  });

  // Showing or hiding the right panel swaps every visual between its chip and
  // its figure.
  $effect(() => {
    const hidden = ui.panelHidden;
    untrack(() => {
      for (const [host, entry] of visuals) {
        const asFigure = drawsAsFigure(entry.info, hidden);
        if (entry.asFigure === asFigure) continue;
        unmount(entry.component);
        mountVisual(host, entry, asFigure);
      }
    });
  });

  $effect(() => () => {
    renderer?.destroy();
    renderer = null;
    rendererFor = null;
  });

  // A click on a decoration (a phase 4 branch link, say) goes to its kind's
  // handler; a click that ends a selection does not.
  function onclick(event) {
    const span = event.target.closest?.(".md-deco");
    if (!span || !root.contains(span) || !renderer?.map) return;
    if (root.ownerDocument.getSelection?.()?.isCollapsed === false) return;
    const decoration = renderer.map.decorations[Number(span.dataset.deco)];
    if (decoration) decorationHandler(decoration.kind)?.(decoration, event);
  }
</script>

<!-- The links inside are anchors; the guard and the handlers above act on them. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="md" class:md-plan={plan && plan !== "none"} bind:this={root} use:linkGuard {onclick}></div>
