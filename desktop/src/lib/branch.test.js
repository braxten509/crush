// @vitest-environment jsdom
// Branch rules (desktop-design 3.1, 3.2): the title and the draft of a new
// branch, cuts as UTF-16 ranges of the stored text through the real
// renderers' source maps, selections that end inside markup, where a cut can
// start, overlaps (touching ranges are fine) and the reasons a selection
// cannot branch.
import { describe, expect, test, vi } from "vitest";

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));

const { HINTS, TITLE_MAX, branchErrorText, branchTitle, canBranch, cutOfRange, overlaps, quoteDraft, quoteInReply, selectionToCut } = await import("./branch.js");
const { quoteLines } = await import("./copy.js");
const { renderMarkdown } = await import("./render/markdown.js");
const { sourceMapOf } = await import("./render/sourcemap.js");
const { userTextInto } = await import("./render/usertext.js");

const LONE_SURROGATE = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/;

function userRoot(stored) {
  const el = document.createElement("div");
  el.className = "text";
  userTextInto(el, stored);
  document.body.replaceChildren(el);
  return el;
}

function replyRoot(stored) {
  const el = renderMarkdown(stored, { messageId: "r1" });
  document.body.replaceChildren(el);
  return el;
}

function textNodes(root) {
  const out = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) out.push(node);
  return out;
}

// [node, offset] where needle starts (or ends, with atEnd) in the drawn text.
function point(root, needle, atEnd = false) {
  for (const node of textNodes(root)) {
    const at = node.data.indexOf(needle);
    if (at >= 0) return [node, atEnd ? at + needle.length : at];
  }
  throw new Error(`no ${JSON.stringify(needle)} drawn`);
}

// The drawn range from the start of `from` to the end of `to` (to defaults to from).
function drag(root, from, to = from) {
  const range = document.createRange();
  range.setStart(...point(root, from));
  range.setEnd(...point(root, to, true));
  return range;
}

const cutOf = (root, from, to, messageId = "m1") => selectionToCut({ messageId, range: drag(root, from, to) }, sourceMapOf(root));

describe("the branch's title and first draft (3.1, 3.2)", () => {
  test("white space collapses to one line; a short title stays whole", () => {
    expect(branchTitle("  Fix the\n\tlogin   bug \n")).toBe("Fix the login bug");
  });

  test(`${TITLE_MAX} characters stay; one more becomes 33 and an ellipsis`, () => {
    const exact = "a".repeat(TITLE_MAX);
    expect(branchTitle(exact)).toBe(exact);
    const over = `${"b".repeat(33)}cd`;
    expect(branchTitle(over)).toBe(`${"b".repeat(33)}…`);
    expect([...branchTitle(over)]).toHaveLength(TITLE_MAX);
  });

  test("a 60-character selection across a line break: the title is cut, the draft is not (B2)", () => {
    const words = "The session store keeps every chat it\nhas loaded in one map.";
    expect(words).toHaveLength(60);
    const flat = "The session store keeps every chat it has loaded in one map.";
    expect(branchTitle(words)).toBe(`${flat.slice(0, 33)}…`);
    expect(quoteDraft(words)).toBe(`> ${flat}\n\n`);
    expect(quoteDraft(words)).not.toContain("…");
  });

  test("the title is cut between characters as people count them, never inside an emoji", () => {
    const words = `${"a".repeat(32)}👩‍💻🚀 and more`;
    const title = branchTitle(words);
    expect(title).toBe(`${"a".repeat(32)}👩‍💻…`);
    expect(LONE_SURROGATE.test(title)).toBe(false);
    // 34 characters, 37 UTF-16 units: whole.
    expect(branchTitle(`${"x".repeat(31)}🚀🚀🚀`)).toBe(`${"x".repeat(31)}🚀🚀🚀`);
  });

  test("Quote in reply quotes every line, the same rule as copy (D14)", () => {
    expect(quoteInReply("one\n  two\r\nthree")).toBe("> one\n>   two\n> three\n\n");
    expect(quoteInReply("one\ntwo")).toBe(quoteLines("one\ntwo"));
    expect(quoteDraft("one\ntwo")).toBe("> one two\n\n");
  });
});

describe("a cut is a UTF-16 range of the stored text (3.1)", () => {
  test("your own message after an emoji: offsets count UTF-16 units, as the engine does", () => {
    const stored = "Hi 👩‍💻 team, please check the login flow";
    const cut = cutOf(userRoot(stored), "login", "flow");
    expect(cut).toEqual({ message_id: "m1", start: stored.indexOf("login"), end: stored.length, text: "login flow" });
    expect(stored.slice(cut.start, cut.end)).toBe(cut.text);
    // Counting code points would put the cut two places too early.
    expect(Array.from(stored.slice(0, cut.start)).length).toBe(cut.start - 2);
  });

  test("your own message's trimmed start still counts in the stored text", () => {
    const stored = "\n\n   Branch \x1b[1mright\x1b[0m here please";
    const cut = cutOf(userRoot(stored), "right", "here");
    expect(cut.start).toBe(stored.indexOf("right"));
    expect(cut.end).toBe(stored.indexOf("here") + 4);
    expect(cut.text).toBe(stored.slice(cut.start, cut.end));
  });

  test("a reply after an emoji and markup: the stored Markdown's offsets", () => {
    const stored = "🎉 **Done.** The cache is warm now.";
    const cut = cutOf(replyRoot(stored), "cache", "warm");
    expect(cut).toMatchObject({ start: stored.indexOf("cache"), text: "cache is warm" });
    expect(stored.slice(cut.start, cut.end)).toBe("cache is warm");
  });

  test("white space at either end of the selection is left out; white space alone is no cut", () => {
    const stored = "The cache is warm now.";
    const root = replyRoot(stored);
    expect(cutOf(root, " cache", "warm ")).toMatchObject({ start: 4, end: 17, text: "cache is warm" });
    expect(cutOf(root, " ", " ")).toEqual({ error: "empty" });
    expect(selectionToCut({ messageId: "m1", range: null }, sourceMapOf(root))).toEqual({ error: "empty" });
  });
});

describe("selections that end inside markup (3.1)", () => {
  test.each([
    ["part of an emphasis: the letters only", "Use **bold words** here.", "bold", "bold", "bold"],
    ["the whole emphasis: its marks too", "Use **bold words** here.", "bold", "words", "**bold words**"],
    ["part of a link label: the letters only", "See [the docs](https://example.com/a) now.", "docs", "docs", "docs"],
    ["the whole link label: the whole link", "See [the docs](https://example.com/a) now.", "the", "docs", "[the docs](https://example.com/a)"],
    ["part of a code span: the letters only", "Run `make test` first.", "make", "make", "make"],
    ["a code span inside the words: its ticks too", "Run `make test` first.", "Run", "first", "Run `make test` first"],
  ])("%s", (_name, stored, from, to, want) => {
    const cut = cutOf(replyRoot(stored), from, to);
    expect(cut.text).toBe(want);
    expect(stored.slice(cut.start, cut.end)).toBe(want);
  });
});

// -- Where a cut can start: the timeline's selection --

function frame(key, kind, fill) {
  const el = document.createElement("div");
  el.setAttribute("role", "article");
  el.dataset.key = key;
  const byline = document.createElement("p");
  byline.textContent = kind.toUpperCase();
  el.append(byline);
  fill(el);
  return el;
}

function page(items) {
  const root = document.createElement("div");
  for (const item of items) {
    root.append(
      frame(item.key, item.kind, (el) => {
        if (item.kind === "reply") el.append(renderMarkdown(item.text, { messageId: item.messageId }));
        else if (item.kind === "user") {
          const text = document.createElement("div");
          text.className = "text";
          userTextInto(text, item.text);
          el.append(text);
        } else {
          const p = document.createElement("p");
          p.textContent = item.text;
          el.append(p);
        }
      }),
    );
  }
  document.body.replaceChildren(root);
  return root;
}

const frameOf = (root, key) => [...root.querySelectorAll("[data-key]")].find((el) => el.dataset.key === key);

describe("where a cut can start (3.1)", () => {
  const items = [
    { kind: "user", key: "u1", messageId: "u1", text: "Please look at the login flow." },
    { kind: "reply", key: "a1", messageId: "a1", text: "The **login** flow keeps its token." },
    { kind: "tools", key: "g1", messageId: "a1", text: "Read auth/session.go" },
    { kind: "pending", key: "p1", messageId: "p1", text: "and the logout flow?" },
  ];

  test("words inside one reply name that reply's stored text", () => {
    const root = page(items);
    const range = drag(frameOf(root, "a1"), "login", "keeps");
    expect(cutOfRange(range, items)).toEqual({ message_id: "a1", start: 4, end: 24, text: "**login** flow keeps" });
  });

  test("a selection over two messages is two_messages; a triple click's empty tail is one message", () => {
    const root = page(items);
    const both = document.createRange();
    both.setStart(...point(frameOf(root, "u1"), "login"));
    both.setEnd(...point(frameOf(root, "a1"), "flow", true));
    expect(cutOfRange(both, items)).toEqual({ error: "two_messages" });
    const triple = document.createRange();
    triple.setStart(...point(frameOf(root, "u1"), "Please"));
    triple.setEnd(frameOf(root, "a1"), 0);
    expect(cutOfRange(triple, items)).toMatchObject({ message_id: "u1", text: "Please look at the login flow." });
  });

  test("a tool row is tool_output; a prompt still on its way is unfinished", () => {
    const root = page(items);
    expect(cutOfRange(drag(frameOf(root, "g1"), "auth"), items)).toEqual({ error: "tool_output" });
    expect(cutOfRange(drag(frameOf(root, "p1"), "logout"), items)).toEqual({ error: "unfinished" });
  });
});

describe("overlaps: half-open ranges (engine 5.11 step 4)", () => {
  const links = [{ highlight_start: 10, highlight_end: 20 }];
  test.each([
    [0, 10, false, "touching before"],
    [20, 30, false, "touching after"],
    [9, 11, true, "over the start"],
    [19, 25, true, "over the end"],
    [12, 15, true, "inside"],
    [5, 25, true, "around"],
    [10, 20, true, "the same words"],
  ])("[%i, %i) → %s (%s)", (start, end, want) => {
    expect(overlaps(links, start, end)).toBe(want);
  });
});

describe("canBranch: the reasons of 3.1", () => {
  const text = (t) => ({ type: "text", data: { text: t } });
  const finish = { type: "finish", data: { reason: "end_turn", time: 1 } };
  const reply = (...parts) => ({ id: "a1", role: "assistant", parts: [text("The login flow keeps its token."), ...parts] });
  const cut = { message_id: "a1", start: 4, end: 9, text: "login" };

  test("a finished reply with a clean cut can branch", () => {
    expect(canBranch(reply(finish), cut, [])).toEqual({ ok: true });
    expect(canBranch({ id: "u1", role: "user", parts: [text("Please look")] }, { ...cut, message_id: "u1" }, [])).toEqual({ ok: true });
  });

  test("where the words are comes first: two messages, then tool output", () => {
    expect(canBranch(reply(finish), { error: "two_messages" }, [])).toEqual({ reason: "two_messages" });
    expect(canBranch(reply(finish), { error: "tool_output" }, [])).toEqual({ reason: "tool_output" });
    expect(canBranch({ id: "t1", role: "tool", parts: [] }, cut, [])).toEqual({ reason: "tool_output" });
  });

  test("a reply without its finish, or with a tool still unanswered, is unfinished", () => {
    expect(canBranch(reply(), cut, [])).toEqual({ reason: "unfinished" });
    const call = { type: "tool_call", data: { id: "c1", name: "view", input: "{}" } };
    expect(canBranch(reply(call, finish), cut, [], { answered: new Set() })).toEqual({ reason: "unfinished" });
    expect(canBranch(reply(call, finish), cut, [], { answered: new Set(["c1"]) })).toEqual({ ok: true });
    expect(canBranch(reply(), { error: "unfinished" }, [])).toEqual({ reason: "unfinished" });
    // A reply still being written says so whatever the selection (a whole line spills into the next item).
    expect(canBranch(reply(), { error: "two_messages" }, [])).toEqual({ reason: "unfinished" });
    expect(HINTS.unfinished).toBe("Wait for the reply to finish");
  });

  test("no words is empty", () => {
    expect(canBranch(reply(finish), { error: "empty" }, [])).toEqual({ reason: "empty" });
    expect(canBranch(reply(finish), null, [])).toEqual({ reason: "empty" });
    expect(canBranch(null, null, [])).toEqual({ reason: "empty" });
  });

  test("words already under a link on this message overlap; touching words and other messages' links do not", () => {
    const link = { id: "l1", source_message_id: "a1", highlight_start: 0, highlight_end: 4 };
    expect(canBranch(reply(finish), cut, [{ ...link, highlight_end: 5 }])).toEqual({ reason: "overlap" });
    expect(canBranch(reply(finish), cut, [link])).toEqual({ ok: true });
    expect(canBranch(reply(finish), cut, [{ ...link, source_message_id: "u1", highlight_end: 9 }])).toEqual({ ok: true });
  });
});

test("a refused branch's notice: the desktop texts of 3.2 step 3, else the engine's message", () => {
  const cases = [
    [{ code: "bad_cut", message: "highlight does not match the message text" }, "highlight does not match the message text"],
    [{ code: "cut_not_finished", message: "x" }, "This reply is still being written."],
    [{ code: "highlight_overlaps_link", message: "x" }, "These words already open a branch."],
    [{ code: "transport", message: "x" }, "Branch not made: lost the connection to Crush."],
    [{ code: "engine_unavailable", message: "x" }, "Branch not made: lost the connection to Crush."],
    [{ code: "something_new", message: "the engine's own words" }, "the engine's own words"],
  ];
  for (const [error, want] of cases) expect(branchErrorText(error), error.code).toBe(want);
});
