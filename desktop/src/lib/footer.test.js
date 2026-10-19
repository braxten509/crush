import { describe, expect, it } from "vitest";
import {
  cliKind,
  DEFAULT_FOOTER_ITEMS,
  FOOTER_CHOICES,
  footerAnswer,
  footerFields,
  footerItems,
  footerText,
  footerTokenCount,
  formatReasoningEffort,
  goFixed,
  limitWindows,
  meterModel,
  modelInfo,
  shortDuration,
} from "./footer.js";

// SIDE-014, SIDE-019 to SIDE-024: ports of composer_footer.go, sidebar.go and
// common/elements.go. The Go answers below were printed by Go 1.27 itself
// (fmt %.0f, strconv.FormatFloat 'f', time.Duration.Round).

describe("Go's number formatting", () => {
  it("%.0f rounds a value exactly half way to even", () => {
    const cases = [
      [0.5, "0"], [1.5, "2"], [2.5, "2"], [14.5, "14"], [85.5, "86"], [99.5, "100"], [12.345, "12"], [0.125, "0"],
      [62, "62"], [37.5, "38"], [38.000001, "38"], [33.333333333333336, "33"], [66.66666666666667, "67"], [0.49999999999999994, "0"],
    ];
    for (const [value, text] of cases) expect(goFixed(value, 0), String(value)).toBe(text);
  });

  it("'f' with 1 and 2 decimals", () => {
    const two = [[1.125, "1.12"], [1.375, "1.38"], [12.345, "12.35"], [0.005, "0.01"], [0.015, "0.01"], [0.025, "0.03"], [2.675, "2.67"], [1.005, "1.00"], [0.125, "0.12"], [123.455, "123.45"]];
    for (const [value, text] of two) expect(goFixed(value, 2), String(value)).toBe(text);
    const one = [[0.05, "0.1"], [0.15, "0.1"], [0.25, "0.2"], [12.25, "12.2"], [12.35, "12.3"]];
    for (const [value, text] of one) expect(goFixed(value, 1), String(value)).toBe(text);
  });

  it("footerTokenCount", () => {
    const cases = [
      [0, "0"], [999, "999"], [1000, "1k"], [1125, "1.12k"], [1375, "1.38k"], [12345, "12.35k"], [12300, "12.3k"], [124000, "124k"],
      [200000, "200k"], [999999, "1000k"], [1000000, "1m"], [1500000, "1.5m"], [1234567, "1.23m"], [10000000, "10m"], [1005, "1k"],
      [1015, "1.01k"], [1025, "1.02k"],
    ];
    for (const [n, text] of cases) expect(footerTokenCount(n), String(n)).toBe(text);
  });

  it("shortDuration rounds to the minute, half away from zero", () => {
    const cases = [
      [0, "1m"], [1, "1m"], [29, "1m"], [30, "1m"], [59, "1m"], [60, "1m"], [89, "1m"], [90, "2m"], [3599, "1h0m"], [3600, "1h0m"],
      [9660, "2h41m"], [9689, "2h41m"], [9690, "2h42m"], [86399, "1d0h"], [86400, "1d0h"], [102599, "1d4h"], [102600, "1d4h"], [273600, "3d4h"],
    ];
    for (const [seconds, text] of cases) expect(shortDuration(seconds * 1000), String(seconds)).toBe(text);
  });

  it("formatReasoningEffort", () => {
    expect(formatReasoningEffort("xhigh")).toBe("X-High");
    expect(formatReasoningEffort("max")).toBe("Max");
    expect(formatReasoningEffort("medium")).toBe("Medium");
    expect(formatReasoningEffort("HIGH")).toBe("High");
    expect(formatReasoningEffort("")).toBe("");
  });
});

const opus = (cfg = {}, model = {}) => ({
  is_ready: true,
  model: { id: "opus", name: "Opus 5.5", context_window: 200_000, can_reason: true, reasoning_levels: ["low", "medium", "high", "xhigh", "max"], default_reasoning_effort: "high", ...model },
  model_cfg: { model: "opus", provider: "claude-code", reasoning_effort: "max", ...cfg },
});
const chat = (fields = {}) => ({ prompt_tokens: 100_000, completion_tokens: 24_000, estimated_usage: false, cost: 0, ...fields });
const windows = [
  { name: "5h", left: 14, resetsIn: 9660 * 1000 },
  { name: "Weekly", left: 69, resetsIn: 3 * 86_400_000 },
  { name: "Opus", left: 50, resetsIn: null },
];

describe("the Customize rows and the chosen items", () => {
  it("eight rows in the terminal's order and words; the default is every row but the resets", () => {
    expect(FOOTER_CHOICES.map((c) => c.label)).toEqual([
      "Model name", "Reasoning, fast and Ultracode", "Tokens used", "Context left", "5-hour usage left", "Weekly usage left", "Other usage limits",
      "When usage limits reset",
    ]);
    expect(DEFAULT_FOOTER_ITEMS).toEqual(FOOTER_CHOICES.map((c) => c.id).filter((id) => id !== "resets"));
  });

  it("footerItems: the saved list, an empty one too; no list means the defaults", () => {
    expect(footerItems(null)).toBe(DEFAULT_FOOTER_ITEMS);
    expect(footerItems({ options: { tui: {} } })).toBe(DEFAULT_FOOTER_ITEMS);
    expect(footerItems({ options: { tui: { composer_footer: ["tokens"] } } })).toEqual(["tokens"]);
    expect(footerItems({ options: { tui: { composer_footer: [] } } })).toEqual([]);
  });

  it("footerAnswer keeps the rows' order and saves an explicit none", () => {
    expect(footerAnswer([])).toEqual(["none"]);
    expect(footerAnswer(["resets", "model", "bogus"])).toEqual(["model", "resets"]);
  });
});

describe("footerFields (renderFooterItems)", () => {
  it("the defaults, with ~ for estimated usage and an estimated budget equal to the limit", () => {
    const session = chat({ estimated_usage: true, context_budget: { provider: "claude-code", model: "opus", tokens: 200_000, estimated: true } });
    const fields = footerFields({ agent: opus(), session, limit: 200_000, windows });
    expect(footerText(fields)).toBe("Opus 5.5 max · ~124k/~200k tokens · Context ~38% left · 5h 14% left · Weekly 69% left · Opus 50% left");
  });

  it("an estimated budget that is not the limit gets no ~ on the limit", () => {
    const session = chat({ context_budget: { provider: "claude-code", model: "opus", tokens: 150_000, estimated: true } });
    expect(footerText(footerFields({ items: ["tokens", "context"], session, limit: 200_000 }))).toBe("124k/200k tokens · Context 38% left");
  });

  it("resets only when chosen and still ahead", () => {
    const fields = footerFields({ items: ["limit_5h", "limit_weekly", "limit_other", "resets"], windows });
    expect(fields.map((f) => f.text)).toEqual(["5h 14% left resets 2h41m", "Weekly 69% left resets 3d0h", "Opus 50% left"]);
    const past = footerFields({ items: ["limit_5h", "resets"], windows: [{ name: "5h", left: 100, resetsIn: -5 }] });
    expect(past.map((f) => f.text)).toEqual(["5h 100% left"]);
  });

  it("an unknown limit: no limit after the tokens, and Context unknown", () => {
    expect(footerText(footerFields({ agent: opus(), session: chat(), limit: 0 }))).toBe("Opus 5.5 max · 124k tokens · Context unknown");
  });

  it("nothing chosen shows nothing", () => {
    expect(footerFields({ items: ["none"], agent: opus(), session: chat(), limit: 200_000, windows })).toEqual([]);
    expect(footerFields({ items: [], agent: opus(), session: chat(), limit: 200_000, windows })).toEqual([]);
  });

  it("the model's settings: effort (or its default), else thinking; fast; ultracode", () => {
    const only = (agent, items = ["model", "settings"]) => footerText(footerFields({ items, agent }));
    expect(only(opus(), ["settings"])).toBe("max");
    expect(only(opus({ reasoning_effort: "" }))).toBe("Opus 5.5 high");
    expect(only(opus({ reasoning_effort: "", think: true }, { default_reasoning_effort: "" }))).toBe("Opus 5.5 thinking");
    expect(only(opus({ service_tier: "fast", ultracode: true }))).toBe("Opus 5.5 max fast ultracode");
    expect(only(opus({ model: "opus-x" }, { name: "" }), ["model"])).toBe("opus-x");
    expect(only(opus(), ["model"])).toBe("Opus 5.5");
  });

  it("the context left is clamped and rounded as Go does", () => {
    const left = (prompt, limit) => footerFields({ items: ["context"], session: chat({ prompt_tokens: prompt, completion_tokens: 0 }), limit })[0].text;
    expect(left(300_000, 200_000)).toBe("Context 0% left");
    expect(left(-50, 200)).toBe("Context 100% left");
    expect(left(1, 200)).toBe("Context 100% left"); // 99.5 → 100 (even)
    expect(left(3, 200)).toBe("Context 98% left"); // 98.5 → 98 (even)
  });
});

describe("meterModel (the panel's meters)", () => {
  it("the approved picture: CONTEXT 62 %, 5-HOUR 86 % with the warning, WEEK 31 %", () => {
    const { meters, lines, tokens } = meterModel({ session: chat(), limit: 200_000, limits: windows.slice(0, 2) });
    expect(meters.map((m) => [m.label, m.text, m.warn])).toEqual([
      ["CONTEXT", "62%", false],
      ["5-HOUR", "86%", true],
      ["WEEK", "31%", false],
    ]);
    expect(meters.map((m) => m.used)).toEqual([62, 86, 31]);
    expect(lines).toEqual(["Context 38% left · 124k/200k tokens", "5h 14% left", "Weekly 69% left"]);
    expect(tokens).toBe("124k/200k tokens");
  });

  it("the warning shows under 20 % left, never at 20 % or more", () => {
    const at = (left) => meterModel({ items: ["limit_5h"], limits: [{ name: "5h", left, resetsIn: null }] }).meters[0].warn;
    expect(at(20)).toBe(false);
    expect(at(19.99)).toBe(true);
    expect(at(0)).toBe(true);
    const context = (prompt) => meterModel({ items: ["context"], session: chat({ prompt_tokens: prompt, completion_tokens: 0 }), limit: 100 }).meters[0].warn;
    expect(context(80)).toBe(false);
    expect(context(81)).toBe(true);
  });

  it("checking… until the CLI's first reading; an API model has no limit meters", () => {
    const checking = meterModel({ session: chat(), limit: 200_000, limits: null });
    expect(checking.meters.map((m) => [m.label, m.text])).toEqual([["CONTEXT", "62%"], ["5-HOUR", "checking…"], ["WEEK", "checking…"]]);
    expect(checking.lines.slice(1)).toEqual(["5h checking…", "Weekly checking…"]);
    expect(meterModel({ session: chat(), limit: 200_000, limits: [] }).meters.map((m) => m.label)).toEqual(["CONTEXT"]);
  });

  it("each Customize row controls its meter or words (SIDE-019 to SIDE-024)", () => {
    const labels = (items) => meterModel({ items, session: chat(), limit: 200_000, limits: windows }).meters.map((m) => m.label);
    expect(labels(DEFAULT_FOOTER_ITEMS)).toEqual(["CONTEXT", "5-HOUR", "WEEK", "OPUS"]);
    expect(labels(DEFAULT_FOOTER_ITEMS.filter((id) => id !== "context"))).toEqual(["5-HOUR", "WEEK", "OPUS"]);
    expect(labels(DEFAULT_FOOTER_ITEMS.filter((id) => id !== "limit_5h"))).toEqual(["CONTEXT", "WEEK", "OPUS"]);
    expect(labels(DEFAULT_FOOTER_ITEMS.filter((id) => id !== "limit_weekly"))).toEqual(["CONTEXT", "5-HOUR", "OPUS"]);
    // SIDE-023: a third window shows only while "Other usage limits" is on.
    expect(labels(DEFAULT_FOOTER_ITEMS.filter((id) => id !== "limit_other"))).toEqual(["CONTEXT", "5-HOUR", "WEEK"]);
    const other = meterModel({ items: ["limit_other"], limits: windows }).meters;
    expect(other.map((m) => [m.id, m.text])).toEqual([["limit_other:Opus", "50%"]]);
    // SIDE-019: tokens leave the detail and the strip.
    const noTokens = meterModel({ items: ["context"], session: chat(), limit: 200_000 });
    expect([noTokens.lines, noTokens.tokens]).toEqual([["Context 38% left"], ""]);
    const tokensOnly = meterModel({ items: ["tokens"], session: chat(), limit: 200_000 });
    expect([tokensOnly.meters, tokensOnly.lines]).toEqual([[], ["124k/200k tokens"]]);
    // SIDE-024: resets under each limit meter and in its line.
    const resets = meterModel({ items: ["limit_5h", "limit_weekly", "resets"], limits: windows });
    expect(resets.meters.map((m) => m.resets)).toEqual(["resets 2h41m", "resets 3d0h"]);
    expect(resets.lines).toEqual(["5h 14% left · resets 2h41m", "Weekly 69% left · resets 3d0h"]);
    expect(meterModel({ items: ["limit_5h"], limits: windows }).meters[0].resets).toBe("");
  });

  it("no chat: the context is unknown", () => {
    const { meters, lines, tokens } = meterModel({ session: null, limit: 200_000, limits: [] });
    expect(meters.map((m) => [m.label, m.text, m.used])).toEqual([["CONTEXT", "–", 0]]);
    expect([lines, tokens]).toEqual([["Context unknown"], ""]);
  });

  it("the shown used part and the left part add up to 100 at every half", () => {
    for (const left of [14.5, 15.5, 0.5, 99.5]) {
      const { meters, lines } = meterModel({ items: ["limit_5h"], limits: [{ name: "5h", left, resetsIn: null }] });
      const used = Number(meters[0].text.replace("%", ""));
      const shownLeft = Number(/(\d+)% left/u.exec(lines[0])[1]);
      expect(used + shownLeft, String(left)).toBe(100);
    }
  });
});

describe("cliKind and modelInfo (the details overlay)", () => {
  const providers = [{ id: "claude-code", name: "Claude Code", type: "claude-code" }, { id: "work", name: "Work", type: "codex-cli" }, { id: "abacus", name: "Abacus", type: "openai-compat" }];

  it("cliKind: an agent CLI's type, else nothing", () => {
    expect(cliKind(opus(), providers)).toBe("claude-code");
    expect(cliKind(opus({ provider: "work" }), providers)).toBe("codex-cli");
    expect(cliKind(opus({ provider: "openai-compat" }), providers)).toBe("");
    expect(cliKind(null, providers)).toBe("");
  });

  it("name, provider, reasoning, usage and cost", () => {
    const info = modelInfo({ agent: opus({ service_tier: "fast", ultracode: true }), providers, session: chat({ cost: 0.125 }), limits: windows.slice(0, 2) });
    expect(info.name).toBe("Opus 5.5");
    expect(info.via).toBe("via Claude Code");
    expect(info.reasoning).toBe("Reasoning Max · FAST · Ultracode");
    expect(info.usage).toEqual({ percent: "62%", tokens: "(124K)", cost: "$0.12", warn: false });
    expect(info.limits).toEqual([
      { name: "5h", left: "14%", warn: true, resets: "resets 2h41m" },
      { name: "Weekly", left: "69%", warn: false, resets: "resets 3d0h" },
    ]);
  });

  it("the reasoning words", () => {
    const reasoning = (cfg, model, list = providers) => modelInfo({ agent: opus(cfg, model), providers: list }).reasoning;
    expect(reasoning({ reasoning_effort: "xhigh" })).toBe("Reasoning X-High");
    expect(reasoning({ reasoning_effort: "" })).toBe("Reasoning High");
    expect(reasoning({ think: true }, { reasoning_levels: [] })).toBe("Thinking On");
    expect(reasoning({ think: false }, { reasoning_levels: [] })).toBe("Thinking Off");
    expect(reasoning({ provider: "abacus" }, { id: "gpt-4o", reasoning_levels: [] })).toBe("Thinking");
    expect(reasoning({ provider: "abacus" }, { id: "claude-sonnet", reasoning_levels: [] })).toBe("Thinking Off");
    expect(reasoning({}, { can_reason: false })).toBe("");
    // Fast needs Claude Code or Codex; Ultracode needs Claude Code with effort levels.
    expect(reasoning({ provider: "openai-compat", service_tier: "fast", ultracode: true })).toBe("Reasoning Max");
    expect(reasoning({ provider: "work", service_tier: "fast", ultracode: true })).toBe("Reasoning Max · FAST");
  });

  it("a provider missing from the list: no via, its id as its type", () => {
    const info = modelInfo({ agent: opus({ service_tier: "fast" }), providers: [] });
    expect(info.via).toBe("");
    expect(info.reasoning).toBe("Reasoning Max · FAST");
  });

  it("the usage line: one decimal K and M, ~ when estimated, the warning over 80 %", () => {
    const usage = (fields, window = 200_000) => modelInfo({ agent: opus({}, { context_window: window }), providers, session: chat(fields) }).usage;
    expect(usage({ estimated_usage: true })).toMatchObject({ percent: "~62%", tokens: "(124K)" });
    expect(usage({ prompt_tokens: 1_250_000, completion_tokens: 0 }, 2_000_000)).toMatchObject({ percent: "62%", tokens: "(1.2M)" });
    expect(usage({ prompt_tokens: 999, completion_tokens: 0 })).toMatchObject({ tokens: "(999)", percent: "0%" });
    expect(usage({ prompt_tokens: 12_000, completion_tokens: 0 })).toMatchObject({ tokens: "(12K)" });
    expect(usage({ prompt_tokens: 162_000, completion_tokens: 0 }).warn).toBe(true);
    expect(usage({ prompt_tokens: 160_000, completion_tokens: 0 }).warn).toBe(false);
    expect(usage({ prompt_tokens: 1, completion_tokens: 0 }, 0)).toMatchObject({ percent: "0%" });
  });

  it("no agent model: empty", () => {
    expect(modelInfo({ agent: null })).toEqual({ name: "", via: "", reasoning: "", usage: null, limits: [] });
  });
});

describe("limitWindows (cliagent.Limits and Limit.Left)", () => {
  const now = Date.parse("2026-10-18T12:00:00Z");
  const reading = (fields = {}) => ({
    kind: "claude-code",
    state: "ready",
    read_at: "2026-10-18T11:59:30.123456789Z",
    windows: [
      { name: "5h", used: 86, resets_at: "2026-10-18T14:41:00Z" },
      { name: "Weekly", used: 31, resets_at: "2026-10-21T12:00:00.5Z" },
      { name: "Opus", used: 40, model: "opus" },
      { name: "Sonnet", used: 10, model: "sonnet" },
    ],
    ...fields,
  });

  it("the windows for every model and for this one, with what is left and the time to the reset", () => {
    expect(limitWindows(reading(), "opus", now)).toEqual([
      { name: "5h", left: 14, resetsIn: (2 * 60 + 41) * 60_000 },
      { name: "Weekly", left: 69, resetsIn: 3 * 24 * 3_600_000 + 500 },
      { name: "Opus", left: 60, resetsIn: null },
    ]);
  });

  it("a window past its reset is whole again; used over 100 leaves 0", () => {
    const past = reading({ windows: [{ name: "5h", used: 90, resets_at: "2026-10-18T11:00:00Z" }, { name: "Weekly", used: 120 }] });
    expect(limitWindows(past, "opus", now)).toEqual([
      { name: "5h", left: 100, resetsIn: -3_600_000 },
      { name: "Weekly", left: 0, resetsIn: null },
    ]);
  });

  it("checking until the first reading; no reading means no limits; Go's zero time is no time", () => {
    expect(limitWindows(reading({ state: "checking", read_at: undefined }), "opus", now)).toBeNull();
    expect(limitWindows(reading({ state: "error", read_at: "0001-01-01T00:00:00Z" }), "opus", now)).toBeNull();
    expect(limitWindows(undefined, "opus", now)).toEqual([]);
    expect(limitWindows(reading({ windows: [{ name: "5h", used: 1, resets_at: "0001-01-01T00:00:00Z" }] }), "opus", now)).toEqual([{ name: "5h", left: 99, resetsIn: null }]);
  });

  it("feeds the meters: the picture's 5-HOUR 86 % with the warning", () => {
    const { meters } = meterModel({ items: ["limit_5h", "limit_weekly", "resets"], limits: limitWindows(reading(), "opus", now) });
    expect(meters.map((m) => [m.label, m.text, m.warn, m.resets])).toEqual([
      ["5-HOUR", "86%", true, "resets 2h41m"],
      ["WEEK", "31%", false, "resets 3d0h"],
    ]);
  });
});
