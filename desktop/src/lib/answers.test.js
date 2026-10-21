import { describe, expect, it } from "vitest";
import { answersView, askedIn, parseAnswers } from "./answers.js";

const RESULT = `<crush-task-result>
<name>Questions</name>
<status>answered</status>
<result>
Q1: How should the one ring work?
User selected: ["a"]

Notes:
- only for now

Q2: Anything else?
User provided: Two lines
of text

Q3: Ship it?
User answered: yes

Q4: Which colour?
User skipped this question
</result>
</crush-task-result>`;

const ASK = {
  role: "assistant",
  parts: [
    {
      type: "tool_call",
      data: {
        name: "Bash",
        input: JSON.stringify({
          command: `crush ask <<'EOF'
{"questions":[{"type":"single_choice","question":"How should the one ring work?","choices":[{"id":"a","label":"Desktop hears every open chat"},{"id":"b","label":"Per-folder hand-off"}]},{"type":"free_text","question":"Anything else?"}]}
EOF`,
        }),
      },
    },
  ],
};

describe("answers", () => {
  it("reads each question's answer and notes from the result", () => {
    const answers = parseAnswers(RESULT);
    expect(answers.map((a) => a.question)).toEqual(["How should the one ring work?", "Anything else?", "Ship it?", "Which colour?"]);
    expect(answers[0].selected).toEqual(["a"]);
    expect(answers[0].notes).toEqual([{ choice: "", text: "only for now" }]);
    expect(answers[1].provided).toBe("Two lines\nof text");
    expect(answers[2].yes).toBe(true);
    expect(answers[3].skipped).toBe(true);
  });

  it("finds the questions in a crush ask here-document", () => {
    expect(askedIn("crush ask <<'EOF'\n{\"questions\":[{\"question\":\"Q\"}]}\nEOF")).toEqual([{ question: "Q" }]);
    expect(askedIn("echo hi")).toBeNull();
  });

  it("shows the picked choices by label, from the newest ask before the line", () => {
    const older = { role: "assistant", parts: [{ type: "tool_call", data: { input: JSON.stringify({ command: "crush ask <<'EOF'\n{\"questions\":[{\"question\":\"How should the one ring work?\",\"choices\":[{\"id\":\"a\",\"label\":\"Old label\"}]}]}\nEOF" }) } }] };
    const view = answersView(RESULT, [ASK, older]);
    expect(view[0]).toEqual({ question: "How should the one ring work?", picks: ["Desktop hears every open chat"], skipped: false, notes: ["only for now"] });
    expect(view[1].picks).toEqual(["Two lines\nof text"]);
    expect(view[2].picks).toEqual(["Yes"]);
    expect(view[3].skipped).toBe(true);
  });

  it("falls back to the ids when the questions cannot be found", () => {
    expect(answersView(RESULT, [])[0].picks).toEqual(["a"]);
  });
});
