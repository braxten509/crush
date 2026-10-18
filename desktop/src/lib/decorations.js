// Marks laid over a message's text by other features (desktop-design 1.10).
// Phase 3 registers none; phase 4 adds branch links (3.3). A source gives a
// message's decorations, [{kind, start, end, data}] over source offsets; a
// handler acts on one (for example a click on a branch link). The renderer
// (P3.4) draws them and calls the handler of their kind.

const sources = [];
const handlers = new Map(); // kind → fn(decoration, event)

export function registerDecorationSource(fn) {
  sources.push(fn);
}

export function registerDecorationHandler(kind, fn) {
  handlers.set(kind, fn);
}

// Every source's decorations for one message, sorted by start. Sources may
// read stores, so a renderer that calls this inside an effect redraws when
// they change.
export function decorationsFor(messageId) {
  return sources.flatMap((source) => source(messageId) ?? []).sort((a, b) => a.start - b.start);
}

// The handler for a kind, or null.
export function decorationHandler(kind) {
  return handlers.get(kind) ?? null;
}
