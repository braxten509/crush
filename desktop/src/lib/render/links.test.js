// 1.7 item 3 and review finding 5: one URL check, and every way an anchor can
// act caught by guardLinks.
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

vi.mock("../engine/commands.js", () => ({ open_link: vi.fn() }));

import { open_link } from "../engine/commands.js";
import { conn } from "../stores/connection.svelte.js";
import { ui } from "../stores/ui.svelte.js";
import { guardLinks, linkGuard, makeAnchor, openLink, safeHref } from "./links.js";

// The spellings of desktop-design 2.9 (review finding 5) and their friends.
const REFUSED = [
  "\tjavascript:alert(1)",
  " javascript:alert(1)",
  "JAVASCRIPT:alert(1)",
  "java\nscript:alert(1)",
  "java\tscript:alert(1)",
  "javascript:alert(1)",
  "\u0000javascript:alert(1)",
  "vbscript:x",
  "blob:x",
  "data:text/html,x",
  "data:image/png;base64,AAAA",
  "about:blank",
  "tauri://localhost/x",
  "main.go:10",
  "",
  "   ",
];

const KEPT = [
  "http://example.com/a?b#c",
  "https://example.com",
  "HTTPS://example.com",
  "mailto:a@example.com",
  "file:///etc/hosts",
  "./a.go",
  "../b/c.md",
  "/abs/path.txt",
  "~/notes.md",
  "src/main.go:10:2",
  "#heading",
];

describe("safeHref", () => {
  for (const target of REFUSED) {
    test(`refuses ${JSON.stringify(target)}`, () => expect(safeHref(target)).toBeNull());
  }
  for (const target of KEPT) {
    test(`keeps ${JSON.stringify(target)} as written`, () => expect(safeHref(target)).toBe(target));
  }
});

describe("makeAnchor", () => {
  test("href as written, not draggable, no referrer, no target", () => {
    const a = makeAnchor(document, "~/x.md:3:1");
    expect(a.getAttribute("href")).toBe("~/x.md:3:1");
    expect(a.getAttribute("draggable")).toBe("false");
    expect(a.getAttribute("rel")).toBe("noreferrer");
    expect(a.hasAttribute("target")).toBe(false);
  });
});

function mouse(type, init = {}) {
  return new MouseEvent(type, { bubbles: true, cancelable: true, button: 0, detail: 1, clientX: 10, clientY: 10, ...init });
}

describe("guardLinks", () => {
  let root;
  let a;
  let opened;
  let remove;
  const trusted = { trust: () => true };

  beforeEach(() => {
    root = document.createElement("div");
    a = makeAnchor(document, "https://example.com/x");
    a.textContent = "a link";
    const p = document.createElement("p");
    p.append("before ", a, " after");
    root.append(p);
    document.body.append(root);
    opened = [];
    remove = guardLinks(root, (href) => opened.push(href), trusted);
  });

  afterEach(() => {
    remove();
    root.remove();
    window.getSelection().removeAllRanges();
  });

  test("a plain left click opens once, and the page never follows the link", () => {
    a.dispatchEvent(mouse("mousedown"));
    const click = mouse("click");
    a.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    expect(opened).toEqual(["https://example.com/x"]);
  });

  test("a click on the anchor's inner text counts", () => {
    const inner = document.createElement("strong");
    inner.textContent = "bold";
    a.append(inner);
    inner.dispatchEvent(mouse("click"));
    expect(opened).toEqual(["https://example.com/x"]);
  });

  test("a script's click (not trusted) opens nothing", () => {
    remove();
    remove = guardLinks(root, (href) => opened.push(href));
    const click = mouse("click");
    a.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    a.click();
    expect(opened).toEqual([]);
  });

  test("a drag between press and release opens nothing", () => {
    a.dispatchEvent(mouse("mousedown", { clientX: 10 }));
    a.dispatchEvent(mouse("mousemove", { clientX: 210, buttons: 1 }));
    a.dispatchEvent(mouse("click", { clientX: 210 }));
    expect(opened).toEqual([]);
    // Released in place after moving away and back still counts as a drag.
    a.dispatchEvent(mouse("mousedown", { clientX: 10 }));
    a.dispatchEvent(mouse("mousemove", { clientX: 60, buttons: 1 }));
    a.dispatchEvent(mouse("click", { clientX: 10 }));
    expect(opened).toEqual([]);
  });

  test("a double click opens once", () => {
    a.dispatchEvent(mouse("mousedown"));
    a.dispatchEvent(mouse("click", { detail: 1 }));
    a.dispatchEvent(mouse("mousedown", { detail: 2 }));
    a.dispatchEvent(mouse("click", { detail: 2 }));
    expect(opened).toEqual(["https://example.com/x"]);
  });

  test("a click that ends a text selection opens nothing", () => {
    const range = document.createRange();
    range.selectNodeContents(a);
    window.getSelection().addRange(range);
    a.dispatchEvent(mouse("click"));
    expect(opened).toEqual([]);
  });

  test("the middle and the third buttons open nothing", () => {
    for (const button of [1, 2]) {
      const down = mouse("mousedown", { button });
      a.dispatchEvent(down);
      const aux = mouse("auxclick", { button });
      a.dispatchEvent(aux);
      expect(aux.defaultPrevented).toBe(true);
      if (button === 1) expect(down.defaultPrevented).toBe(true);
      const click = mouse("click", { button });
      a.dispatchEvent(click);
      expect(click.defaultPrevented).toBe(true);
    }
    expect(opened).toEqual([]);
  });

  test("dragstart on the anchor is cancelled", () => {
    const drag = new Event("dragstart", { bubbles: true, cancelable: true });
    a.dispatchEvent(drag);
    expect(drag.defaultPrevented).toBe(true);
    // And the click that may follow opens nothing.
    a.dispatchEvent(mouse("click"));
    expect(opened).toEqual([]);
  });

  test("dragging selected text that holds a link is cancelled", () => {
    const range = document.createRange();
    range.selectNodeContents(root);
    window.getSelection().addRange(range);
    const drag = new Event("dragstart", { bubbles: true, cancelable: true });
    root.firstChild.firstChild.dispatchEvent(drag);
    expect(drag.defaultPrevented).toBe(true);
  });

  test("Enter on the focused anchor opens once; the browser's echo click does not", () => {
    const key = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    a.dispatchEvent(key);
    expect(key.defaultPrevented).toBe(true);
    a.dispatchEvent(mouse("click", { detail: 0 }));
    expect(opened).toEqual(["https://example.com/x"]);
    // A held key's repeats open nothing more; Ctrl+Enter is the router's.
    a.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", repeat: true, bubbles: true, cancelable: true }));
    const ctrl = new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true, cancelable: true });
    a.dispatchEvent(ctrl);
    expect(ctrl.defaultPrevented).toBe(false);
    expect(opened).toEqual(["https://example.com/x"]);
  });

  test("the web view's own menu is stopped on links, and the app's menu still hears it", () => {
    let heard = 0;
    document.body.addEventListener("contextmenu", () => heard++);
    const menu = mouse("contextmenu", { button: 2 });
    a.dispatchEvent(menu);
    expect(menu.defaultPrevented).toBe(true);
    expect(heard).toBe(1);
  });

  test("events away from anchors are left alone", () => {
    const click = mouse("click");
    root.firstChild.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(false);
    const key = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    root.firstChild.dispatchEvent(key);
    expect(key.defaultPrevented).toBe(false);
    expect(opened).toEqual([]);
  });

  test("a guard inside a guard opens once", () => {
    const outer = [];
    const removeOuter = guardLinks(document.body, (href) => outer.push(href), trusted);
    a.dispatchEvent(mouse("click"));
    removeOuter();
    expect(opened).toEqual(["https://example.com/x"]);
    expect(outer).toEqual([]);
  });

  test("removing the guard removes every listener", () => {
    remove();
    a.setAttribute("href", "#unguarded"); // the page model follows it: keep it on this page
    const click = mouse("click");
    a.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(false);
    remove = () => {};
  });

  test("linkGuard is the same guard as a Svelte action", () => {
    remove();
    const action = linkGuard(root, (href) => opened.push(href));
    const click = mouse("click");
    a.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    action.destroy();
    a.setAttribute("href", "#unguarded");
    const after = mouse("click");
    a.dispatchEvent(after);
    expect(after.defaultPrevented).toBe(false);
    remove = () => {};
  });
});

describe("openLink", () => {
  beforeEach(() => {
    open_link.mockReset();
    conn.project = { cwd: "/tmp/home/proj/sub", path: "/tmp/home/proj", dataDir: "", name: "proj" };
    ui.notice = null;
  });

  test("opens with the folder the AI works in", async () => {
    open_link.mockResolvedValue(null);
    expect(await openLink("src/a.go:3")).toBe(true);
    expect(open_link).toHaveBeenCalledWith("src/a.go:3", "/tmp/home/proj");
  });

  test("falls back to the window's folder", async () => {
    open_link.mockResolvedValue(null);
    conn.project = { cwd: "/tmp/home/proj/sub", path: "", dataDir: "", name: "" };
    await openLink("x.md");
    expect(open_link).toHaveBeenCalledWith("x.md", "/tmp/home/proj/sub");
  });

  test("a refusal shows as an error notice", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    open_link.mockRejectedValue({ code: "refused", message: 'cannot open links of type "ftp"' });
    expect(await openLink("ftp://x")).toBe(false);
    expect(ui.notice).toMatchObject({ kind: "error", text: 'cannot open links of type "ftp"' });
    error.mockRestore();
  });
});
