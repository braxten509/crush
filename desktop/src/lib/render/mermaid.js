// Diagrams (CHAT-014 figures; desktop-design 1.7 item 6, 4.3). Mermaid's own
// ESM build is loaded with import() at the first diagram, so the first screen
// does not pay for it. securityLevel "strict" (no iframe, which the content
// rule blocks) and no HTML labels (they need foreignObject, which the cleaner
// drops). Every SVG goes through DOMPurify's SVG profile (sanitize.js).
// Diagrams draw one at a time, in idle time (1.6 item 5).
//
//   renderDiagram(source)   a fragment holding one <svg>, or null when the
//                           diagram cannot be drawn (the figure then shows its
//                           source and DIAGRAM_FAILED)
//   diagramTitle(source)    front-matter title, a title line, else the first
//                           node label, else the diagram's kind
//   whenIdle(task)          runs task in idle time after every task before it
import { svgFragment } from "./sanitize.js";

export const DIAGRAM_FAILED = "This diagram could not be drawn here.";

// The kinds the renderer draws: every kind Mermaid 12.2.0 registers, by the id
// its parse reports and its SVG names (aria-roledescription). The safety
// corpus holds one valid diagram of each (tests/fakes/scripts/unsafe.json,
// "unsafe diagrams"), and p3_safety.py fails when any of them falls back to
// its source, inline or in the panel: the fallback is for bad input only.
export const DIAGRAM_KINDS = [
  "flowchart-v2",
  "sequence",
  "classDiagram",
  "stateDiagram",
  "er",
  "journey",
  "gantt",
  "pie",
  "quadrantChart",
  "requirement",
  "gitGraph",
  "c4",
  "mindmap",
  "timeline",
  "sankey",
  "xychart",
  "block",
  "packet",
  "kanban",
  "architecture",
  "radar",
  "treemap",
  "info",
  "ishikawa",
  "venn",
  "treeView",
  "usecase",
  "wardley",
  "cynefin",
  "railroad",
  "railroadEbnf",
  "railroadAbnf",
  "railroadPeg",
  "agentflow",
  "swimlane",
  "eventmodeling",
  "flowchart-elk",
];

export const MERMAID_CONFIG = {
  startOnLoad: false,
  securityLevel: "strict",
  htmlLabels: false,
  theme: "base",
  look: "neo",
  themeVariables: {
    background: "transparent",
    primaryColor: "#f9f8ef",
    primaryBorderColor: "#383634",
    primaryTextColor: "#0f0f0f",
    lineColor: "#383634",
    fontFamily: "Spectral",
    fontSize: "15px",
    // The base theme turns on gradient borders and a drop shadow for the neo
    // look; every surface keeps flat color and no shadows (desktop-design 4).
    useGradient: false,
    dropShadow: "none",
  },
  // Keys a diagram's own %%{init}%% may not change: Mermaid's list, plus the
  // look and every way to turn HTML labels back on.
  secure: [
    "secure",
    "securityLevel",
    "startOnLoad",
    "maxTextSize",
    "suppressErrorRendering",
    "maxEdges",
    "htmlLabels",
    "flowchart",
    "theme",
    "themeVariables",
    "themeCSS",
    "look",
    "darkMode",
    "fontFamily",
    "altFontFamily",
    "handDrawnSeed",
    "deterministicIds",
    "deterministicIDSeed",
    "dompurifyConfig",
  ],
  suppressErrorRendering: true,
};

let loading = null;

// The module, initialised once. load can be replaced in tests.
export function loadMermaid(load = () => import("mermaid")) {
  loading ??= load().then((module) => {
    const mermaid = module.default ?? module;
    mermaid.initialize(MERMAID_CONFIG);
    return mermaid;
  });
  return loading;
}

export function resetMermaidForTests() {
  loading = null;
}

let count = 0;

export async function renderDiagram(source, { load } = {}) {
  let mermaid;
  try {
    mermaid = await loadMermaid(load);
  } catch {
    return null;
  }
  const id = `md-diagram-${++count}`;
  try {
    if (typeof document !== "undefined" && document.fonts?.ready) await document.fonts.ready;
    const { svg } = await mermaid.render(id, String(source));
    const fragment = svgFragment(svg);
    // One <svg> (its id scopes the diagram's own style rules, so it stays).
    if (!fragment?.querySelector?.("svg") || fragment.childElementCount !== 1) return null;
    return fragment;
  } catch {
    return null;
  } finally {
    // Mermaid leaves its scratch element behind when a render fails.
    if (typeof document !== "undefined") document.getElementById(`d${id}`)?.remove();
  }
}

const queue = [];
let draining = false;

function idle(callback) {
  if (typeof requestIdleCallback === "function") requestIdleCallback(callback, { timeout: 1000 });
  else setTimeout(callback, 0);
}

export function whenIdle(task) {
  return new Promise((resolve, reject) => {
    queue.push({ task, resolve, reject });
    if (!draining) {
      draining = true;
      idle(drain);
    }
  });
}

async function drain() {
  const next = queue.shift();
  if (!next) {
    draining = false;
    return;
  }
  try {
    next.resolve(await next.task());
  } catch (error) {
    next.reject(error);
  }
  idle(drain);
}

// A node id and its label in [ ], ( ), { }, (( )), [[ ]], [( )], ([ ]), {{ }},
// > ], [/ /] or [\ \].
const NODE_LABEL = /^[\w$-]+\s*(?:\(\(|\[\[|\[\(|\(\[|\{\{|\[\/|\[\\|[[({>])\s*("?)(.+?)\1\s*(?:\)\)|\]\]|\)\]|\]\)|\}\}|\/\]|\\\]|[\])}])/;
const EDGE = /\s*(?:<?[-=.]{2,}[>ox]?|&)\s*(?:\|[^|]*\|\s*)?/;

export function diagramTitle(source) {
  const lines = String(source).replace(/\r\n?/g, "\n").split("\n");
  let start = 0;
  // Front matter: "---" on the first line, a "title:" inside.
  if (lines[0]?.trim() === "---") {
    const end = lines.findIndex((line, i) => i > 0 && line.trim() === "---");
    for (const line of lines.slice(1, end < 0 ? lines.length : end)) {
      const m = /^\s*title\s*:\s*(.+?)\s*$/.exec(line);
      if (m) return unquote(m[1]);
    }
    start = end < 0 ? lines.length : end + 1;
  }
  const body = lines
    .slice(start)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("%%"));
  if (body.length === 0) return "";
  const kind = body[0].split(/\s+/)[0];
  // A title line: "title Pets", or on the kind's own line ("pie title Pets").
  for (const line of body) {
    const m = /^(?:\w+\s+)?title\s+(.+)$/.exec(line);
    if (m && !/^(?:subgraph|participant|actor|class|section|note)\b/i.test(line)) return unquote(m[1]);
  }
  if (kind === "sequenceDiagram") {
    for (const line of body.slice(1)) {
      const p = /^(?:participant|actor)\s+(.+?)(?:\s+as\s+(.+))?$/.exec(line);
      if (p) return unquote(p[2] ?? p[1]);
      const message = /^([^\s:<>-][^:<>]*?)\s*(?:--?>>?|--?[x)])/.exec(line);
      if (message) return message[1];
    }
  }
  for (const line of body.slice(1)) {
    for (const part of line.split(EDGE)) {
      const m = NODE_LABEL.exec(part);
      if (m) return unquote(m[2]);
    }
    const c = /^class\s+([\w$-]+)/.exec(line);
    if (c) return c[1];
  }
  return kind;
}

function unquote(s) {
  return s.replace(/^"(.*)"$/, "$1").replace(/^'(.*)'$/, "$1").trim();
}
