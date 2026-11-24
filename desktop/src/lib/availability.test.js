import { describe, expect, it } from "vitest";
import { shortUnavailable, unavailableReason } from "./availability.js";

describe("provider availability", () => {
  it("keeps the engine's sentence intact for the hover title", () => {
    const byProvider = { "plan-api": "This provider is not usable here: this folder is not a git project." };
    expect(unavailableReason("plan-api", byProvider)).toBe(byProvider["plan-api"]);
    expect(unavailableReason("openai", byProvider)).toBe("");
  });

  it("shortens the row text to the reason alone", () => {
    expect(shortUnavailable("This provider is not usable here: this is your home folder.")).toBe("this is your home folder.");
    expect(shortUnavailable("")).toBe("");
  });
});
