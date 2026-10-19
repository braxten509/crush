import { describe, expect, it } from "vitest";
import { answerBody, initialState, isAnswered, respond, summaryLine, tabLabels } from "./form.js";

// What a question form sends back (desktop-design-5-6 2.6): a wrong answer
// here reaches the agent with nothing on screen to show it.
const single = { id: "q1", type: "single_choice", question: "Pick a color for the theme", choices: [{ id: "a", label: "Red" }, { id: "b", label: "Blue" }] };
const multi = { id: "q2", type: "multi_choice", label: "Parts", question: "Which parts?", choices: [{ id: "x", label: "X" }, { id: "y", label: "Y" }], selected: ["y", "gone"] };
const yesno = { id: "q3", type: "yes_no", question: "Go ahead?" };
const free = { id: "q4", type: "free_text", question: "Anything else?" };
const secure = { id: "q5", type: "secure_entry", label: "Key", question: "Enter the key" };

describe("respond", () => {
  it("single choice sends the choice under the cursor and any custom text", () => {
    const st = { ...initialState(single), cursor: 1, custom: "  teal  " };
    expect(respond(single, st)).toEqual({ request_id: "q1", selected_ids: ["b"], fill_in_text: "teal" });
    // The cursor on "Something else?" picks no choice.
    expect(respond(single, { ...initialState(single), cursor: 2 }).selected_ids).toEqual([]);
  });

  it("multi choice keeps preselected ids that exist, in choice order", () => {
    const st = initialState(multi);
    expect(st.picked).toEqual([false, true]);
    expect(respond(multi, { ...st, picked: [true, true] }).selected_ids).toEqual(["x", "y"]);
  });

  it("yes/no starts on No, and notes travel with the answer", () => {
    const st = initialState(yesno);
    expect(respond(yesno, st).yes).toBe(false);
    expect(respond(yesno, st, { yes: true }).yes).toBe(true);
    expect(respond(yesno, { ...st, notes: { q3: "careful" } }).notes).toEqual({ q3: "careful" });
  });

  it("blank free text is no answer at all", () => {
    expect(respond(free, { ...initialState(free), text: "   " })).toBeNull();
    expect(respond(free, { ...initialState(free), text: " ok " }).fill_in_text).toBe("ok");
  });
});

describe("answerBody", () => {
  it("sends one response per question, empty where unanswered, never a secret", () => {
    const request = { id: "batch", questions: [single, secure, free] };
    const leaked = { request_id: "q5", selected_ids: [], fill_in_text: "sk-secret" };
    const body = answerBody(request, [respond(single, initialState(single)), leaked, null]);
    expect(body.batch_request_id).toBe("batch");
    expect(body.responses).toEqual([
      { request_id: "q1", selected_ids: ["a"], fill_in_text: "" },
      { request_id: "q5", selected_ids: [], fill_in_text: "" },
      { request_id: "q4", selected_ids: [], fill_in_text: "" },
    ]);
  });
});

describe("labels and summary", () => {
  it("labels a batch with first-three-word labels and Confirm", () => {
    expect(tabLabels({ questions: [single, multi] })).toEqual(["Pick a color", "Parts", "Confirm"]);
    expect(tabLabels({ questions: [single] })).toEqual(["Pick a color"]);
  });

  it("summarizes answers, and secure ones by status only", () => {
    expect(isAnswered({ selected_ids: [], fill_in_text: "", yes: false })).toBe(true);
    expect(summaryLine(multi, { selected_ids: ["x"], fill_in_text: "Z" })).toBe("Parts: X, Z");
    expect(summaryLine(yesno, null)).toBe("Go ahead?: (not answered)");
    expect(summaryLine(secure, { fill_in_text: "sk-secret" }, "entered")).toBe("Key: entered");
  });
});
