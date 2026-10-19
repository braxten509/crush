import { describe, expect, it } from "vitest";
import {
  BUSY_SWITCH,
  CODE_INFO,
  PLAN_BANNER,
  READY_PLACEHOLDERS,
  WORKING_PLACEHOLDERS,
  YOLO_BANNER,
  bannerFor,
  cycleSteps,
  isQuitWord,
  markOf,
  modelButtonText,
  modelLabel,
  pickPlaceholders,
  placeholderFor,
} from "./modes.js";

const picks = { ready: "Ready?", working: "Brrrrr..." };

describe("placeholderFor (TYPE-003, composer.go:82-95)", () => {
  it("follows the terminal's order: bang, YOLO coding, busy, plan, ready", () => {
    expect(placeholderFor({ picks })).toBe("Ready?");
    expect(placeholderFor({ plan: true, picks })).toBe("Let's plan");
    expect(placeholderFor({ busy: true, picks })).toBe("Brrrrr...");
    expect(placeholderFor({ plan: true, busy: true, picks })).toBe("Brrrrr...");
    expect(placeholderFor({ yolo: true, busy: true, picks })).toBe("Go crazy");
    expect(placeholderFor({ yolo: true, plan: true, picks })).toBe("Let's plan");
    expect(placeholderFor({ bang: true, yolo: true, busy: true, plan: true, picks })).toBe("Run a shell command");
  });

  it("picks from the terminal's lists", () => {
    expect(pickPlaceholders(() => 0)).toEqual({ ready: "Ready!", working: "Working!" });
    expect(pickPlaceholders(() => 0.999999)).toEqual({ ready: "Ready for instructions", working: "Thinking..." });
    const seen = pickPlaceholders(Math.random);
    expect(READY_PLACEHOLDERS).toContain(seen.ready);
    expect(WORKING_PLACEHOLDERS).toContain(seen.working);
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
    expect(BUSY_SWITCH).toBe("Agent is busy, please wait before switching input mode...");
  });
});

describe("bannerFor (TYPE-025, input_mode.go:96-123)", () => {
  it("gives the terminal's kinds and words", () => {
    expect(bannerFor("plan", true)).toEqual({ kind: "plan", text: "Plan with Crush before generating any code." });
    expect(bannerFor("code", true)).toEqual({ kind: "yolo", text: YOLO_BANNER });
    expect(bannerFor("code", false)).toEqual({ kind: "info", text: "input mode: code" });
    expect(PLAN_BANNER).toBe("Plan with Crush before generating any code.");
    expect(YOLO_BANNER).toBe("Skip permission prompts. System level commands will be blocked.");
  });
});

describe("modelLabel (SIDE-006, composer_footer.go:255-290)", () => {
  const agent = {
    is_ready: true,
    model: { id: "claude-opus-5-5", name: "Opus 5.5", default_reasoning_effort: "high", supports_attachments: true },
    model_cfg: { model: "claude-opus-5-5", provider: "anthropic", reasoning_effort: "MAX" },
  };

  it("joins the name and settings with a dot", () => {
    expect(modelLabel(agent)).toBe("Opus 5.5 · max");
    expect(modelLabel({ ...agent, model_cfg: { ...agent.model_cfg, reasoning_effort: "", service_tier: "fast", ultracode: true } }))
      .toBe("Opus 5.5 · high · fast · ultracode");
    expect(modelLabel({ ...agent, model: { name: "" }, model_cfg: { model: "local-7b", think: true } })).toBe("local-7b · thinking");
  });

  it("follows the footer items (SIDE-017, SIDE-018)", () => {
    expect(modelLabel(agent, ["settings"])).toBe("max");
    expect(modelLabel(agent, ["model"])).toBe("Opus 5.5");
    expect(modelLabel(agent, [])).toBe("");
  });

  it("is empty before the agent has a model", () => {
    expect(modelLabel(null)).toBe("");
    expect(modelLabel({ is_ready: false })).toBe("");
  });

  it("gives the button Unknown until then, and never for hidden footer items", () => {
    expect(modelButtonText(null)).toBe("Unknown");
    expect(modelButtonText({ is_ready: false })).toBe("Unknown");
    expect(modelButtonText(agent)).toBe("Opus 5.5 · max");
    expect(modelButtonText(agent, [])).toBe("");
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
