// Two helpers for the tool components, used as Svelte attachments
// ({@attach …}).
//
// show(build): puts the nodes build() returns into the element, and again
// whenever what build reads changes. The nodes come from createElement and
// text nodes (render/code.js, render/ansi.js); no HTML text is ever parsed
// here (desktop-design 1.7).
//
// size(fn): one shared ResizeObserver for every tool row; fn(rect) runs
// when the element's box changes, and once when it starts being watched.
export function show(build) {
  return (element) => {
    const nodes = build();
    element.replaceChildren(...(nodes === null || nodes === undefined ? [] : [nodes]));
  };
}

let observer = null;
const watchers = new Map();

export function size(fn) {
  return (element) => {
    if (typeof ResizeObserver === "undefined") return undefined;
    observer ??= new ResizeObserver((entries) => {
      for (const entry of entries) watchers.get(entry.target)?.(entry.contentRect);
    });
    watchers.set(element, fn);
    observer.observe(element);
    return () => {
      watchers.delete(element);
      observer.unobserve(element);
    };
  };
}
