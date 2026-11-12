import { describe, expect, it } from "vitest";
import {
  BANG_PLACEHOLDER,
  BUSY_SWITCH,
  CODE_INFO,
  PLAN_PLACEHOLDER,
  YOLO_PLACEHOLDER,
  cycleSteps,
  isQuitWord,
  markOf,
  modelButtonText,
  modelLabel,
  placeholderFor,
} from "./modes.js";

const picks = { ready: "Ready?", working: "Brrrrr..." };

describe("placeholderFor (TYPE-003, composer.go:82-95)", () => {
  it("follows the terminal's order: bang, YOLO coding, busy, plan, ready", () => {
    expect(placeholderFor({ picks })).toBe(picks.ready);
    expect(placeholderFor({ plan: true, picks })).toBe(PLAN_PLACEHOLDER);
    expect(placeholderFor({ busy: true, picks })).toBe(picks.working);
    expect(placeholderFor({ plan: true, busy: true, picks })).toBe(picks.working);
    expect(placeholderFor({ yolo: true, busy: true, picks })).toBe(YOLO_PLACEHOLDER);
    expect(placeholderFor({ yolo: true, plan: true, picks })).toBe(PLAN_PLACEHOLDER);
    expect(placeholderFor({ bang: true, yolo: true, busy: true, plan: true, picks })).toBe(BANG_PLACEHOLDER);
  });
});

describe("markOf (TYPE-002)", () => {
  it("shows one mark: bang beats plan, plan beats YOLO", () => {
    expect(markOf({})).toBe("normal");
    expect(markOf({ yolo: true })).toBe("yolo");
    expect(markOf({ plan: true, yolo: true })).toBe("plan");
    expect(markOf({ bang: true, plan: true, yolo: true })).toBe("bang");
  });
});

describe("cycleSteps (TYPE-023, input_mode.go:16-36)", () => {
  it("goes code → plan → code with YOLO → code, turning off only the YOLO it turned on", () => {
    expect(cycleSteps({ plan: false, skip: false, cycleYolo: false })).toEqual({ switchTo: "plan" });
    expect(cycleSteps({ plan: true, skip: false, cycleYolo: false })).toEqual({ yoloOn: true, claim: true, switchTo: "code" });
    expect(cycleSteps({ plan: false, skip: true, cycleYolo: true })).toEqual({ yoloOff: true, info: CODE_INFO });
  });

  it("leaves a YOLO the user turned on alone", () => {
    expect(cycleSteps({ plan: true, skip: true, cycleYolo: false })).toEqual({ yoloOn: false, claim: false, switchTo: "code" });
    expect(cycleSteps({ plan: false, skip: true, cycleYolo: false })).toEqual({ switchTo: "plan" });
  });

  it("refuses while busy or switching", () => {
    expect(cycleSteps({ plan: false, skip: false, cycleYolo: false, busy: true })).toEqual({ warn: BUSY_SWITCH });
    expect(cycleSteps({ plan: true, skip: false, cycleYolo: false, switching: true })).toEqual({ warn: BUSY_SWITCH });
  });
});

describe("modelLabel (SIDE-006, composer_footer.go:255-290)", () => {
  const agent = {
    is_ready: true,
    model: { id: "claude-opus-5-5", name: "Opus 5.5", default_reasoning_effort: "high", supports_attachments: true },
    model_cfg: { model: "claude-opus-5-5", provider: "anthropic", reasoning_effort: "MAX" },
  };

  const containsAll = (text, parts) => {
    for (const part of parts) expect(text).toContain(part);
  };

  it("shows the name and the settings", () => {
    containsAll(modelLabel(agent), ["Opus 5.5", "max"]);
    containsAll(modelLabel({ ...agent, model_cfg: { ...agent.model_cfg, reasoning_effort: "", service_tier: "fast", ultracode: true } }), ["Opus 5.5", "high", "fast", "ultracode"]);
    containsAll(modelLabel({ ...agent, model: { name: "" }, model_cfg: { model: "local-7b", think: true } }), ["local-7b", "thinking"]);
  });

  it("follows the footer items (SIDE-017, SIDE-018)", () => {
    expect(modelLabel(agent, ["settings"])).toContain("max");
    expect(modelLabel(agent, ["settings"])).not.toContain("Opus 5.5");
    expect(modelLabel(agent, ["model"])).toContain("Opus 5.5");
    expect(modelLabel(agent, ["model"])).not.toContain("max");
    expect(modelLabel(agent, [])).toBe("");
  });

  it("is empty before the agent has a model", () => {
    expect(modelLabel(null)).toBe("");
    expect(modelLabel({ is_ready: false })).toBe("");
  });

  it("gives the button a placeholder until then, and nothing for hidden footer items", () => {
    expect(modelButtonText(null)).toBeTruthy();
    expect(modelButtonText({ is_ready: false })).toBeTruthy();
    expect(modelButtonText(agent)).toContain("Opus 5.5");
    expect(modelButtonText(agent, [])).toBeFalsy();
  });
});

describe("isQuitWord (TYPE-007)", () => {
  it("matches exit and quit, trimmed, and nothing else", () => {
    expect(isQuitWord(" exit ")).toBe(true);
    expect(isQuitWord("quit\n")).toBe(true);
    expect(isQuitWord("Exit")).toBe(false);
    expect(isQuitWord("quit now")).toBe(false);
    expect(isQuitWord("")).toBe(false);
  });
});
