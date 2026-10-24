import { describe, expect, it } from "vitest";
import { shortUnavailable, unavailableReason } from "./availability.js";

describe("provider availability", () => {
  it("keeps the engine's sentence intact for the hover title", () => {
    const byProvider = { "kimi-coding": "Moonshot AI is not usable here: this folder is not a git project." };
    expect(unavailableReason("kimi-coding", byProvider)).toBe(byProvider["kimi-coding"]);
    expect(unavailableReason("openai", byProvider)).toBe("");
  });

  it("shortens the row text to the reason alone", () => {
    expect(shortUnavailable("Moonshot AI is not usable here: this is your home folder.")).toBe("this is your home folder.");
    expect(shortUnavailable("")).toBe("");
  });
});
