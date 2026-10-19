<script>
  // A tool's Markdown result (CHAT-070, CHAT-078, CHAT-079: Fetch, Search,
  // Agent and Agentic Fetch), quiet: the reply renderer of lib/render/
  // markdown.js in the body's smaller type and slate. Its tables, math,
  // diagrams and pictures are not the reply's figures (the right panel lists
  // only replies' figures, 4.3), so each draws in place (FigureView inline);
  // a web picture stays a link chip. Links open only after a click (1.7).
  import { mount, unmount } from "svelte";
  import { linkGuard } from "../../lib/render/links.js";
  import { createMarkdown } from "../../lib/render/markdown.js";
  import FigureView from "../render/FigureView.svelte";

  let { text, sessionId = "", messageId = "" } = $props();

  let root;
  const mounted = new Map(); // host → component

  // null leaves the renderer's own chip (a web picture's link chip).
  function visual(info) {
    if (!info.figure || info.href) return null;
    const host = root.ownerDocument.createElement(info.inline ? "span" : "div");
    host.className = info.inline ? "md-visual md-visual-inline" : "md-visual";
    mounted.set(host, mount(FigureView, { target: host, props: { figure: info.figure, inline: true, streaming: false } }));
    return host;
  }

  function release(host) {
    const component = mounted.get(host);
    if (!component) return;
    mounted.delete(host);
    unmount(component);
  }

  $effect(() => {
    const renderer = createMarkdown(root, { messageId, sessionId, visual, release });
    renderer.update(text, { finished: true });
    return () => {
      renderer.destroy?.();
      for (const host of [...mounted.keys()]) release(host);
    };
  });
</script>

<div class="tool-markdown" bind:this={root} use:linkGuard></div>

<style>
  .tool-markdown {
    font: var(--t-body-small);
    color: var(--slate);
    overflow-wrap: anywhere;
  }
</style>
