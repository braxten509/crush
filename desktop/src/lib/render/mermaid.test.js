// Diagrams (1.7 item 6, 4.3): the settings, the title rule, the cleaner on
// the SVG and the fallback. The page model cannot lay out SVG text, so the
// drawing itself is checked in the window (p3_safety.py); here Mermaid is a
// stand-in that returns a fixed SVG.
import { afterEach, describe, expect, test } from "vitest";
import script from "../../../tests/fakes/scripts/unsafe.json";
import { DIAGRAM_FAILED, DIAGRAM_KINDS, MERMAID_CONFIG, diagramTitle, renderDiagram, resetMermaidForTests, whenIdle } from "./mermaid.js";

afterEach(() => resetMermaidForTests());

function fake(svg, seen = {}) {
  return async () => ({
    default: {
      initialize(config) {
        seen.config = config;
      },
      async render(id, source) {
        seen.id = id;
        seen.source = source;
        if (svg instanceof Error) throw svg;
        return { svg: svg.replaceAll("ID", id) };
      },
    },
  });
}

describe("settings", () => {
  test("strict, no HTML labels, the base theme with the paper's colors, flat", () => {
    expect(MERMAID_CONFIG).toMatchObject({ startOnLoad: false, securityLevel: "strict", htmlLabels: false, theme: "base", look: "neo" });
    expect(MERMAID_CONFIG.themeVariables).toEqual({
      background: "transparent",
      primaryColor: "#f9f8ef",
      primaryBorderColor: "#383634",
      primaryTextColor: "#0f0f0f",
      lineColor: "#383634",
      fontFamily: "Spectral",
      fontSize: "15px",
      // The neo look's own gradient borders and shadows stay off (desktop-design 4: flat color, no shadows).
      useGradient: false,
      dropShadow: "none",
    });
    for (const key of ["securityLevel", "htmlLabels", "flowchart", "theme", "themeVariables", "themeCSS", "look", "secure"]) {
      expect(MERMAID_CONFIG.secure).toContain(key);
    }
  });
});

describe("renderDiagram", () => {
  test("the SVG is cleaned: no script, no handler, no outside link or picture", async () => {
    const seen = {};
    const svg =
      '<svg id="ID" xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><style>#ID .node{fill:#f9f8ef}@import url(http://127.0.0.1/beacon.css);</style>' +
      '<script>alert(2)</script><a href="javascript:alert(3)"><text>link</text></a>' +
      '<image href="http://127.0.0.1/beacon"/><foreignObject><div>html</div></foreignObject>' +
      '<rect style="fill:url(http://127.0.0.1/beacon)" width="10" height="10"/><g class="node"><text>Start</text></g>' +
      '<path marker-end="url(#ID_arrow)"/></svg>';
    const fragment = await renderDiagram("flowchart LR\nA[Start]-->B", { load: fake(svg, seen) });
    expect(seen.config).toBe(MERMAID_CONFIG);
    expect(seen.source).toBe("flowchart LR\nA[Start]-->B");
    const root = fragment.querySelector("svg");
    expect(root.getAttribute("id")).toBe(seen.id);
    expect(fragment.querySelector("script, a, image, foreignObject, foreignobject")).toBeNull();
    for (const el of fragment.querySelectorAll("*")) {
      for (const attr of el.attributes) {
        expect(attr.name).not.toMatch(/^on/i);
        if (!attr.name.startsWith("xmlns")) expect(attr.value).not.toMatch(/http:|javascript:/i);
      }
    }
    expect(fragment.querySelector("style").textContent).not.toMatch(/@import|beacon/);
    expect(fragment.querySelector("path").getAttribute("marker-end")).toBe(`url(#${seen.id}_arrow)`);
    expect(fragment.textContent).toContain("Start");
  });

  test("a diagram Mermaid cannot draw gives null (the figure shows its source)", async () => {
    expect(await renderDiagram("not a diagram", { load: fake(new Error("Parse error")) })).toBeNull();
    resetMermaidForTests();
    expect(await renderDiagram("x", { load: fake("<div>not svg</div>") })).toBeNull();
    resetMermaidForTests();
    expect(await renderDiagram("x", { load: () => Promise.reject(new Error("no module")) })).toBeNull();
    expect(DIAGRAM_FAILED).toBe("This diagram could not be drawn here.");
  });
});

describe("the kinds the renderer draws", () => {
  test("the safety corpus holds one valid diagram of each kind, and Mermaid itself reads each as that kind", async () => {
    // p3_safety.py fails when any of these falls back, inline or in the panel.
    expect(DIAGRAM_KINDS.length).toBeGreaterThan(30);
    const reply = script.turns.find((turn) => turn.when === "unsafe diagrams")?.steps[0].text ?? "";
    const fences = [...reply.matchAll(/^```mermaid\n([\s\S]*?)\n```$/gm)].map((m) => m[1]);
    expect(fences.map(diagramTitle)).toEqual(DIAGRAM_KINDS.map((kind) => `Valid ${kind}`));
    const { default: mermaid } = await import("mermaid");
    mermaid.initialize(MERMAID_CONFIG);
    for (const [i, fence] of fences.entries()) {
      expect((await mermaid.parse(fence))?.diagramType, DIAGRAM_KINDS[i]).toBe(DIAGRAM_KINDS[i]);
    }
  });
});

describe("whenIdle", () => {
  test("one task at a time, in order", async () => {
    const log = [];
    const slow = (name, ms) => () => new Promise((resolve) => setTimeout(() => (log.push(name), resolve(name)), ms));
    const results = await Promise.all([whenIdle(slow("a", 20)), whenIdle(slow("b", 1)), whenIdle(() => log.push("c") && "c")]);
    expect(results).toEqual(["a", "b", "c"]);
    expect(log).toEqual(["a", "b", "c"]);
  });

  test("a failing task does not stop the queue", async () => {
    const failed = whenIdle(() => {
      throw new Error("x");
    });
    const next = whenIdle(() => "ok");
    await expect(failed).rejects.toThrow("x");
    expect(await next).toBe("ok");
  });
});

describe("diagramTitle", () => {
  test("front matter wins", () => {
    expect(diagramTitle("---\ntitle: Release flow\n---\nflowchart LR\nA[Start]-->B")).toBe("Release flow");
  });
  test("a title line", () => {
    expect(diagramTitle('pie title "Pets adopted"\n"Dogs" : 386')).toBe("Pets adopted");
    expect(diagramTitle("gantt\n  title A Gantt Diagram\n  section A\n  task :a1, 2014-01-01, 30d")).toBe("A Gantt Diagram");
  });
  test("the first node label", () => {
    expect(diagramTitle("flowchart LR\n  A[Start] --> B{Is it?}")).toBe("Start");
    expect(diagramTitle("graph TD\n  %% a comment\n  id1((Round one)) --> id2")).toBe("Round one");
    expect(diagramTitle('flowchart TD\n  A["Quoted label"] -->|yes| B')).toBe("Quoted label");
    expect(diagramTitle("flowchart TD\n  A --> B[Second]")).toBe("Second");
  });
  test("sequence and class diagrams", () => {
    expect(diagramTitle("sequenceDiagram\n  participant A as Alice\n  A->>B: hi")).toBe("Alice");
    expect(diagramTitle("sequenceDiagram\n  Alice->>John: Hello")).toBe("Alice");
    expect(diagramTitle("classDiagram\n  class Animal\n  Animal <|-- Duck")).toBe("Animal");
  });
  test("else the diagram's kind", () => {
    expect(diagramTitle("stateDiagram-v2\n  [*] --> Still")).toBe("stateDiagram-v2");
    expect(diagramTitle("")).toBe("");
  });
});
