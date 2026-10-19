// CHAT-055 hook lines and CHAT-072 question rows (ports of
// chat/tool_hooks.go and chat/question.go with question_test.go).
import { describe, expect, it } from "vitest";
import { callHookLines, hookDetail, hookLines, isLikelyPath, MAX_HOOK_NAME, truncateHookName } from "./hooks.js";
import { answerParts, questionBlocks, questionBody, questionSummary } from "./question.js";

const meta = (hooks) => JSON.stringify({ hook: { hook_count: hooks.length, decision: "allow", hooks } });

describe("hook lines", () => {
  it("lists each hook with its matcher and outcome", () => {
    expect(
      hookLines(
        meta([
          { name: "gofmt", matcher: "edit", decision: "allow", input_rewrite: true },
          { name: "guard", decision: "deny", reason: "no rm" },
          { name: "wakatime", event: "PreToolUse", duration_ms: 12 },
        ]),
      ),
    ).toEqual([
      { name: "gofmt", matcher: "edit", detail: { text: "OK", tone: "ok", rewrote: "Rewrote Output" }, denied: false },
      { name: "guard", matcher: "", detail: { text: "Denied", tone: "denied", reason: "no rm" }, denied: true },
      { name: "wakatime", matcher: "", detail: { text: "OK", tone: "ok", rewrote: "" }, denied: false },
    ]);
  });
  it("reads a call's hooks list first, else its result's metadata", () => {
    const fromMeta = { hooks: [], result: { metadata: meta([{ name: "gofmt", decision: "allow" }]) } };
    expect(callHookLines(fromMeta).map((h) => h.name)).toEqual(["gofmt"]);
    const listed = { hooks: [{ name: "guard", decision: "deny", reason: "no" }], result: { metadata: meta([{ name: "gofmt" }]) } };
    expect(callHookLines(listed)).toEqual([{ name: "guard", matcher: "", detail: { text: "Denied", tone: "denied", reason: "no" }, denied: true }]);
    expect(callHookLines({ hooks: [], result: null })).toEqual([]);
    expect(callHookLines({})).toEqual([]);
  });
  it("shows nothing without hooks or with unreadable metadata", () => {
    expect(hookLines("")).toEqual([]);
    expect(hookLines("{")).toEqual([]);
    expect(hookLines(JSON.stringify({ other: 1 }))).toEqual([]);
    expect(hookLines(meta([]))).toEqual([]);
    expect(hookLines(JSON.stringify({ hook: { hooks: [{ name: 5 }] } }))).toEqual([]);
  });
  it("marks newlines in names and cuts long names, paths from the left", () => {
    expect(hookLines(meta([{ name: "a\nb" }]))[0].name).toBe("a¶b");
    expect(truncateHookName("/home/me/.config/crush/hooks/format.sh", 20)).toBe("…ush/hooks/format.sh");
    expect(truncateHookName("run the formatter on every file", 20)).toBe("run the formatter o…");
    expect(truncateHookName("short", MAX_HOOK_NAME)).toBe("short");
    expect(isLikelyPath("hooks/x.sh")).toBe(true);
    expect(isLikelyPath("/x.sh")).toBe(true);
    expect(isLikelyPath("a b/c")).toBe(false);
    expect(isLikelyPath("plain")).toBe(false);
    expect(hookDetail({ decision: "deny", reason: "" })).toEqual({ text: "Denied", tone: "denied", reason: "" });
  });
});

describe("questions", () => {
  it("sums up the questions in the header", () => {
    expect(questionSummary([{ question: "Pick a color?" }])).toBe("Pick a color?");
    expect(questionSummary([{ question: "x".repeat(61) }])).toBe("x".repeat(59) + "…");
    expect(questionSummary([{ question: "x".repeat(60) }])).toBe("x".repeat(60));
    expect(questionSummary([{ question: "y".repeat(41) }, { question: "b" }, { question: "c" }])).toBe("y".repeat(39) + "… (+2 more)");
    expect(questionSummary([])).toBe("");
  });
  it("reads each answer (question_test.go)", () => {
    expect(answerParts("User provided: cool flag\n500\nForensics Steganography").map((p) => p.text).join(", ")).toBe("cool flag 500 Forensics Steganography");
    expect(answerParts('User selected: ["speed","readability"]\nUser provided: maintainability').map((p) => p.text).join(", ")).toBe("speed, readability, maintainability");
    expect(answerParts("User answered: yes")).toEqual([{ text: "Yes", tone: "yes" }]);
    expect(answerParts("User answered: no")).toEqual([{ text: "No", tone: "no" }]);
    expect(answerParts("User skipped this question")).toEqual([{ text: "Skipped", tone: "skipped" }]);
  });
  it("splits the result into questions, answers and notes", () => {
    const content = "Q1: Pick a color?\nUser selected: [\"red\"]\n\nNotes:\n- bright\nQ2: Ship it?\nUser answered: yes";
    expect(questionBlocks(content)).toEqual([
      { question: "Pick a color?", answer: 'User selected: ["red"]', notes: ["bright"] },
      { question: "Ship it?", answer: "User answered: yes", notes: [] },
    ]);
    expect(questionBody(content).blocks[1].parts).toEqual([{ text: "Yes", tone: "yes" }]);
    expect(questionBody("")).toBeNull();
  });
});
