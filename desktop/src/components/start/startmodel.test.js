import { describe, expect, it } from "vitest";
import {
  baseName,
  chooseScreen,
  fitRows,
  formatEffort,
  gateView,
  gateWords,
  initKey,
  lspRows,
  lspState,
  mcpRows,
  modelLine,
  openingElapsed,
  openingText,
  pathLine,
  providerName,
  skillRows,
  STARTING,
  TRY_AGAIN,
} from "./startmodel.js";

describe("the engine gate (2.9, 4.12)", () => {
  it("names each failure and keeps the error's own message as the body", () => {
    expect(gateWords({ code: "crush_not_found", message: "crush was not found on PATH" })).toEqual({
      title: "Crush not found",
      body: "crush was not found on PATH",
      fix: "Install Crush, or set CRUSH_DESKTOP_CRUSH to its path.",
    });
    expect(gateWords({ code: "crush_too_old", message: "crush 0.9 has no engine command" })).toEqual({
      title: "Crush is too old",
      body: "crush 0.9 has no engine command",
      fix: "Update Crush, then press Try again.",
    });
    // START-008: a missing --cwd folder shows the terminal's words as they come.
    const missing = "failed to change directory: chdir /tmp/home/missing: no such file or directory";
    expect(gateWords({ code: "bad_path", message: missing })).toEqual({ title: "Crush could not open this project", body: missing, fix: "" });
    expect(STARTING).toBe("Starting Crush…");
    expect(TRY_AGAIN).toBe("Try again");
  });

  it("shows nothing for the first 400 ms, then the starting line, then the failure", () => {
    expect(gateView({ state: "idle", workspace: null })).toEqual({ kind: "blank" });
    expect(gateView({ state: "connecting", slow: false, workspace: null })).toEqual({ kind: "blank" });
    expect(gateView({ state: "connecting", slow: true, workspace: null })).toEqual({ kind: "starting" });
    expect(gateView({ state: "failed", error: { code: "crush_too_old", message: "old" }, workspace: null })).toMatchObject({ kind: "failed", title: "Crush is too old", body: "old" });
    expect(gateView({ state: "connected", workspace: null })).toBeNull();
  });

  it("never comes back once the window has joined: the conversation stays through a reconnect", () => {
    const joined = { id: "w1" };
    for (const state of ["connected", "reconnecting", "connecting", "failed"]) {
      expect(gateView({ state, slow: true, error: { code: "x", message: "y" }, workspace: joined })).toBeNull();
    }
  });
});

describe("chooseScreen", () => {
  const base = { currentId: "", opening: null, loaded: true, launch: "done", needsInit: false, agentReady: true, configured: true };

  it("shows an opening chat first, then the shown chat", () => {
    expect(chooseScreen({ ...base, opening: { id: "b" }, currentId: "a" })).toBe("opening");
    expect(chooseScreen({ ...base, currentId: "a", needsInit: true })).toBe("chat");
  });

  it("holds while the list, a launch chat or the init answer is on its way", () => {
    expect(chooseScreen({ ...base, loaded: false })).toBe("hold");
    expect(chooseScreen({ ...base, launch: "pending" })).toBe("hold");
    expect(chooseScreen({ ...base, needsInit: null })).toBe("hold");
  });

  it("onboards only when the agent is not ready and no provider is set up (START-001)", () => {
    expect(chooseScreen({ ...base, agentReady: false, configured: false, needsInit: true })).toBe("onboarding");
    expect(chooseScreen({ ...base, agentReady: false, configured: true })).toBe("landing");
    expect(chooseScreen({ ...base, agentReady: true, configured: false })).toBe("landing");
    expect(chooseScreen({ ...base, agentReady: undefined, configured: false })).toBe("landing");
  });

  it("asks to initialize before the landing page (START-002, START-003)", () => {
    expect(chooseScreen({ ...base, needsInit: true })).toBe("init");
    expect(chooseScreen(base)).toBe("landing");
  });
});

describe("initKey (keys.go:323-338)", () => {
  const key = (k, mods = {}) => initKey({ key: k, ctrlKey: false, altKey: false, metaKey: false, shiftKey: false, ...mods });

  it("answers, switches and refuses like the terminal", () => {
    expect(key("Enter")).toBe("enter");
    expect(key("ArrowLeft")).toBe("switch");
    expect(key("ArrowRight")).toBe("switch");
    expect(key("Tab")).toBe("switch");
    expect(key("y")).toBe("yes");
    expect(key("Y", { shiftKey: true })).toBe("yes");
    expect(key("n")).toBe("no");
    expect(key("N", { shiftKey: true })).toBe("no");
    expect(key("Escape")).toBe("no");
    expect(key("Escape", { altKey: true })).toBe("no");
  });

  it("leaves every other key alone", () => {
    expect(key("Tab", { shiftKey: true })).toBeNull();
    expect(key("y", { ctrlKey: true })).toBeNull();
    expect(key("n", { metaKey: true })).toBeNull();
    expect(key("y", { altKey: true })).toBeNull();
    expect(key("Escape", { ctrlKey: true })).toBeNull();
    expect(key("a")).toBeNull();
    expect(key(" ")).toBeNull();
    expect(key("ArrowUp")).toBeNull();
  });
});

describe("the model line (sidebar.go:27-86)", () => {
  const agent = (model, cfg) => ({ is_ready: true, model: { id: "m", name: "Model M", can_reason: false, reasoning_levels: [], ...model }, model_cfg: { model: "m", provider: "p", ...cfg } });

  it("writes the effort as the terminal does", () => {
    expect(formatEffort("xhigh")).toBe("X-High");
    expect(formatEffort("high")).toBe("High");
    expect(formatEffort("MEDIUM")).toBe("Medium");
    expect(formatEffort("")).toBe("");
    expect(formatEffort(undefined)).toBe("");
  });

  it("has nothing to say before the agent has a model", () => {
    expect(modelLine(null)).toBeNull();
    expect(modelLine({ is_ready: false, model: { name: "x" }, model_cfg: {} })).toBeNull();
  });

  it("names the model and its provider", () => {
    expect(modelLine(agent({}, {}), "Fake AI")).toEqual({ name: "Model M", via: "via Fake AI", reasoning: "" });
    expect(modelLine(agent({ name: "" }, { model: "fallback-id" }))).toEqual({ name: "fallback-id", via: "", reasoning: "" });
  });

  it("says how the model reasons", () => {
    expect(modelLine(agent({ can_reason: true, reasoning_levels: ["low", "high"], default_reasoning_effort: "low" }, { reasoning_effort: "high" })).reasoning).toBe("Reasoning High");
    expect(modelLine(agent({ can_reason: true, reasoning_levels: ["low"], default_reasoning_effort: "xhigh" }, {})).reasoning).toBe("Reasoning X-High");
    // A reasoning model without levels or a toggle always thinks.
    expect(modelLine(agent({ can_reason: true }, { provider: "abacus" })).reasoning).toBe("Thinking");
    // With the toggle: on or off.
    expect(modelLine(agent({ can_reason: true }, { provider: "anthropic", think: true })).reasoning).toBe("Thinking On");
    expect(modelLine(agent({ can_reason: true }, { provider: "anthropic", think: false })).reasoning).toBe("Thinking Off");
    expect(modelLine(agent({ can_reason: true, id: "deepseek-r1" }, { provider: "abacus", think: true })).reasoning).toBe("Thinking On");
  });

  it("adds FAST and Ultracode only where the provider has them", () => {
    const claude = agent({ can_reason: true, reasoning_levels: ["high"], default_reasoning_effort: "high" }, { provider: "claude-code", service_tier: "fast", ultracode: true });
    expect(modelLine(claude).reasoning).toBe("Reasoning High · FAST · Ultracode");
    expect(modelLine(agent({}, { provider: "codex-cli", service_tier: "fast", ultracode: true })).reasoning).toBe("FAST");
    expect(modelLine(agent({}, { provider: "openai", service_tier: "fast" })).reasoning).toBe("");
  });

  it("finds the provider's name: configured, else the catalog's, else the id", () => {
    expect(providerName("fake", { fake: "Fake AI" }, [{ id: "fake", name: "Catalog" }])).toBe("Fake AI");
    expect(providerName("openai", {}, [{ id: "openai", name: "OpenAI" }])).toBe("OpenAI");
    expect(providerName("local", {}, [])).toBe("local");
    expect(providerName("", { "": "x" }, [])).toBe("");
  });
});

describe("the landing columns (lsp.go:151-200, mcp.go:199-262, skills.go:100-140)", () => {
  it("reads the LSP state as the number the engine sends", () => {
    expect([0, 1, 2, 3, 4, 5].map(lspState)).toEqual(["unstarted", "starting", "ready", "error", "stopped", "disabled"]);
    expect(lspState("ready")).toBe("ready");
    expect(lspState(9)).toBe("");
  });

  it("lists LSPs by name with the terminal's words and the diagnostics counts", () => {
    const rows = lspRows({
      z: { name: "zls", state: 2, diagnostics: { error: 2, warning: 1 } },
      g: { name: "gopls", state: 1 },
      r: { name: "rust-analyzer", state: 3, error: "crashed" },
      a: { name: "astro", state: 0 },
      s: { name: "svelte", state: 4 },
      d: { name: "deno", state: 5 },
      x: { name: "unknown", state: 9 },
    });
    expect(rows).toEqual([
      { name: "astro", icon: "circle", text: "unstarted" },
      { name: "deno", icon: "offline", text: "disabled" },
      { name: "gopls", icon: "half-circle", text: "starting..." },
      { name: "rust-analyzer", icon: "cross", tone: "error", text: "error: crashed" },
      { name: "svelte", icon: "circle", text: "stopped" },
      { name: "zls", icon: "dot", text: "", counts: { error: 2, warning: 1, hint: 0, information: 0 } },
    ]);
  });

  it("lists MCPs by name with their counts and states", () => {
    const rows = mcpRows({
      docker: { name: "docker", state: "connected", tool_count: 3, prompt_count: 0, resource_count: 2, channel: true },
      b: { name: "bad", state: "error", error: "exit 1" },
      a: { name: "auth", state: "needs auth" },
      o: { name: "off", state: "disabled" },
      s: { name: "slow", state: "starting" },
      q: { name: "quiet", state: "connected", tool_count: 0 },
    });
    expect(rows).toEqual([
      { name: "Docker MCP", icon: "dot", text: "3 tools 2 resources channel" },
      { name: "auth", icon: "info", tone: "warn", text: "needs authentication" },
      { name: "bad", icon: "cross", tone: "error", text: "error: exit 1" },
      { name: "off", icon: "offline", text: "disabled" },
      { name: "quiet", icon: "dot", text: "" },
      { name: "slow", icon: "half-circle", text: "starting..." },
    ]);
  });

  it("lists skills by name", () => {
    expect(skillRows([{ id: "2", name: "write" }, { id: "1", name: "plan" }, { id: "3", name: "" }, null])).toEqual([
      { name: "plan", icon: "dot", text: "" },
      { name: "write", icon: "dot", text: "" },
    ]);
  });

  it("cuts a column to the rows that fit and counts every hidden one", () => {
    const rows = ["a", "b", "c", "d", "e"].map((name) => ({ name }));
    expect(fitRows(rows, 5)).toEqual({ shown: rows, more: 0 });
    expect(fitRows(rows, 9)).toEqual({ shown: rows, more: 0 });
    expect(fitRows(rows, 4)).toEqual({ shown: rows.slice(0, 3), more: 2 });
    expect(fitRows(rows, 2)).toEqual({ shown: rows.slice(0, 1), more: 4 });
    // No room at all still says how many there are.
    expect(fitRows(rows, 0)).toEqual({ shown: [], more: 5 });
    expect(fitRows([], 0)).toEqual({ shown: [], more: 0 });
  });
});

describe("the opening line and the head (LIST-034)", () => {
  it("names the chat being opened, its spaces collapsed", () => {
    expect(openingText("Login   keeps\nredirecting")).toBe("Opening Login keeps redirecting…");
    expect(openingText("")).toBe("Opening conversation…");
    expect(openingText(undefined)).toBe("Opening conversation…");
  });

  it("counts whole seconds once one has passed", () => {
    expect(openingElapsed(1000, 1999)).toBe("");
    expect(openingElapsed(1000, 2000)).toBe("1s");
    expect(openingElapsed(1000, 13_500)).toBe("12s");
  });

  it("writes the folder and its branch", () => {
    expect(pathLine("/tmp/home/proj", "main")).toBe("/tmp/home/proj · main");
    expect(pathLine("/tmp/home/proj", "")).toBe("/tmp/home/proj");
    expect(baseName("/tmp/home/proj/")).toBe("proj");
    expect(baseName("C:\\work\\app")).toBe("app");
    expect(baseName("")).toBe("");
  });
});
