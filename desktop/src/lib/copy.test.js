// @vitest-environment jsdom
// Copying (CHAT-012, CHAT-017, CHAT-030, CHAT-087 to CHAT-091): a reply's
// visible text with the Go cases (copy_source_test.go), selections of a
// reply as plain text (no Markdown marks; code exactly as it is), the middle of a long selection read from the items, never
// the page, and the clipboard's failure text.
import { beforeEach, describe, expect, test, vi } from "vitest";

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));

const { invoke } = await import("@tauri-apps/api/core");
const { copyBlocks, copySelection, copyVisible, itemCopy, messageItemFor, quoteLines, registerItemCopy, wholeText, withQuote, writeClipboard } = await import("./copy.js");
const { lastClipboardWrite } = await import("./engine/commands.js");
const { menuItems } = await import("./menu.js");
await import("./plugins/timeline.js");
const { renderMarkdown } = await import("./render/markdown.js");
const { PLAN_READY_MARKER, PLAN_START_MARKER } = await import("./render/plan.js");
const { userTextInto } = await import("./render/usertext.js");
const { ui } = await import("./stores/ui.svelte.js");

describe("a reply's visible text (CHAT-030; copy_source_test.go)", () => {
  test("hidden markup, plan markers and HTML are never copied", () => {
    const source = `alpha<!-- secret -->omega\n\n${PLAN_START_MARKER}\n\n\`\`\`sh\ns h\n\`\`\`\n\n${PLAN_READY_MARKER}`;
    expect(copyBlocks(source)).toEqual(["alphaomega", "s h\n"]);
  });

  test.each([
    ["Send **mail** to `no-reply@accounts.google.com`.", "Send mail to no-reply@accounts.google.com."],
    ["Use ``a`b`` here.", "Use a`b here."],
    ["Keep \\`literal\\` marks.", "Keep `literal` marks."],
    ["```sh\necho `date`\n```", "echo `date`\n"],
    ["Started a wait (PID `1438360`) with `sleep 10` and `nohup`.", "Started a wait (PID 1438360) with sleep 10 and nohup."],
  ])("%j copies as %j", (source, want) => {
    expect(copyVisible(source)).toBe(want);
  });

  test.each([
    "bash /home/example/Documents/forecaster-ui-public/scripts/deployment/install-worker-key.sh /home/example/.ssh/forecaster-scraper-deploy.ABCDEFGH/id_ed25519",
    "bash /home/example/Documents/forecaster-ui-public/scripts/deployment/install-worker-key.sh \\\n  /home/example/.ssh/forecaster-scraper-deploy.ABCDEFGH/id_ed25519",
    "printf '%s\\n' 'a   b'\n    printf '%s\\n' 'second line'\n\n\tprintf '%s\\n' 'tabbed line'",
    `printf '%s' '路径/é/👩‍💻/${"long".repeat(30)}'`,
    `${"x".repeat(50)}\n${"y".repeat(50)}`,
    "s h",
    "    s  h",
  ])("code keeps its source: %j", (code) => {
    expect(copyVisible(`\`\`\`sh\n${code}\n\`\`\``)).toBe(`${code}\n`);
  });

  test("blocks are joined by a blank line; soft breaks are spaces, hard ones newlines", () => {
    expect(copyVisible("# Title\n\nOne\ntwo  \nthree\n\n- a\n- **b**\n\n> quoted &amp; kept\n\n[label](https://example.com) <https://x.example>")).toBe(
      "Title\n\nOne two\nthree\n\na\n\nb\n\nquoted & kept\n\nlabel https://x.example",
    );
  });
});

// -- Selections --

function frame(key, kind, fill) {
  const el = document.createElement("div");
  el.setAttribute("role", "article");
  el.dataset.key = key;
  el.dataset.kind = kind;
  const byline = document.createElement("p");
  byline.textContent = kind === "user" ? "YOU 10:42" : kind === "reply" ? "CLAUDE 10:43" : "";
  if (byline.textContent) el.append(byline);
  fill(el);
  return el;
}

const replyFrame = (item) => frame(item.key, "reply", (el) => el.append(renderMarkdown(item.text, { messageId: item.key })));
const userFrame = (item) =>
  frame(item.key, "user", (el) => {
    const text = document.createElement("div");
    text.className = "text";
    userTextInto(text, item.text);
    el.append(text);
  });
const noticeFrame = (item) =>
  frame(item.key, "notice", (el) => {
    const p = document.createElement("p");
    p.textContent = wholeText(item);
    el.append(p);
  });

function page(items, mounted = () => true) {
  const root = document.createElement("div");
  for (const item of items) {
    if (!mounted(item)) continue;
    root.append(item.kind === "reply" ? replyFrame(item) : item.kind === "user" ? userFrame(item) : noticeFrame(item));
  }
  document.body.replaceChildren(root);
  return root;
}

// The text node holding needle inside the frame of key, and the offset of needle in it.
function find(root, key, needle) {
  const el = [...root.querySelectorAll("[data-key]")].find((x) => x.dataset.key === key);
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const at = n.data.indexOf(needle);
    if (at >= 0) return [n, at];
  }
  throw new Error(`no ${needle} in ${key}`);
}

function select(root, [a, ai], [b, bi]) {
  const range = document.createRange();
  range.setStart(a, ai);
  range.setEnd(b, bi);
  return range;
}

const reply = (key, text) => ({ kind: "reply", key, messageId: key, version: 1, text, finished: true, plan: "none" });
const user = (key, text) => ({ kind: "user", key, messageId: key, version: 1, text, chips: [] });

describe("a selection copies plain text (CHAT-088, CHAT-089)", () => {
  test("inline code copies without its backticks, whole or in part (CHAT-017)", () => {
    const items = [reply("r1", "Started a wait (PID `1438360`) with `sleep 10`.")];
    const root = page(items);
    const [node, at] = find(root, "r1", "1438360");
    expect(copySelection(select(root, [node, at], [node, at + 7]), items)).toBe("1438360");
    expect(copySelection(select(root, [node, at + 1], [node, at + 6]), items)).toBe("43836");
  });

  test.each([
    ["trailing word space", "foo bar", "foo ", 0, 4],
    ["indentation only", "    foo", "  ", 0, 2],
    ["trailing spaces at the end", "foo  ", "foo  ", 0, 5],
    ["blank line indentation", "foo\n    \nbar", "    ", 4, 8],
  ])("code: %s", (_name, code, want, from, to) => {
    const items = [reply("r1", `\`\`\`\n${code}\n\`\`\``)];
    const root = page(items);
    const [node, at] = find(root, "r1", code.slice(0, 3));
    expect(copySelection(select(root, [node, at + from], [node, at + to]), items)).toBe(want);
  });

  test("a tab copies as the tab it was", () => {
    const items = [reply("r1", "```\n\tfoo\n```")];
    const root = page(items);
    const [node, at] = find(root, "r1", "foo");
    expect(copySelection(select(root, [node, 0], [node, at]), items)).toBe("\t");
  });

  test("a selection over nothing copies nothing", () => {
    const items = [reply("r1", "foo")];
    const root = page(items);
    const [node, at] = find(root, "r1", "foo");
    expect(copySelection(select(root, [node, at], [node, at]), items)).toBe("");
  });

  test("a wrapped paragraph copies without an inserted newline (CHAT-089)", () => {
    const long = "word ".repeat(120).trim();
    const items = [reply("r1", long)];
    const root = page(items);
    const [node] = find(root, "r1", "word");
    expect(copySelection(select(root, [node, 0], [node, node.data.length]), items)).toBe(long);
  });

  test("across a code span and a list the marks are left out", () => {
    const items = [reply("r1", "Run `make` first:\n\n- one\n- *two*")];
    const root = page(items);
    const start = find(root, "r1", "Run");
    const end = find(root, "r1", "two");
    expect(copySelection(select(root, start, [end[0], end[1] + 3]), items)).toBe("Run make first:\n\none\n\ntwo");
  });

  test("a selection from inside a code block on into the text keeps the code as it is", () => {
    const items = [reply("r1", "```sh\n  make all\n```\n\nThen **run** it.")];
    const root = page(items);
    const start = find(root, "r1", "make");
    const end = find(root, "r1", "it.");
    expect(copySelection(select(root, start, [end[0], end[1] + 3]), items)).toBe("make all\n\nThen run it.");
  });

  test("a user message copies as typed", () => {
    const items = [user("u1", "  <b>x</b> *y* `z`\ttab  ")];
    const root = page(items);
    const [node] = find(root, "u1", "<b>");
    expect(copySelection(select(root, [node, 0], [node, node.data.length]), items)).toBe("<b>x</b> *y* `z`\ttab");
  });
});

describe("the middle of a long selection is never read from the page (CHAT-087)", () => {
  const items = [];
  for (let i = 0; i < 40; i++) {
    items.push(user(`u${i}`, `Question ${i} with \`code\` and *marks*`));
    items.push(reply(`r${i}`, `Answer **${i}**:\n\n- first\n- second\n\n\`\`\`sh\necho ${i}\n\`\`\``));
    if (i % 10 === 0) items.push({ kind: "notice", key: `notice:${i}`, messageId: `n${i}`, version: 1, notice: "task", state: "done", name: `job${i}`, text: `Task job${i} finished` });
  }
  const first = items[1];
  const last = items[items.length - 1];

  function copyWith(mounted) {
    const root = page(items, mounted);
    const start = find(root, first.key, "Answer");
    const end = find(root, last.key, `echo`);
    return copySelection(select(root, [start[0], start[1] + 3], [end[0], end[1] + 4]), items);
  }

  test("only the two ends mounted gives the same text as every item mounted", () => {
    const all = copyWith(() => true);
    const ends = copyWith((item) => item === first || item === last);
    expect(ends).toBe(all);
    expect(all.startsWith("wer 0:")).toBe(true);
    expect(all).not.toContain("**");
    expect(all.endsWith("echo")).toBe(true);
    expect(all).toContain("Question 1 with `code` and *marks*");
    expect(all).toContain("Task job10 finished");
    expect(all.split("\n\n").length).toBeGreaterThan(80);
  });
});

describe("a selection across an Agent tool (CHAT-056, CHAT-087)", () => {
  test("the tool's copy text comes along, its row mounted or not", async () => {
    await import("./plugins/tools.js");
    const call = { id: "c1", name: "agent", input: '{"prompt":"find the bug"}', result: { tool_call_id: "c1", content: "Found it.", is_error: false } };
    const items = [reply("r0", "Before the agent."), { kind: "tool", key: "tool:c1", messageId: "r0", version: 1, call }, reply("r1", "After the agent.")];
    const want = "## Agent Tool Call\n\n### Parameters:\n\n**Task:**\nfind the bug\n\n### Result:\n\n```markdown\nFound it.\n```";
    const copyWith = (mounted) => {
      const root = page(items, mounted);
      const start = find(root, "r0", "Before");
      const end = find(root, "r1", "agent");
      return copySelection(select(root, start, [end[0], end[1] + 5]), items);
    };
    for (const mounted of [() => true, (item) => item.kind !== "tool"]) {
      expect(copyWith(mounted)).toBe(`Before the agent.\n\n${want}\n\nAfter the agent`);
    }
  });
});

describe("c/y on an item (CHAT-012, CHAT-030, CHAT-100)", () => {
  test("a user message copies its text as sent; a reply its visible text", () => {
    expect(itemCopy(user("u1", "  hello `x`  "))).toEqual({ text: "  hello `x`  ", notice: "Message copied to clipboard" });
    expect(itemCopy(reply("r1", "Use **this** `code`."))).toEqual({ text: "Use this code.", notice: "Message copied to clipboard" });
  });

  test("a notice copies the message it came from", () => {
    const item = { kind: "notice", key: "notice:m1", notice: "task", state: "done", name: "build", text: "Task build finished" };
    const stored = "<crush-task-result><name>build</name><status>completed</status></crush-task-result>";
    expect(itemCopy(item, { parts: [{ type: "text", data: { text: stored } }] }).text).toBe(stored);
  });

  test("tools copy in their registered format; without one nothing is copied", () => {
    const group = { kind: "tools", key: "group:a", calls: [] };
    expect(itemCopy(group)).toBeNull();
    registerItemCopy("tools", () => ({ text: "## Bash Tool Call", notice: "Tool content copied to clipboard" }));
    expect(itemCopy(group)).toEqual({ text: "## Bash Tool Call", notice: "Tool content copied to clipboard" });
    expect(itemCopy({ kind: "footer", key: "footer:a", text: "x", saved: "" })).toBeNull();
  });
});

describe("the right-click menu (4.2, D14)", () => {
  test("Quote in reply puts \"> \" before every line, then a blank line", () => {
    expect(quoteLines("  first\nsecond\r\n\nthird  ")).toBe("> first\n> second\n> \n> third\n\n");
    expect(quoteLines(" \n ")).toBe("");
  });

  test("a quote starts a block of its own after typed text", () => {
    expect(withQuote("", "> a\n\n")).toBe("> a\n\n");
    expect(withQuote("Look:", "> a\n\n")).toBe("Look:\n\n> a\n\n");
    expect(withQuote("Look:\n", "> a\n\n")).toBe("Look:\n\n> a\n\n");
    expect(withQuote("Look:\n\n", "> a\n\n")).toBe("Look:\n\n> a\n\n");
  });

  test("a reply's banner, footer or status stands for the reply; tool rows are not messages", () => {
    const r = reply("r1", "Hi");
    const items = [user("u1", "q"), r, { kind: "banner", key: "banner:r1", messageId: "r1" }, { kind: "footer", key: "footer:r1", messageId: "r1" }, { kind: "tools", key: "group:c1", messageId: "r1" }, { kind: "status", key: "status:waiting:x", messageId: "" }];
    expect(messageItemFor(items[2], items)).toBe(r);
    expect(messageItemFor(items[3], items)).toBe(r);
    expect(messageItemFor(items[4], items)).toBeNull();
    expect(messageItemFor(items[5], items)).toBeNull();
    expect(messageItemFor(items[0], items)).toBe(items[0]);
  });

  test("three entries in order; Copy and Quote need a timeline selection; Copy whole message any message", () => {
    const items = [user("u1", "hello"), reply("r1", "Use **this**."), { kind: "tools", key: "group:c1", messageId: "r1", calls: [] }];
    const none = menuItems({ sessionId: "s", key: "r1", item: items[1], message: null, range: null, text: "", items });
    expect(none.map((e) => [e.label, e.disabled, e.hint])).toEqual([
      ["Copy", true, ""],
      ["Quote in reply", true, ""],
      ["Copy whole message", false, ""],
    ]);
    const range = document.createRange();
    const some = menuItems({ sessionId: "s", key: "group:c1", item: items[2], message: null, range, text: "words", items });
    expect(some.map((e) => e.disabled)).toEqual([false, false, true]);
  });

  test("Copy copies the selection's source; Copy whole message the message's text", async () => {
    invoke.mockReset();
    invoke.mockResolvedValue(null);
    const items = [reply("r1", "Use **this** `code`.")];
    const entries = menuItems({ sessionId: "s", key: "r1", item: items[0], message: null, range: document.createRange(), text: "`code`", items });
    entries[0].run();
    await Promise.resolve();
    expect(invoke).toHaveBeenLastCalledWith("clipboard_write", { text: "`code`" });
    entries[2].run();
    await Promise.resolve();
    expect(invoke).toHaveBeenLastCalledWith("clipboard_write", { text: "Use this code." });
  });
});

describe("the clipboard (CHAT-090)", () => {
  beforeEach(() => {
    invoke.mockReset();
    ui.notice = null;
  });

  test("a copy shows the row's notice", async () => {
    invoke.mockResolvedValue(null);
    expect(await writeClipboard("hello", "Message copied to clipboard")).toBe(true);
    expect(invoke).toHaveBeenCalledWith("clipboard_write", { text: "hello" });
    expect(lastClipboardWrite().text).toBe("hello");
    expect(ui.notice).toMatchObject({ kind: "info", text: "Message copied to clipboard" });
  });

  test("a failed copy shows the command's message", async () => {
    invoke.mockRejectedValue({ code: "clipboard_failed", message: "Failed to copy to clipboard" });
    expect(await writeClipboard("hello", "Message copied to clipboard")).toBe(false);
    expect(ui.notice).toMatchObject({ kind: "warn", text: "Failed to copy to clipboard" });
  });
});
