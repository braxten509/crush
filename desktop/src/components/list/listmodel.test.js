import { describe, expect, it } from "vitest";
import { buildRows, dayLabel, emptyText, rowState, rowTime, titleMatches } from "../../lib/listmodel.js";

// Local times, so the checks hold in any time zone.
const NOW = new Date(2026, 9, 18, 15, 0).getTime();
const at = (month, day, hour = 12, minute = 0, year = 2026) => new Date(year, month, day, hour, minute).getTime() / 1000;

function chat(id, updated, extra = {}) {
  return { id, title: id.toUpperCase(), created_at: updated, updated_at: updated, ...extra };
}

const ids = (rows) => rows.map((r) => r.id);
const shape = (items) => items.map((i) => (i.kind === "day" ? `# ${i.label}` : `${"  ".repeat(i.depth)}${i.id}`));

describe("days and times", () => {
  it("groups by the local day of the last activity", () => {
    expect(dayLabel(at(9, 18, 0, 1), NOW)).toBe("Today");
    expect(dayLabel(at(9, 19, 9), NOW)).toBe("Today"); // a clock ahead still counts as today
    expect(dayLabel(at(9, 17, 23, 59), NOW)).toBe("Yesterday");
    expect(dayLabel(at(9, 16), NOW)).toBe("Previous 7 days");
    expect(dayLabel(at(9, 11), NOW)).toBe("Previous 7 days");
    expect(dayLabel(at(9, 10), NOW)).toBe("Previous 30 days");
    expect(dayLabel(at(8, 18), NOW)).toBe("Previous 30 days");
    expect(dayLabel(at(8, 17), NOW)).toBe("Older");
  });

  it("shows the clock today and yesterday, the weekday within a week, else the date", () => {
    expect(rowTime(at(9, 18, 10, 43), NOW)).toBe("10:43 AM");
    expect(rowTime(at(9, 17, 9, 5), NOW)).toBe("9:05 AM");
    expect(rowTime(at(9, 14), NOW)).toBe("Wed");
    expect(rowTime(at(8, 3), NOW)).toBe("Sep 3");
    expect(rowTime(at(11, 24, 12, 0, 2025), NOW)).toBe("Dec 24, 2025");
    expect(rowTime(0, NOW)).toBe("");
  });
});

describe("rowState (LIST-012)", () => {
  it("puts a waiting prompt before a run, and a run before background work", () => {
    expect(rowState({ pending_prompt: "permission", has_unfinished_work: true }, true)).toEqual({ kind: "waiting", text: "Waiting for permission" });
    expect(rowState({ pending_prompt: "question" }, true)).toEqual({ kind: "waiting", text: "Waiting for your answer" });
    expect(rowState({ pending_prompt: "", has_unfinished_work: true }, true)).toEqual({ kind: "working", text: "Working…" });
    expect(rowState({ has_unfinished_work: true }, false)).toEqual({ kind: "background", text: "Background" });
    expect(rowState({ pending_prompt: "" }, false)).toBeNull();
  });
});

describe("buildRows", () => {
  const list = [
    chat("a", at(9, 18, 11)),
    chat("b", at(9, 18, 9)),
    chat("c", at(9, 17)),
    chat("d", at(9, 12)),
    chat("e", at(9, 1)),
    chat("f", at(5, 1)),
  ];

  it("keeps the list's order under day headers, with times and states", () => {
    const busy = (id) => id === "b";
    const { items, rows, closedCount, empty } = buildRows(list, { now: NOW, busy, selectedId: "a" });
    expect(shape(items)).toEqual(["# Today", "a", "b", "# Yesterday", "c", "# Previous 7 days", "d", "# Previous 30 days", "e", "# Older", "f"]);
    expect(rows[0]).toMatchObject({ id: "a", title: "A", time: "11:00 AM", depth: 0, selected: true, closed: false, state: "" });
    expect(rows[1]).toMatchObject({ id: "b", selected: false, state: "Working…", stateKind: "working" });
    expect(closedCount).toBe(0);
    expect(empty).toBe("");
  });

  it("names an untitled chat as the engine does", () => {
    expect(buildRows([chat("x", at(9, 18), { title: "" })], { now: NOW }).rows[0].title).toBe("New Session");
  });

  it("uses the activity time it is given for groups and times", () => {
    const timeOf = (s) => (s.id === "f" ? at(9, 18, 14, 30) : s.updated_at);
    const { rows } = buildRows([list[5]], { now: NOW, timeOf });
    expect(rows[0].time).toBe("2:30 PM");
  });

  // p ← q ← r (r is q's branch, q is p's branch); s ← t; u's parent is not listed.
  const tree = [
    chat("p", at(9, 18, 14)),
    chat("q", at(9, 18, 13), { created_at: at(9, 18, 10) }),
    chat("q2", at(9, 18, 12), { created_at: at(9, 18, 9) }),
    chat("r", at(9, 18, 11)),
    chat("s", at(9, 17, 10)),
    chat("t", at(9, 17, 9)),
    chat("u", at(9, 16)),
  ];
  const parents = { q: "p", q2: "p", r: "q", t: "s", u: "gone" };
  const parentOf = (id) => parents[id] ?? null;

  it("hides the branches of a chat that is not open, and counts them", () => {
    const { items, rows } = buildRows(tree, { now: NOW, parentOf });
    expect(shape(items)).toEqual(["# Today", "p", "# Yesterday", "s", "# Previous 7 days", "u"]);
    expect(rows[0]).toMatchObject({ id: "p", branches: 2, expanded: false });
  });

  it("opens the open chat and every ancestor; branches come oldest first", () => {
    const { items, rows } = buildRows(tree, { now: NOW, parentOf, selectedId: "r" });
    expect(shape(items)).toEqual(["# Today", "p", "  q2", "  q", "    r", "# Yesterday", "s", "# Previous 7 days", "u"]);
    expect(rows.find((r) => r.id === "r")).toMatchObject({ depth: 2, selected: true });
    expect(rows.find((r) => r.id === "q")).toMatchObject({ depth: 1, expanded: true, branches: 1 });
  });

  it("opens the chats it is told to", () => {
    const { items } = buildRows(tree, { now: NOW, parentOf, expanded: ["s"] });
    expect(shape(items)).toEqual(["# Today", "p", "# Yesterday", "s", "  t", "# Previous 7 days", "u"]);
  });

  it("leaves a closed chat out and moves its branches up to the nearest open ancestor", () => {
    const isClosed = (id) => id === "q";
    const { items, closedCount } = buildRows(tree, { now: NOW, parentOf, isClosed, selectedId: "r" });
    expect(shape(items)).toEqual(["# Today", "p", "  q2", "  r", "# Yesterday", "s", "# Previous 7 days", "u"]);
    expect(closedCount).toBe(1);
  });

  it("moves the branches of a closed top-level chat to the top, in its place and day", () => {
    const isClosed = (id) => id === "s";
    const late = tree.map((s) => (s.id === "t" ? { ...s, updated_at: at(9, 1) } : s));
    const { items } = buildRows(late, { now: NOW, parentOf, isClosed });
    expect(shape(items)).toEqual(["# Today", "p", "# Yesterday", "t", "# Previous 7 days", "u"]);
  });

  it("shows a branch whose parent is not listed at the top", () => {
    expect(ids(buildRows(tree, { now: NOW, parentOf }).rows)).toContain("u");
  });

  it("never loses a chat to a parent loop", () => {
    const loop = { x: "y", y: "x" };
    const { rows } = buildRows([chat("x", at(9, 18, 10)), chat("y", at(9, 18, 9))], { now: NOW, parentOf: (id) => loop[id] ?? null });
    expect(ids(rows).sort()).toEqual(["x", "y"]);
  });

  it("searches titles anywhere, any case, unnested, leaving closed chats out", () => {
    const isClosed = (id) => id === "t";
    const named = tree.map((s) => ({ ...s, title: { p: "Login keeps redirecting", r: "Cookie lifetime after LOGIN", t: "Login cookie", u: "Other" }[s.id] ?? s.title }));
    const { rows, empty } = buildRows(named, { now: NOW, parentOf, isClosed, query: "  login " });
    expect(rows.map((r) => [r.id, r.depth])).toEqual([["p", 0], ["r", 0]]);
    expect(empty).toBe("");
    expect(buildRows(named, { now: NOW, query: "zebra" }).empty).toBe("No chats match “zebra”.");
  });

  it("says when there are no chats", () => {
    expect(buildRows([], { now: NOW }).empty).toBe("No chats yet");
    expect(emptyText("", 0)).toBe("No chats yet");
    expect(titleMatches("Card Wars", "WAR")).toBe(true);
  });
});
