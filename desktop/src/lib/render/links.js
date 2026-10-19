// Links (desktop-design 1.7 item 3; review finding 5). One normalised check
// decides whether a target may stay a link, and one guard catches every way an
// anchor can act, on every surface that draws Markdown.
//
//   safeHref(target)        the target to keep, or null: the URL parser reads it
//                           once, so "\tjavascript:", " JAVASCRIPT:" and
//                           "java\nscript:" all show their real protocol; only
//                           http:, https:, mailto: and file: (which covers every
//                           target without a scheme) are kept
//   makeAnchor(doc, href)   an anchor for a kept target: href as written (the
//                           opener needs ~ and :line:col), draggable="false",
//                           rel="noreferrer", never target
//   guardLinks(root, open)  catches click, auxclick, the middle button's press,
//                           dragstart, Enter and the native context menu on
//                           anchors inside root; returns a function that removes
//                           the listeners. open(href) runs only after a real
//                           click or Enter
//   linkGuard               the same as a Svelte action: use:linkGuard
//   openLink(href)          open_link (1.3) with the project folder; a refusal
//                           shows as an error notice
//   linksAsked()            how many times openLink ran (the safety probe)
import { open_link } from "../engine/commands.js";
import { conn } from "../stores/connection.svelte.js";
import { notify } from "../stores/ui.svelte.js";

const KEPT = new Set(["http:", "https:", "mailto:", "file:"]);

export function safeHref(target) {
  if (typeof target !== "string" || target.trim() === "") return null;
  let url;
  try {
    url = new URL(target, "file:///");
  } catch {
    return null;
  }
  return KEPT.has(url.protocol) ? target : null;
}

export function makeAnchor(doc, href) {
  const a = doc.createElement("a");
  a.setAttribute("href", href);
  a.setAttribute("draggable", "false");
  a.setAttribute("rel", "noreferrer");
  return a;
}

// How many opens the page asked for (the probe's safety section reads it; the
// addresses themselves are not kept).
let asked = 0;
export function linksAsked() {
  return asked;
}

export async function openLink(href) {
  asked += 1;
  try {
    await open_link(href, conn.project.path || conn.project.cwd);
    return true;
  } catch (error) {
    notify("error", error?.message ?? String(error));
    return false;
  }
}

// A press that moves farther than this before the release is a drag.
const DRAG_SLOP = 4;
// A click the browser makes from our Enter (detail 0) within this time is the
// same press, not a second one.
const ENTER_ECHO_MS = 500;

// Events already handled by an inner guard (a MarkdownView inside a guarded
// timeline): the outer guard leaves them alone, so a link opens once.
const handled = new WeakSet();

// trust(event) says whether a person made the event; tests replace it, since
// a page model cannot make trusted events.
export function guardLinks(root, open = openLink, { trust = (event) => event.isTrusted } = {}) {
  let press = null; // {x, y, dragged} of the last primary press inside root
  let lastEnter = { anchor: null, at: -Infinity };

  const anchorOf = (event) => {
    let node = event.target;
    if (node && node.nodeType !== 1) node = node.parentElement;
    const a = node?.closest?.("a");
    return a && root.contains(a) ? a : null;
  };

  const onMouseDown = (event) => {
    if (event.button === 0) press = { x: event.clientX, y: event.clientY, dragged: false };
    // The middle button: no autoscroll, no paste, no "open in new window".
    else if (event.button === 1 && anchorOf(event)) event.preventDefault();
  };

  const onMouseMove = (event) => {
    if (!press || press.dragged || (event.buttons & 1) === 0) return;
    if (Math.hypot(event.clientX - press.x, event.clientY - press.y) > DRAG_SLOP) press.dragged = true;
  };

  const onClick = (event) => {
    const a = anchorOf(event);
    if (!a) return;
    event.preventDefault();
    if (handled.has(event)) return;
    handled.add(event);
    if (!trust(event) || event.button !== 0 || event.detail > 1) return;
    if (event.detail === 0 && lastEnter.anchor === a && event.timeStamp - lastEnter.at < ENTER_ECHO_MS) return;
    if (event.detail !== 0) {
      const moved = press && (press.dragged || Math.hypot(event.clientX - press.x, event.clientY - press.y) > DRAG_SLOP);
      if (moved || !selectionCollapsed(root)) return;
    }
    open(a.getAttribute("href"));
  };

  const onAuxClick = (event) => {
    if (anchorOf(event)) event.preventDefault();
  };

  const onDragStart = (event) => {
    // The click that may end this press is a drag's, whatever came before.
    press = { x: press?.x ?? event.clientX ?? 0, y: press?.y ?? event.clientY ?? 0, dragged: true };
    if (anchorOf(event) || selectionHoldsAnchor(root)) event.preventDefault();
  };

  const onKeyDown = (event) => {
    if (event.key !== "Enter" || event.ctrlKey || event.altKey || event.metaKey) return;
    const a = anchorOf(event);
    if (!a) return;
    event.preventDefault();
    if (handled.has(event)) return;
    handled.add(event);
    if (!trust(event) || event.repeat) return;
    lastEnter = { anchor: a, at: event.timeStamp };
    open(a.getAttribute("href"));
  };

  // The web view's own menu would offer "Open Link"; the app's menu (4.2)
  // still gets the event.
  const onContextMenu = (event) => {
    if (anchorOf(event)) event.preventDefault();
  };

  const listeners = [
    ["mousedown", onMouseDown],
    ["mousemove", onMouseMove],
    ["click", onClick],
    ["auxclick", onAuxClick],
    ["dragstart", onDragStart],
    ["keydown", onKeyDown],
    ["contextmenu", onContextMenu],
  ];
  for (const [type, fn] of listeners) root.addEventListener(type, fn);
  return () => {
    for (const [type, fn] of listeners) root.removeEventListener(type, fn);
  };
}

// use:linkGuard or use:linkGuard={open}
export function linkGuard(node, open) {
  let remove = guardLinks(node, open ?? openLink);
  return {
    update(next) {
      remove();
      remove = guardLinks(node, next ?? openLink);
    },
    destroy() {
      remove();
    },
  };
}

function selectionCollapsed(root) {
  const selection = root.ownerDocument.defaultView?.getSelection?.();
  return !selection || selection.isCollapsed;
}

// A drag of selected text that holds a link would carry its address.
function selectionHoldsAnchor(root) {
  const selection = root.ownerDocument.defaultView?.getSelection?.();
  if (!selection || selection.isCollapsed) return false;
  for (const a of root.querySelectorAll("a")) {
    if (selection.containsNode(a, true)) return true;
  }
  return false;
}
