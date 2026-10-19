import { describe, expect, it } from "vitest";
import { hasIncomplete, progress, queueLabel, queueRows, todoHeader, todoList, visibleQueue } from "./trayrows.js";

const task = (name, status) => `<crush-task-result><name>${name}</name><status>${status}</status>\nbody</crush-task-result>`;

describe("queue rows (TYPE-045, pills.go:87-119)", () => {
  it("relabels task results", () => {
    expect(queueLabel(task("build", "done"))).toBe("Result of task build");
    expect(queueLabel(task("build", "running"))).toBe("Progress of build");
    expect(queueLabel(task("Questions", "answered"))).toBe("Your answers to the questions");
    expect(queueLabel(task("Questions", "cancelled"))).toBe("Your answers to the questions");
    expect(queueLabel(task("Questions", "running"))).toBe("Progress of Questions");
  });

  it("keeps each prompt on one line", () => {
    expect(queueLabel("  fix the\n\n  tests  please ")).toBe("fix the tests please");
  });

  it("tags only the first row", () => {
    expect(queueRows(["one", "two"])).toEqual([
      { text: "one", tag: "waiting · sends next" },
      { text: "two", tag: "" },
    ]);
    expect(queueRows([])).toEqual([]);
  });
});

describe("visibleQueue (TYPE-044, TYPE-045, queue_display.go:7-58)", () => {
  it("hides prompts the timeline already shows and adds held ones the engine does not list yet", () => {
    const entries = [
      { prompt: "shown already", submission_id: "a" },
      { prompt: "second", submission_id: "b" },
    ];
    const held = [
      { text: "second", submissionId: "b" },
      { text: "just sent", submissionId: "c" },
    ];
    expect(visibleQueue({ entries, shownIds: new Set(["a"]), held })).toEqual(["second", "just sent"]);
    expect(visibleQueue({})).toEqual([]);
  });
});

describe("to-dos (SIDE-031, pills.go:47-80, todos.go:139-192)", () => {
  const todos = [
    { content: "Write tests", status: "pending", active_form: "Writing tests" },
    { content: "Read the spec", status: "completed", active_form: "Reading the spec" },
    { content: "Build the field", status: "in_progress", active_form: "Building the field" },
    { content: "Ship", status: "pending", active_form: "Shipping" },
  ];

  it("counts done of all and finds the step in progress", () => {
    expect(progress(todos)).toEqual({ done: 1, total: 4, active: todos[2] });
    expect(hasIncomplete(todos)).toBe(true);
    expect(hasIncomplete([{ status: "completed" }])).toBe(false);
  });

  it("closed shows the active step; open does not", () => {
    expect(todoHeader(todos, false)).toEqual({ label: "TO-DO", count: "1/4", step: "Building the field", hint: "ctrl+t show" });
    expect(todoHeader(todos, true)).toEqual({ label: "TO-DO", count: "1/4", step: "", hint: "ctrl+t hide" });
  });

  it("lists done, in progress, then pending, the active one by its active form", () => {
    expect(todoList(todos)).toEqual([
      { status: "completed", text: "Read the spec" },
      { status: "in_progress", text: "Building the field" },
      { status: "pending", text: "Write tests" },
      { status: "pending", text: "Ship" },
    ]);
  });
});
