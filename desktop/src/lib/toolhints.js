// The line under a working tool group (desktop-design-5-6 6.3, LIVE-004;
// scaffold S5, complete): LIVE registers the 60-second hint. fn(item) → text
// or null for a ToolGroupItem (lib/types.js); ToolGroup draws the first
// non-null answer.
const hints = [];

export function registerToolHint(fn) {
  hints.push(fn);
  return () => {
    const at = hints.indexOf(fn);
    if (at >= 0) hints.splice(at, 1);
  };
}

export function toolHint(item) {
  for (const fn of hints) {
    const text = fn(item);
    if (text !== null && text !== undefined) return text;
  }
  return null;
}
