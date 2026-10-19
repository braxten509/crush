// The safety probe section (desktop-design 1.9 and 2.9; PLAN.md section 15).
// What a crafted reply could try, as numbers the hidden-screen check reads:
//
//   light  inline_blocked   the content rule blocked the probe's own inline
//                           script and handler (probe.js inlineCodeBlocked);
//                           null before that test ran
//          csp_violations   securitypolicyviolation events, the probe's own
//                           two left out
//          location         location.href
//          workspace_keys   Object.keys(conn.workspace): never env or config
//          links_asked      how many link opens the page asked for (links.js)
//   heavy  handlers_found   on* attributes anywhere in the page
//          scripts_found    script, frame, object, embed, base and refresh
//                           elements the page did not start with
//          controls_found   forms, fields and buttons inside drawn replies
//          bad_hrefs_found  address attributes anywhere in the page whose
//                           address is not allowed (addressAllowed below)
//          violations       [{directive, blocked, own}] every event seen;
//                           blocked is a keyword ("inline", "eval") or the
//                           address's scheme, never the address
//          roots            [{text}] each drawn reply's words (the first 400)
//          blocks           [{text, rect}] each paragraph and list item drawn
//          anchors          [{text, href, rect}] every link drawn from a reply
//          refused          [{text, rect}] every link turned into text
//          chips            [{figure, kind, title, rect}] the visuals chips
//          figures          [{figure, kind, state}] the figures on the page
//          focus            {tag, href, text} of the focused element, or null
//
// The counter starts when the renderer is first imported, before the probe's
// own test runs; the section reports only in test runs (--probe).
import { probeSection } from "../probe.js";
import { conn } from "../stores/connection.svelte.js";
import { linksAsked, safeHref } from "./links.js";

// Elements that run, load or redirect something. The page starts with its own
// script and the content rule's meta element; anything later is counted.
const ACTIVE = "script, iframe, frame, frameset, object, embed, applet, portal, base, meta[http-equiv]";
// Where replies are drawn: Markdown roots and the figures' bodies.
const DRAWN = ".md, .figure-body";
const CONTROLS = "form, input, button, textarea, select";
// Attributes that hold an address.
const ADDRESS_ATTRS = new Set(["href", "xlink:href", "src", "srcset", "srcdoc", "action", "formaction", "poster", "background", "ping", "data", "codebase", "manifest", "lowsrc", "dynsrc"]);
// Keywords a violation may report instead of an address.
const KEYWORDS = new Set(["inline", "eval", "wasm-eval", "trusted-types-policy", "trusted-types-sink", "self", ""]);

const violations = [];
const startup = new WeakSet();
// The probe's own test (probe.js inlineCodeBlocked) sets window.__probeInline
// and then tries one inline script and one inline handler; the first two
// inline-script violations after that are its own.
let ownLeft = 2;

function blockedKind(uri) {
  const value = String(uri ?? "").trim();
  if (KEYWORDS.has(value)) return value;
  try {
    return new URL(value).protocol;
  } catch {
    return "other";
  }
}

function onViolation(event) {
  const directive = String(event.effectiveDirective || event.violatedDirective || "");
  const blocked = blockedKind(event.blockedURI);
  const own = ownLeft > 0 && "__probeInline" in window && blocked === "inline" && directive.startsWith("script-src");
  if (own) ownLeft -= 1;
  violations.push({ directive, blocked, own });
}

if (typeof window !== "undefined" && typeof document !== "undefined") {
  window.addEventListener("securitypolicyviolation", onViolation, true);
  for (const el of document.querySelectorAll(ACTIVE)) startup.add(el);
}

// The page's own pages: same scheme and host as the window (tauri://localhost).
// An origin compare would not do: every non-web scheme's origin is "null".
function ownPage(url) {
  return url.protocol === location.protocol && url.host === location.host;
}

function pictureAllowed(value) {
  const text = String(value).trim();
  if (/^data:image\//i.test(text)) return true;
  let url;
  try {
    url = new URL(text, location.href);
  } catch {
    return false;
  }
  return url.protocol === "blob:" || url.protocol === "asset:" || ownPage(url);
}

// Whether an address attribute is one the page may hold: a link the renderer
// keeps (links.js safeHref) on an anchor; a picture or resource from the app,
// asset:, blob: or data:image/*; never a frame's own document.
export function addressAllowed(el, name, value) {
  if (name === "srcdoc") return false;
  if (name === "srcset") {
    return String(value)
      .split(",")
      .map((candidate) => candidate.trim().split(/\s+/)[0])
      .filter(Boolean)
      .every(pictureAllowed);
  }
  const tag = el.localName;
  if ((tag === "a" || tag === "area") && (name === "href" || name === "xlink:href")) return safeHref(value) !== null;
  return pictureAllowed(value);
}

function rectOf(el) {
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

function words(el) {
  return (el.textContent ?? "").replace(/\s+/g, " ").trim();
}

export function scanPage(doc = document) {
  let handlers = 0;
  let bad = 0;
  for (const el of doc.querySelectorAll("*")) {
    for (const attr of el.attributes) {
      const name = attr.name.toLowerCase();
      if (name.startsWith("on")) handlers += 1;
      else if (ADDRESS_ATTRS.has(name) && !addressAllowed(el, name, attr.value)) bad += 1;
    }
  }
  const scripts = [...doc.querySelectorAll(ACTIVE)].filter((el) => !startup.has(el)).length;
  // Drawn surfaces may hold each other (a figure inside a reply): each element once.
  const drawn = [...doc.querySelectorAll(DRAWN)];
  const inside = (selector) => [...new Set(drawn.flatMap((root) => [...root.querySelectorAll(selector)]))];
  return {
    handlers_found: handlers,
    scripts_found: scripts,
    controls_found: inside(CONTROLS).filter((el) => !el.closest(".md-chip")).length,
    bad_hrefs_found: bad,
    violations: violations.map((v) => ({ ...v })),
    roots: [...doc.querySelectorAll(".md")].filter((root) => !root.parentElement?.closest(".md")).map((root) => ({ text: words(root).slice(0, 400) })),
    blocks: inside("p, li").map((el) => ({ text: words(el).slice(0, 120), rect: rectOf(el) })),
    anchors: inside("a[href]:not(.md-chip)").map((a) => ({ text: words(a), href: a.getAttribute("href"), rect: rectOf(a) })),
    refused: inside(".md-link-refused").map((span) => ({ text: words(span), rect: rectOf(span) })),
    chips: [...doc.querySelectorAll(".md-chip")].map((chip) => ({
      figure: chip.dataset.figure ?? null,
      kind: chip.dataset.kind ?? null,
      title: words(chip.querySelector(".md-chip-title") ?? chip),
      rect: rectOf(chip),
    })),
    figures: [...doc.querySelectorAll(".figure-view")].map((view) => ({
      figure: view.dataset.figure ?? null,
      kind: view.dataset.kind ?? null,
      state: view.dataset.state ?? null,
    })),
    focus: focusOf(doc),
  };
}

function focusOf(doc) {
  const el = doc.activeElement;
  if (!el || el === doc.body || el === doc.documentElement) return null;
  return { tag: el.localName, href: el.getAttribute("href"), text: words(el).slice(0, 80) };
}

export function safetyLight() {
  return {
    inline_blocked: "__probeInline" in window ? window.__probeInline !== true : null,
    csp_violations: violations.filter((v) => !v.own).length,
    location: location.href,
    workspace_keys: Object.keys(conn.workspace ?? {}),
    links_asked: linksAsked(),
  };
}

probeSection("safety", safetyLight, () => scanPage(document));
