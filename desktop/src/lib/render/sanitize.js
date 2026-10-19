// The one door between HTML strings and the page (desktop-design 1.7 item 9).
// This is the only module that imports DOMPurify and the only one that ever
// parses an HTML string; every other renderer builds nodes with
// document.createElement and textContent. A unit test (source-grep.test.js)
// fails when any other file in src/ parses HTML.
//
// Each function hands back nodes, never a string, so nothing downstream can
// put the text into the page as HTML by mistake:
//   stripTags(html)       raw HTML in a reply → its text, every tag removed (1.7 item 2)
//   htmlTextUnits(html)   where that text's characters are in the raw HTML (a
//                         scan of offsets only; it builds nothing)
//   decodeEntity(ref)     one "&name;" / "&#n;" reference → its character(s)
//   mathFragment(html)    KaTeX output → nodes (default HTML profile, MathML kept; 1.7 item 5)
//   svgFragment(svg)      Mermaid output → nodes (SVG profile, links and outside
//                         addresses removed; 1.7 item 6)
//   highlightFragment(h)  highlight.js output → spans with classes only (1.7 item 7)
import DOMPurify from "dompurify";

// Elements whose content is never visible text: dropped whole, as the
// terminal's sanitizer skips them (glamour/v2 ansi/elements.go:415-430 runs
// raw HTML through bluemonday's strict policy and prints what is left).
export const SKIP_CONTENT = ["script", "style", "template", "noscript", "iframe", "noembed", "noframes", "object", "title", "xmp", "plaintext"];

function purifier() {
  // A purifier of our own per profile, so hooks never leak between them.
  return typeof window === "undefined" ? null : DOMPurify(window);
}

const textPurifier = purifier();
const mathPurifier = purifier();
const codePurifier = purifier();
const svgPurifier = purifier();

// The text of raw HTML with every tag removed, character references decoded
// and the ends trimmed, the way the terminal prints it.
export function stripTags(html) {
  if (!textPurifier) return String(html).replace(/<[^>]*>/g, "").trim();
  const fragment = textPurifier.sanitize(String(html), {
    ALLOWED_TAGS: [],
    ALLOWED_ATTR: [],
    KEEP_CONTENT: true,
    FORBID_CONTENTS: SKIP_CONTENT,
    RETURN_DOM_FRAGMENT: true,
  });
  return (fragment.textContent ?? "").trim();
}

// The characters of raw HTML that show as text, in order, each with its source
// range: [{text, start, end}]. Tags, comments, declarations and the content of
// SKIP_CONTENT elements are left out, as stripTags leaves them out; a character
// reference is one unit, its decoded text over the whole reference. The
// builder lines stripTags' text up with these, so a selection of an HTML
// block's words maps to those words and never into a tag.
export function htmlTextUnits(html) {
  const raw = String(html);
  const lower = raw.toLowerCase();
  const units = [];
  let k = 0;
  while (k < raw.length) {
    if (raw[k] === "<") {
      const end = markupEnd(raw, k);
      if (end > k) {
        const name = /^<([a-z][a-z0-9-]*)/.exec(lower.slice(k, k + 40))?.[1];
        k = end;
        if (name && SKIP_CONTENT.includes(name)) {
          const close = name === "plaintext" ? -1 : lower.indexOf(`</${name}`, k);
          k = close < 0 ? raw.length : markupEnd(raw, close);
        }
        continue;
      }
    } else if (raw[k] === "&") {
      const ref = ENTITY_START.exec(raw.slice(k, k + 40))?.[0];
      const value = ref ? decodeEntity(ref) : null;
      if (ref && value !== ref) {
        units.push({ text: value, start: k, end: k + ref.length });
        k += ref.length;
        continue;
      }
    }
    units.push({ text: raw[k], start: k, end: k + 1 });
    k++;
  }
  return units;
}

// Where the markup starting with the "<" at raw[k] ends, or k when that "<"
// is text (the HTML tokenizer's rule: a tag needs a letter after "<" or "</").
function markupEnd(raw, k) {
  if (raw.startsWith("<!--", k)) {
    const close = raw.indexOf("-->", k + 4);
    return close < 0 ? raw.length : close + 3;
  }
  const next = raw[k + 1] ?? "";
  const tag = /[A-Za-z]/.test(next) || (next === "/" && /[A-Za-z]/.test(raw[k + 2] ?? ""));
  if (!tag && next !== "!" && next !== "?" && next !== "/") return k;
  // To the first ">" outside a quoted attribute value.
  let quote = null;
  let afterEquals = false;
  for (let i = k + 1; i < raw.length; i++) {
    const ch = raw[i];
    if (quote) {
      if (ch === quote) quote = null;
    } else if (tag && afterEquals && (ch === '"' || ch === "'")) quote = ch;
    else if (ch === ">") return i + 1;
    if (!quote && ch !== " " && ch !== "\t" && ch !== "\n") afterEquals = ch === "=";
  }
  return raw.length;
}
// its semicolon. Anything else is not decoded.
const ENTITY = /^&(?:[A-Za-z][A-Za-z0-9]{1,31}|#[0-9]{1,7}|#[xX][0-9A-Fa-f]{1,6});$/;
const BASIC = { "&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": '"', "&apos;": "'", "&nbsp;": " " };
const entityCache = new Map();
let decoder = null;

// The characters one reference stands for, or the reference itself when it is
// not a known HTML entity (CommonMark leaves unknown names as written). The
// reference is read by the page's own HTML parser inside a detached textarea,
// whose content can never become elements. Only recognized names are cached
// (HTML has about 2,200), so made-up names and numbers in a reply never grow
// the cache; a number is worked out each time.
export function decodeEntity(ref) {
  if (!ENTITY.test(ref)) return ref;
  if (ref[1] === "#") {
    const code = ref[2] === "x" || ref[2] === "X" ? parseInt(ref.slice(3, -1), 16) : parseInt(ref.slice(2, -1), 10);
    // CommonMark: an invalid code point becomes U+FFFD.
    return code === 0 || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff) ? "�" : String.fromCodePoint(code);
  }
  const cached = entityCache.get(ref);
  if (cached !== undefined) return cached;
  if (typeof document === "undefined") return BASIC[ref] ?? ref;
  decoder ??= document.createElement("textarea");
  decoder.innerHTML = ref;
  const value = decoder.value;
  // A name the parser did not know comes back unchanged, and a known legacy
  // name read without the rest of the reference leaves that rest behind
  // ("&ampx;" reads "&x;"): either way the value ends as the reference does.
  if (value.endsWith(ref.slice(-2))) return ref;
  entityCache.set(ref, value);
  return value;
}

// How many references the cache holds (for the test).
export function entityCacheSize() {
  return entityCache.size;
}

// Every character reference in text decoded (CommonMark decodes them in text,
// link targets and titles, never in code).
export const ENTITY_REF = /&(?:[A-Za-z][A-Za-z0-9]{1,31}|#[0-9]{1,7}|#[xX][0-9A-Fa-f]{1,6});/g;
const ENTITY_START = new RegExp(`^${ENTITY_REF.source}`);
export function decodeEntities(text) {
  const s = String(text);
  return s.includes("&") ? s.replace(ENTITY_REF, decodeEntity) : s;
}

// KaTeX's HTML and MathML, sanitized with DOMPurify's default profile.
export function mathFragment(html) {
  if (!mathPurifier) return emptyFragment();
  return mathPurifier.sanitize(String(html), { RETURN_DOM_FRAGMENT: true });
}

// highlight.js output: spans with classes and their text, nothing else.
export function highlightFragment(html) {
  if (!codePurifier) return emptyFragment();
  return codePurifier.sanitize(String(html), {
    ALLOWED_TAGS: ["span"],
    ALLOWED_ATTR: ["class"],
    KEEP_CONTENT: true,
    RETURN_DOM_FRAGMENT: true,
  });
}

// A CSS value may refer to the diagram's own parts (url(#arrowhead)) but never
// to an address outside it: a crafted diagram could otherwise make the page
// ask for a picture from the web (blocked by the content rule, but still an
// attempt). @import is never allowed.
const URL_REF = /url\(\s*(['"]?)(.*?)\1\s*\)/gi;
export function outsideRefs(css) {
  if (/@import/i.test(css)) return true;
  for (const match of String(css).matchAll(URL_REF)) {
    if (!match[2].trim().startsWith("#")) return true;
  }
  return false;
}

function cleanCss(css) {
  return String(css)
    .replace(/@import[^;]*;?/gi, "")
    .replace(URL_REF, (whole, quote, target) => (target.trim().startsWith("#") ? whole : "none"));
}

if (svgPurifier) {
  svgPurifier.addHook("uponSanitizeAttribute", (_node, data) => {
    if (typeof data.attrValue === "string" && outsideRefs(data.attrValue)) data.keepAttr = false;
  });
  svgPurifier.addHook("afterSanitizeElements", (node) => {
    if (node.nodeName?.toLowerCase() === "style" && outsideRefs(node.textContent ?? "")) {
      node.textContent = cleanCss(node.textContent ?? "");
    }
  });
  // Mermaid puts a clickable node's position on the <a> around it. The <a>
  // goes, so its transform moves onto a <g> holding what the <a> held, which
  // the cleaner then keeps (and checks) in the <a>'s place.
  svgPurifier.addHook("uponSanitizeElement", (node, data) => {
    if (data.tagName !== "a" || !node.hasAttribute?.("transform")) return;
    const g = node.ownerDocument.createElementNS("http://www.w3.org/2000/svg", "g");
    g.setAttribute("transform", node.getAttribute("transform"));
    g.append(...node.childNodes);
    node.append(g);
  });
}

// Mermaid's SVG with the SVG profile: no links (their words stay, in their
// place), no pictures, no outside addresses.
export function svgFragment(svg) {
  if (!svgPurifier) return emptyFragment();
  return svgPurifier.sanitize(String(svg), {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: ["a", "image", "foreignobject", "foreignObject", "script", "animate", "set", "animatemotion", "animatetransform", "animatecolor"],
    FORBID_ATTR: ["href", "xlink:href", "src"],
    KEEP_CONTENT: true,
    RETURN_DOM_FRAGMENT: true,
  });
}

function emptyFragment() {
  return typeof document === "undefined" ? null : document.createDocumentFragment();
}
