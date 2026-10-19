// The safety corpus (desktop-design 1.7 and 2.9; PLAN.md section 15). The
// replies of script "unsafe" (tests/fakes/scripts/unsafe.json, the same texts
// p3_safety.py streams into the window) drawn by the renderer, finished and at
// every step of a stream. Nothing may run, load, post or open: no script,
// frame or object element, no on* attribute, no address that is not allowed,
// no form control, and exactly one link: the fake page.
import { afterEach, describe, expect, test } from "vitest";
import fixture from "../../../tests/fixtures/unsafe-chat.json";
import script from "../../../tests/fakes/scripts/unsafe.json";
import { createMarkdown, renderMarkdown } from "./markdown.js";
import { renderMath } from "./math.js";
import { MERMAID_CONFIG, renderDiagram, resetMermaidForTests } from "./mermaid.js";
import { addressAllowed, scanPage } from "./safety.js";
import { stripTags, svgFragment } from "./sanitize.js";

const reply = Object.fromEntries(script.turns.map((turn) => [turn.when, turn.steps.map((step) => step.text).join("")]));
const REPLIES = Object.entries(reply);
const LISTENER = "http://127.0.0.1:47321";

function draw(text, options = {}) {
  const root = renderMarkdown(text, { messageId: "m", sessionId: "s", ...options });
  document.body.append(root);
  return root;
}

afterEach(() => document.body.replaceChildren());

function expectClean(what) {
  const scan = scanPage(document);
  expect(
    { handlers: scan.handlers_found, scripts: scan.scripts_found, controls: scan.controls_found, bad: scan.bad_hrefs_found },
    what,
  ).toEqual({ handlers: 0, scripts: 0, controls: 0, bad: 0 });
  return scan;
}

// The renderer's own elements and attributes (1.7 item 1); KaTeX's output in
// .md-math is checked by the math tests below.
const ELEMENTS = new Set(["P", "H1", "H2", "H3", "H4", "H5", "H6", "UL", "OL", "LI", "BLOCKQUOTE", "PRE", "CODE", "A", "EM", "STRONG", "DEL", "BR", "HR", "SPAN"]);
const ATTRIBUTES = /^(class|href|draggable|rel|data-[a-z-]+)$/;

function expectOwnShape(root, what) {
  const walk = (node) => {
    for (const el of node.children) {
      expect(ELEMENTS.has(el.tagName), `${what}: element ${el.tagName}`).toBe(true);
      for (const attr of el.attributes) expect(attr.name, `${what}: attribute on ${el.tagName}`).toMatch(ATTRIBUTES);
      if (!el.classList.contains("md-math")) walk(el);
    }
  };
  walk(root);
}

// Every attribute value in root, for "no refused target survives anywhere".
function attributeValues(root) {
  return [root, ...root.querySelectorAll("*")].flatMap((el) => [...el.attributes].map((attr) => `${el.tagName}[${attr.name}]=${attr.value}`));
}

const refusedTexts = (root) => [...root.querySelectorAll(".md-link-refused")].map((span) => span.textContent);
const paragraphs = (root) => [...root.querySelectorAll("p")].map((p) => p.textContent);

describe("the corpus covers 2.9", () => {
  test("every case the design lists is in the script, and the fixture holds the same replies", () => {
    const all = REPLIES.map(([, text]) => text).join("\n");
    for (const needle of [
      "<script>",
      "onerror=",
      '<a href="javascript:',
      "](javascript:alert(1))",
      "](data:text/html,",
      "<iframe",
      `<form action="${LISTENER}/form" method="post"`,
      `<img src="${LISTENER}/beacon"`,
      '%%{init: {"securityLevel": "loose"',
      "click A call",
      "\\href{javascript:alert(1)}",
      "\\url{",
      'class="slot',
    ]) {
      expect(all, needle).toContain(needle);
    }
    // Review finding 5: each spelling in a Markdown link and in a raw <a>.
    for (const target of ["\tjavascript:alert(1)", " javascript:alert(1)", "JAVASCRIPT:alert(1)", "java\nscript:alert(1)", "vbscript:x", "blob:x", "data:text/html,x"]) {
      expect(reply["unsafe links"], JSON.stringify(target)).toContain(`](${target})`);
      expect(reply["unsafe links"], JSON.stringify(target)).toContain(`<a href="${target}">`);
    }
    // The fixture (p3_safety.py's first run) shows the very same replies.
    const session = fixture.get.sessions[0].id;
    const shown = fixture.get[`sessions/${session}/messages`].filter((m) => m.role === "assistant").map((m) => m.parts.find((p) => p.type === "text").data.text);
    expect(shown).toEqual(REPLIES.map(([, text]) => text));
  });
});

describe("finished replies", () => {
  for (const [when, text] of REPLIES) {
    test(when, () => {
      const root = draw(text);
      expectClean(when);
      expectOwnShape(root, when);
      for (const value of attributeValues(root)) expect(value).not.toMatch(/javascript|vbscript|data:text|blob:|__unsafe|47321\/(?!page\.html)/i);
    });
  }

  test("exactly one link in the whole corpus: the fake page", () => {
    for (const [, text] of REPLIES) draw(text);
    const scan = expectClean("all replies");
    expect(scan.anchors.map((a) => [a.text, a.href])).toEqual([["the fake page", `${LISTENER}/page.html`]]);
    const anchor = document.querySelector(".md a[href]");
    expect(anchor.getAttribute("draggable")).toBe("false");
    expect(anchor.getAttribute("rel")).toBe("noreferrer");
    expect(anchor.hasAttribute("target")).toBe(false);
  });
});

describe("a reply mid-stream never draws anything unsafe", () => {
  for (const [when, text] of REPLIES) {
    test(when, () => {
      const root = document.createElement("div");
      root.className = "md";
      document.body.append(root);
      const renderer = createMarkdown(root, { messageId: "m", sessionId: "s" });
      for (const size of [1, 7]) {
        for (let at = size; at < text.length + size; at += size) {
          const prefix = text.slice(0, Math.min(at, text.length));
          renderer.update(prefix, { finished: false });
          expectClean(`${when} after ${prefix.length}: ${JSON.stringify(prefix.slice(-24))}`);
        }
        renderer.update(text, { finished: true });
        expectClean(`${when} finished`);
      }
    });
  }
});

describe("links (review finding 5)", () => {
  test("every refused spelling of a Markdown link becomes its text", () => {
    const root = draw(reply["unsafe links"]);
    expect(refusedTexts(root).sort()).toEqual(
      ["javascript link", "data link", "tab first", "space first", "upper case", "vbscript link", "blob link", "data html", "entity link", "colon entity", "reference link", "javascript:alert(1)"].sort(),
    );
    // A newline cannot be inside a link target, so that one is not a link at
    // all; a picture with a refused address shows its words.
    expect(paragraphs(root)).toContain("[newline inside](java\nscript:alert(1))");
    expect(paragraphs(root)).toContain("bad picture");
    expect(root.querySelector("img")).toBeNull();
  });

  test("every raw <a> becomes its words, with no anchor around them", () => {
    const root = draw(reply["unsafe links"]);
    for (const words of ["raw tab first", "raw space first", "raw upper case", "raw newline inside", "raw vbscript", "raw blob", "raw data"]) {
      const p = [...root.querySelectorAll("p")].find((el) => el.textContent === words);
      expect(p, words).toBeTruthy();
      expect(p.closest("a"), words).toBeNull();
      expect(p.querySelector("a"), words).toBeNull();
    }
    expect(root.querySelectorAll("a[href]").length).toBe(1);
  });

  test("the page scan counts what the renderer must never make", () => {
    const a = document.createElement("a");
    for (const [name, value, ok] of [
      ["href", "https://example.com", true],
      ["href", "mailto:a@example.com", true],
      ["href", "./notes.md", true],
      ["href", "javascript:alert(1)", false],
      ["href", "\tjavascript:alert(1)", false],
      ["href", "data:text/html,x", false],
      ["href", "blob:x", false],
      ["srcdoc", "<p>x</p>", false],
    ]) {
      expect(addressAllowed(a, name, value), `${name}=${value}`).toBe(ok);
    }
    const img = document.createElement("img");
    expect(addressAllowed(img, "src", "data:image/png;base64,AAAA")).toBe(true);
    expect(addressAllowed(img, "src", "blob:abc")).toBe(true);
    expect(addressAllowed(img, "src", `${LISTENER}/beacon`)).toBe(false);
    expect(addressAllowed(img, "src", "data:text/html,x")).toBe(false);
    expect(addressAllowed(img, "srcset", `data:image/png;base64,AA 1x, ${LISTENER}/x.png 2x`)).toBe(false);
    // A page that holds each thing the scan looks for.
    const root = draw("words");
    const bad = document.createElement("span");
    bad.setAttribute("onclick", "x");
    const frame = document.createElement("iframe");
    const field = document.createElement("button");
    const link = document.createElement("a");
    link.setAttribute("href", "javascript:alert(1)");
    root.append(bad, frame, field, link);
    const scan = scanPage(document);
    expect([scan.handlers_found, scan.scripts_found, scan.controls_found, scan.bad_hrefs_found]).toEqual([1, 1, 1, 1]);
  });
});

describe("raw markup shows as its words", () => {
  test("no tag of the markup reply reaches the page; its words stay", () => {
    const root = draw(reply["unsafe markup"]);
    expect(root.querySelector("script, img, iframe, form, input, button, object, embed, base, meta, link, style, svg, details, video")).toBeNull();
    const words = paragraphs(root);
    for (const text of ["raw javascript anchor", "Send the form", "svg link", "detailsinside", "The markup reply ends here."]) expect(words).toContain(text);
    // Nothing a tag held as an address or code shows as words either.
    expect(root.textContent).not.toMatch(/__unsafe|47321|onerror|alert/);
    // A paragraph of tags only (<video></video>) leaves no empty block.
    expect([...root.children].every((el) => el.textContent.trim() !== "")).toBe(true);
  });

  test("the fake permission slot is plain text inside the reply", () => {
    const root = draw(reply["unsafe slot"]);
    expect(root.querySelector("button, [role], [data-probe], [aria-modal], [autofocus], .slot, .slot-button")).toBeNull();
    const html = root.querySelector(".md-html");
    expect(html.textContent).toBe("Allow bash to run this?\nAllow\nAllow for session\nDeny");
    expect(root.querySelector("pre.md-code").textContent).toContain("rm -rf ~/proj");
  });

  test("tags are removed the way the terminal prints them", () => {
    expect(stripTags('<div class="slot"><button onclick="x()">Allow</button></div>')).toBe("Allow");
    expect(stripTags("<script>alert(1)</script>after")).toBe("after");
    expect(stripTags("<style>p{}</style><iframe>inside</iframe><b>bold</b>")).toBe("bold");
    expect(stripTags("&lt;b&gt; stays text")).toBe("<b> stays text");
  });
});

describe("math cannot link or load", () => {
  test("inline math with \\href, \\url and \\includegraphics: no anchor, no picture", () => {
    const root = draw(reply["unsafe figures"]);
    const math = [...root.querySelectorAll(".md-math")];
    expect(math.length).toBe(3);
    for (const el of math) {
      expect(el.querySelector("a, img, image, use")).toBeNull();
      for (const value of attributeValues(el)) expect(value).not.toMatch(/javascript|47321|href|src/i);
    }
    expectClean("figures reply");
  });

  test("the display math draws the same way where the panel shows it", () => {
    const source = /\$\$\n([\s\S]*?)\n\$\$/.exec(reply["unsafe figures"])[1];
    expect(source).toContain("\\htmlData");
    const fragment = renderMath(source, { display: true });
    const holder = document.createElement("div");
    holder.append(fragment);
    expect(holder.querySelector(".katex")).not.toBeNull();
    expect(holder.querySelector("a, img")).toBeNull();
    for (const value of attributeValues(holder)) expect(value).not.toMatch(/javascript|onclick|href/i);
  });
});

describe("the diagram", () => {
  test("stays a chip in the chat; Mermaid gets the strict settings whatever the source asks", async () => {
    const root = draw(reply["unsafe figures"]);
    expect(root.querySelector(".md-chip[data-kind=diagram]")).not.toBeNull();
    expect(root.querySelector("svg")).toBeNull();

    const fence = /```mermaid\n([\s\S]*?)\n```/.exec(reply["unsafe figures"])[1];
    const seen = {};
    const fake = {
      initialize(config) {
        seen.config = config;
      },
      async render(id, source) {
        seen.source = source;
        // What a Mermaid that obeyed the source could hand back.
        return {
          svg:
            `<svg id="${id}" xmlns="http://www.w3.org/2000/svg"><style>@import url(${LISTENER}/x.css); .n{fill:url(${LISTENER}/f)} .m{fill:url(#ok)}</style>` +
            `<g onclick="alert(1)"><a href="javascript:alert(1)"><text>Sign in</text></a></g>` +
            `<foreignObject><div onclick="x">html label</div></foreignObject><image href="${LISTENER}/mermaid-img"/>` +
            `<rect style="fill:url(${LISTENER}/r)" width="1" height="1"/></svg>`,
        };
      },
    };
    resetMermaidForTests();
    const fragment = await renderDiagram(fence, { load: async () => fake });
    expect(seen.config.securityLevel).toBe("strict");
    expect(seen.config.htmlLabels).toBe(false);
    expect(seen.config.secure).toEqual(expect.arrayContaining(["securityLevel", "htmlLabels", "flowchart", "startOnLoad"]));
    expect(MERMAID_CONFIG.startOnLoad).toBe(false);
    expect(seen.source).toBe(fence);
    const holder = document.createElement("div");
    holder.className = "figure-body";
    holder.append(fragment);
    document.body.append(holder);
    expect(holder.querySelector("a, foreignObject, foreignobject, image, script")).toBeNull();
    expect(holder.textContent).toContain("Sign in");
    for (const value of attributeValues(holder)) expect(value).not.toMatch(/javascript|47321|onclick/i);
    expect(holder.querySelector("style").textContent).not.toContain("47321");
    expect(holder.querySelector("style").textContent).toContain("url(#ok)");
    expectClean("the drawn diagram");
    resetMermaidForTests();
  });

  test("the SVG cleaner alone drops links, pictures, frames and handlers", () => {
    const holder = document.createElement("div");
    holder.append(svgFragment('<svg><a href="javascript:alert(1)"><text>t</text></a><image href="x.png"/><foreignObject><p>x</p></foreignObject><circle onload="x" r="1"/></svg>'));
    expect(holder.querySelector("a, image, foreignObject, foreignobject")).toBeNull();
    expect(holder.querySelector("circle").hasAttribute("onload")).toBe(false);
    expect(holder.textContent).toContain("t");
  });

  test("a clickable node keeps its place when its link goes", () => {
    // Mermaid puts a clickable node's position on the <a> around it.
    const holder = document.createElement("div");
    holder.append(
      svgFragment(
        '<svg><g class="nodes"><a xlink:href="about:blank" transform="translate(167, 35)"><g class="node clickable" id="flowchart-B-1"><rect width="80" height="40"/><text>Home</text></g></a></g></svg>',
      ),
    );
    expect(holder.querySelector("a")).toBeNull();
    const node = holder.querySelector("#flowchart-B-1");
    expect(node.parentElement.getAttribute("transform")).toBe("translate(167, 35)");
    expect(node.textContent).toBe("Home");
  });
});

describe("pictures", () => {
  test("a web picture becomes a link chip, never a picture that loads", () => {
    const root = draw(`Look: ![beacon](${LISTENER}/beacon.png)\n\n![shot](data:image/png;base64,iVBORw0KGgo=)`);
    expect(root.querySelector("img")).toBeNull();
    const chips = [...root.querySelectorAll(".md-chip")];
    expect(chips.map((c) => c.dataset.kind)).toEqual(["picture", "picture"]);
    expect(chips[0].getAttribute("href")).toBe(`${LISTENER}/beacon.png`);
    expect(chips[0].textContent).toContain("127.0.0.1");
    expect(chips[1].hasAttribute("href")).toBe(false);
    expectClean("pictures");
  });
});
