import { describe, expect, it } from "vitest";
import { commandArguments, fillArguments, placeholders } from "./customcommands.js";

// Custom command arguments (desktop-design-5-6 2.2, CMD-057 to CMD-059): a
// slip sends the AI a prompt with the wrong text in it.
describe("custom command arguments", () => {
  it("finds each $NAME once, in order", () => {
    expect(placeholders("Fix $ISSUE in $FILE, then $ISSUE again; $lower and $ are not names")).toEqual(["ISSUE", "FILE"]);
  });

  it("fills longest names first, so $FOO never cuts $FOOBAR", () => {
    expect(fillArguments("$FOOBAR and $FOO", { FOO: "a", FOOBAR: "b" })).toBe("b and a");
  });

  it("uses declared arguments when there are any, else the placeholders", () => {
    expect(commandArguments({ arguments: [{ name: "PATH_NAME", required: false }] })).toEqual([{ id: "PATH_NAME", title: "Path Name", description: "", required: false }]);
    expect(commandArguments({ content: "Review $PULL_REQUEST" })).toEqual([{ id: "PULL_REQUEST", title: "Pull Request", description: "", required: true }]);
  });
});
