// The four render components (desktop-design 4.3, 4.9; CHAT-006, CHAT-007,
// CHAT-014, CHAT-018; TYPE-036 to TYPE-038), mounted in the page model:
// MarkdownView's figures and decorations, VisualChip, FigureView for
// every kind, and the attachment Chip.
import { flushSync, mount, tick, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import Chip, { chipName } from "../../components/render/Chip.svelte";
import FigureView, { PICTURE_FAILED, keepLastWords } from "../../components/render/FigureView.svelte";
import MarkdownView from "../../components/render/MarkdownView.svelte";
import VisualChip from "../../components/render/VisualChip.svelte";
import script from "../../../tests/fakes/scripts/unsafe.json";
import { registerDecorationHandler, registerDecorationSource } from "../decorations.js";
import { setPanelHidden, ui } from "../stores/ui.svelte.js";
import { extractFigures } from "./figures.js";
import { DIAGRAM_FAILED, DIAGRAM_KINDS, loadMermaid, resetMermaidForTests } from "./mermaid.js";
import { scanPage } from "./safety.js";

const spies = vi.hoisted(() => ({ reveal: vi.fn(), fileRead: vi.fn(), openLink: vi.fn() }));
vi.mock("../stores/visuals.svelte.js", () => ({ revealFigure: spies.reveal, visuals: {}, setFigures() {}, figuresFor: () => [] }));
vi.mock("../engine/commands.js", () => ({ file_read: spies.fileRead, open_link: spies.openLink, lastClipboardWrite: () => null }));

const mounted = [];
function show(Component, props) {
  const target = document.createElement("div");
  document.body.append(target);
  const component = mount(Component, { target, props });
  mounted.push(component);
  flushSync();
  return target;
}

// Idle-time work (diagrams, display math) runs through setTimeout in the page model.
async function idle() {
  for (let i = 0; i < 6; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await tick();
  }
  flushSync();
}

beforeEach(() => {
  spies.reveal.mockReset();
  spies.fileRead.mockReset();
  spies.openLink.mockReset();
  setPanelHidden(false);
  ui.viewer = null;
  ui.scrollTo = null;
});

afterEach(() => {
  for (const component of mounted.splice(0)) unmount(component);
  document.body.replaceChildren();
  resetMermaidForTests();
});

const REPLY = "Intro with a [link](https://example.com).\n\n| Property | Value |\n|:--|--:|\n| `SameSite` | **Lax** |\n\n$$\nE = mc^2\n$$\n\nEnd.";

describe("MarkdownView", () => {
  test("visuals draw in place as figures with the figures' ids (owner, 2026-10-19)", async () => {
    const target = show(MarkdownView, { text: REPLY, messageId: "m1", sessionId: "s1", finished: true });
    const root = target.querySelector(".md");
    expect(root.querySelector(".md-chip")).toBeNull();
    const figures = [...root.querySelectorAll(".figure-view.inline")];
    expect(figures.map((f) => [f.dataset.kind, f.dataset.figure])).toEqual([
      ["table", "m1:0"],
      ["math", "m1:1"],
    ]);
    expect(figures.map((f) => f.dataset.figure)).toEqual(extractFigures("s1", "m1", REPLY).map((figure) => figure.id));
    expect(figures[0].querySelector(".caption-kind").textContent).toBe("TABLE");
    expect(figures[0].querySelector("table.md-table")).not.toBeNull();
    await idle();
    expect(figures[1].dataset.state).toBe("drawn");
    expect(figures[1].querySelector(".katex")).not.toBeNull();
    // Hiding or showing the right panel leaves them in place.
    setPanelHidden(true);
    flushSync();
    setPanelHidden(false);
    flushSync();
    expect(root.querySelectorAll(".figure-view.inline").length).toBe(2);
    expect(root.querySelector(".md-chip")).toBeNull();
  });

  test("a streaming reply: the table shows as it arrives; nodes before stay", async () => {
    const props = $state({ text: "First paragraph.\n\n| a |\n|---|\n| 1 |", messageId: "m2", sessionId: "s1", finished: false });
    const target = document.createElement("div");
    document.body.append(target);
    mounted.push(mount(MarkdownView, { target, props }));
    flushSync();
    const root = target.querySelector(".md");
    const first = root.firstChild;
    const shown = () => root.querySelectorAll(".figure-view.inline");
    expect(shown().length).toBe(1);
    expect(shown()[0].querySelector("table.md-table")).not.toBeNull();
    props.text += "\n\nMore text.";
    flushSync();
    props.text += "\n\nAnd more";
    flushSync();
    expect(root.firstChild).toBe(first);
    props.finished = true;
    flushSync();
    expect([...shown()].map((f) => f.dataset.figure)).toEqual(["m2:0"]);
  });

  test("links are guarded: no navigation, and nothing opens without a real click", () => {
    const target = show(MarkdownView, { text: REPLY, messageId: "m1", sessionId: "s1" });
    const anchor = target.querySelector(".md a[href]");
    const click = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, detail: 1 });
    anchor.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    expect(spies.openLink).not.toHaveBeenCalled();
  });

  test("decorations draw over their text and a click reaches their handler", () => {
    const handler = vi.fn();
    registerDecorationSource((messageId) => (messageId === "m-deco" ? [{ kind: "test-mark", start: 6, end: 11, data: { n: 1 } }] : []));
    registerDecorationHandler("test-mark", handler);
    const target = show(MarkdownView, { text: "Hello world, again.", messageId: "m-deco", sessionId: "s1" });
    const deco = target.querySelector(".md-deco");
    expect(deco.textContent).toBe("world");
    expect(deco.className).toBe("md-deco md-deco-test-mark");
    deco.click();
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0]).toMatchObject({ kind: "test-mark", start: 6, end: 11, data: { n: 1 } });
  });

  test("the safety corpus through the component: nothing unsafe, one link", () => {
    for (const [i, turn] of script.turns.entries()) {
      show(MarkdownView, { text: turn.steps.map((s) => s.text).join(""), messageId: `u${i}`, sessionId: "s1" });
    }
    const scan = scanPage(document);
    expect([scan.handlers_found, scan.scripts_found, scan.controls_found, scan.bad_hrefs_found]).toEqual([0, 0, 0, 0]);
    expect(scan.anchors.map((a) => a.href)).toEqual(["http://127.0.0.1:47321/page.html"]);
    // The hostile diagram and the math, then one valid diagram of every kind.
    expect(scan.figures.map((f) => f.kind)).toEqual(["diagram", "math", ...DIAGRAM_KINDS.map(() => "diagram")]);
  });

  test("leaving the page unmounts every visual", () => {
    const target = document.createElement("div");
    document.body.append(target);
    const component = mount(MarkdownView, { target, props: { text: REPLY, messageId: "m1", sessionId: "s1" } });
    flushSync();
    expect(target.querySelectorAll(".figure-view").length).toBe(2);
    unmount(component);
    flushSync();
    expect(target.querySelector(".figure-view, .md")).toBeNull();
  });
});

describe("VisualChip", () => {
  test("the renderer's chip look; Enter or a click reveals; no id, nothing", () => {
    const target = show(VisualChip, { figureId: "m1:3", kind: "diagram", title: "Sign-in flow" });
    const chip = target.querySelector(".md-chip");
    expect(chip.tagName).toBe("BUTTON");
    expect(chip.getAttribute("type")).toBe("button");
    expect([...chip.querySelectorAll("span")].map((s) => s.className)).toEqual(["md-chip-line", "md-chip-kind", "md-chip-dotbox", "md-chip-dot", "md-chip-title"]);
    expect(chip.textContent).toBe("DIAGRAMSign-in flow");
    chip.click();
    expect(spies.reveal).toHaveBeenCalledWith("m1:3");
    const streaming = show(VisualChip, { figureId: null, kind: "table", title: "a" }).querySelector(".md-chip");
    expect(streaming.hasAttribute("data-figure")).toBe(false);
    streaming.click();
    expect(spies.reveal).toHaveBeenCalledTimes(1);
  });

  test("a web picture is a link chip with its host: no target, not draggable", () => {
    const target = show(VisualChip, { figureId: "m1:0", kind: "picture", title: "cat", href: "https://cdn.example.com/cat.jpg", host: "cdn.example.com", inline: true });
    const chip = target.querySelector("a.md-chip.md-chip-inline");
    expect(chip.getAttribute("href")).toBe("https://cdn.example.com/cat.jpg");
    expect(chip.getAttribute("draggable")).toBe("false");
    expect(chip.getAttribute("rel")).toBe("noreferrer");
    expect(chip.hasAttribute("target")).toBe(false);
    expect(chip.querySelector(".md-chip-title").textContent).toBe("cdn.example.com");
  });
});

function figure(kind, fields) {
  return { id: `m9:${fields.index ?? 0}`, sessionId: "s9", messageId: "m9", index: 0, kind, title: "A figure", source: "", ...fields };
}

describe("FigureView", () => {
  test("the heading keeps its last two words together and shows the chip in the chat", () => {
    expect(keepLastWords("Check session cookie")).toBe("Check session cookie");
    expect(keepLastWords("Two words")).toBe("Two words");
    const target = show(FigureView, { figure: figure("table", { title: "Check session cookie", source: "| a |\n|---|\n| 1 |" }) });
    const heading = target.querySelector("figcaption .heading");
    expect(heading.textContent).toBe("Check session cookie");
    heading.click();
    expect(ui.scrollTo).toEqual({ sessionId: "s9", messageId: "m9", figureId: "m9:0" });
  });

  test("a table: the picture's table, cells with their marks, alignment as a class, links guarded", () => {
    const source = "| Name | Size |\n|:-:|--:|\n| `a.go` [docs](https://example.com) [bad](javascript:alert(1)) | **12** |";
    const target = show(FigureView, { figure: figure("table", { source }) });
    const view = target.querySelector(".figure-view");
    expect(view.dataset.state).toBe("drawn");
    const table = view.querySelector(".figure-table > table.md-table");
    expect([...table.querySelectorAll("th")].map((th) => [th.textContent, th.className])).toEqual([
      ["Name", "md-align-center"],
      ["Size", "md-align-right"],
    ]);
    const cell = table.querySelector("td");
    expect(cell.querySelector("code.md-icode").textContent).toBe("a.go");
    expect(cell.querySelector("a").getAttribute("href")).toBe("https://example.com");
    expect(cell.querySelector(".md-link-refused").textContent).toBe("bad");
    expect(table.querySelector("strong").textContent).toBe("12");
    const click = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, detail: 1 });
    cell.querySelector("a").dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
  });

  test("a diagram: its source until idle time, then the drawing", async () => {
    const fake = {
      initialize() {},
      render: async (id) => ({ svg: `<svg id="${id}" xmlns="http://www.w3.org/2000/svg" aria-roledescription="flowchart-v2" style="max-width: 120px;" height="40"><text>Sign in</text></svg>` }),
    };
    loadMermaid(async () => fake);
    const target = show(FigureView, { figure: figure("diagram", { title: "Sign-in flow", source: "flowchart LR\n  A[Sign in] --> B" }) });
    const view = target.querySelector(".figure-view");
    expect(view.dataset.state).toBe("pending");
    expect(view.querySelector(".figure-source").textContent).toBe("flowchart LR\n  A[Sign in] --> B");
    await idle();
    expect(view.dataset.state).toBe("drawn");
    const svg = view.querySelector(".figure-body svg");
    expect(svg.getAttribute("width")).toBe("100%");
    expect(svg.hasAttribute("height")).toBe(false);
    expect(svg.style.maxWidth).toBe("");
    // The safety scan names the figure by its title and reads the kind its
    // drawing says it is, so the window check can tell each diagram apart.
    expect(scanPage(document).figures).toEqual([{ figure: "m9:0", kind: "diagram", title: "Sign-in flow", state: "drawn", svg: "flowchart-v2", inline: false }]);
  });

  test("a diagram that cannot be drawn shows its source and the line", async () => {
    loadMermaid(async () => ({ initialize() {}, render: async () => Promise.reject(new Error("parse error")) }));
    const target = show(FigureView, { figure: figure("diagram", { source: "not a diagram" }), inline: true });
    await idle();
    const view = target.querySelector(".figure-view");
    expect(view.dataset.state).toBe("fallback");
    expect(view.querySelector(".figure-source").textContent).toBe("not a diagram");
    expect(view.querySelector(".figure-failed").textContent).toBe(DIAGRAM_FAILED);
    expect(scanPage(document).figures.map(({ title, state, svg }) => ({ title, state, svg }))).toEqual([{ title: "A figure", state: "fallback", svg: null }]);
  });

  test("math: KaTeX display in idle time; while streaming only the source", async () => {
    const live = show(FigureView, { figure: figure("math", { source: "x^2" }), inline: true, streaming: true }).querySelector(".figure-view");
    expect(live.dataset.state).toBe("source");
    expect(live.querySelector(".caption-kind").textContent).toBe("MATH");
    expect(live.querySelector("figcaption .heading")).toBeNull();
    const view = show(FigureView, { figure: figure("math", { source: "\\frac{a}{b}" }) }).querySelector(".figure-view");
    await idle();
    expect(view.dataset.state).toBe("drawn");
    expect(view.querySelector(".figure-math .katex-display")).not.toBeNull();
    expect(live.dataset.state).toBe("source");
  });

  test("a data: picture draws; a click in place opens the viewer, a drag does not", () => {
    const src = "data:image/png;base64,iVBORw0KGgo=";
    const target = show(FigureView, { figure: figure("picture", { title: "shot", source: src, src }) });
    const img = target.querySelector("img.figure-picture");
    expect(img.getAttribute("src")).toBe(src);
    expect(img.getAttribute("alt")).toBe("shot");
    expect(img.tabIndex).toBe(0);
    const at = (type, x) => new MouseEvent(type, { bubbles: true, cancelable: true, button: 0, clientX: x, clientY: 5, detail: 1 });
    img.dispatchEvent(at("mousedown", 10));
    img.dispatchEvent(at("click", 30));
    expect(ui.viewer).toBeNull();
    img.dispatchEvent(at("mousedown", 10));
    img.dispatchEvent(at("click", 11));
    expect(ui.viewer).toEqual({ src, name: "shot" });
  });

  test("a file picture is read through file_read; a file that is not a picture shows the line", async () => {
    spies.fileRead.mockResolvedValueOnce({ mime: "image/png", base64: "AAAA" });
    const target = show(FigureView, { figure: figure("picture", { title: "shot", source: "./shots/a.png", src: "./shots/a.png" }) });
    await idle();
    expect(spies.fileRead).toHaveBeenCalledWith("./shots/a.png", expect.any(String), 5 * 1024 * 1024);
    expect(target.querySelector("img").getAttribute("src")).toBe("data:image/png;base64,AAAA");
    spies.fileRead.mockResolvedValueOnce({ mime: "text/plain", base64: "AAAA" });
    const other = show(FigureView, { figure: figure("picture", { title: "notes", source: "notes.txt", src: "notes.txt" }) });
    await idle();
    expect(other.querySelector("img")).toBeNull();
    expect(other.querySelector(".figure-failed").textContent).toBe(PICTURE_FAILED);
    expect(other.querySelector(".figure-view").dataset.state).toBe("fallback");
  });

  test("a web picture is a link chip with its host", () => {
    const src = "https://cdn.example.com/p/cat.jpg";
    const target = show(FigureView, { figure: figure("picture", { title: "cat", source: src, src, host: "cdn.example.com" }) });
    const view = target.querySelector(".figure-view");
    expect(view.dataset.state).toBe("link");
    expect(view.querySelector("img")).toBeNull();
    expect(view.querySelector("a.md-chip").getAttribute("href")).toBe(src);
    expect(view.querySelector(".md-chip-title").textContent).toBe("cdn.example.com");
  });
});

describe("Chip", () => {
  test("names: the last part of a path, cut to 15 characters with …", () => {
    expect(chipName("/home/me/screenshot-2026-10-18.png")).toBe("screenshot-202…");
    expect(chipName("exactly15chars!")).toBe("exactly15chars!");
    expect(chipName("short.txt")).toBe("short.txt");
    expect(Array.from(chipName("😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀")).length).toBe(15);
  });

  test("a sent message's chip: icon by kind, the name, no remove control", () => {
    const target = show(Chip, { chip: { kind: "file", name: "notes/plan.md", mime: "text/markdown", viewable: false }, removable: false });
    const chip = target.querySelector(".chip");
    expect(chip.querySelector(".icon")).not.toBeNull();
    expect(chip.querySelector(".name").textContent).toBe("plan.md");
    expect(chip.querySelector("button, .remove, .trail")).toBeNull();
  });

  test("removable: the ✕ removes; in delete mode the digit takes its place", () => {
    const onremove = vi.fn();
    const props = $state({ chip: { kind: "picture", name: "a.png", mime: "image/png", viewable: true, src: "data:image/png;base64,AA" }, removable: true, number: null, onremove });
    const target = document.createElement("div");
    document.body.append(target);
    mounted.push(mount(Chip, { target, props }));
    flushSync();
    const remove = target.querySelector("button.remove");
    expect(remove.getAttribute("aria-label")).toBe("Remove a.png");
    remove.click();
    expect(onremove).toHaveBeenCalledWith(props.chip);
    expect(ui.viewer).toBeNull();
    props.number = 3;
    flushSync();
    expect(target.querySelector("button.remove")).toBeNull();
    expect(target.querySelector(".trail.number").textContent).toBe("3");
  });

  test("a kept skill shows SKILL before its name", () => {
    const chip = show(Chip, { chip: { kind: "skill", name: "deploy", mime: "", viewable: false } }).querySelector(".chip");
    expect([...chip.querySelector(".label").children].map((el) => el.classList[0])).toEqual(["icon", "skill", "name"]);
    expect(chip.querySelector(".skill").textContent).toBe("Skill");
  });

  test("a click in place on a picture opens the viewer; a drag or another kind does not", () => {
    const picture = { kind: "picture", name: "a.png", mime: "image/png", viewable: true, src: "data:image/png;base64,AA" };
    const chip = show(Chip, { chip: picture }).querySelector(".chip .label");
    expect(chip.getAttribute("role")).toBe("button");
    expect(chip.tabIndex).toBe(0);
    const at = (type, x) => new MouseEvent(type, { bubbles: true, cancelable: true, button: 0, clientX: x, clientY: 3, detail: 1 });
    chip.dispatchEvent(at("mousedown", 5));
    chip.dispatchEvent(at("click", 25));
    expect(ui.viewer).toBeNull();
    chip.dispatchEvent(at("mousedown", 5));
    chip.dispatchEvent(at("click", 6));
    expect(ui.viewer).toEqual({ src: picture.src, name: "a.png", mime: "image/png" });
    ui.viewer = null;
    const file = show(Chip, { chip: { kind: "file", name: "a.txt", mime: "text/plain", viewable: false } }).querySelector(".chip .label");
    file.dispatchEvent(at("mousedown", 5));
    file.dispatchEvent(at("click", 5));
    expect(ui.viewer).toBeNull();
    expect(file.hasAttribute("tabindex")).toBe(false);
  });
});
