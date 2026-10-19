// The typing box's modes as pure rules (TYPE-002, TYPE-003, TYPE-023 to
// TYPE-025) and the model button's words (SIDE-006's label). The composer
// store runs them; nothing here talks to the engine.

// internal/ui/model/editor.go:450-464.
export const READY_PLACEHOLDERS = ["Ready!", "Ready...", "Ready?", "Ready for instructions"];
export const WORKING_PLACEHOLDERS = ["Working!", "Working...", "Brrrrr...", "Prrrrrrrr...", "Processing...", "Thinking..."];

// input_mode.go:57-60 and :16-18.
export const PLAN_BANNER = "Plan with Crush before generating any code.";
export const YOLO_BANNER = "Skip permission prompts. System level commands will be blocked.";
export const CODE_INFO = "input mode: code";
export const YOLO_OFF = "Yolo mode disabled";
export const BUSY_SWITCH = "Agent is busy, please wait before switching input mode...";
export const SWITCHING = "Switching input mode, one moment...";

// The engine's agent ids (config.AgentCoder, config.AgentPlan).
export const AGENT_FOR = { code: "coder", plan: "plan" };

// randomizePlaceholders (composer.go:73-77): one ready and one working text,
// picked again after every send.
export function pickPlaceholders(random = Math.random) {
  const pick = (list) => list[Math.min(list.length - 1, Math.floor(random() * list.length))];
  return { ready: pick(READY_PLACEHOLDERS), working: pick(WORKING_PLACEHOLDERS) };
}

// composer.go:82-95, in this order: bang, YOLO coding, busy, plan, ready.
export function placeholderFor({ bang = false, plan = false, yolo = false, busy = false, picks }) {
  if (bang) return "Run a shell command";
  if (!plan && yolo) return "Go crazy";
  if (busy) return picks.working;
  if (plan) return "Let's plan";
  return picks.ready;
}

// The one mark the field shows (TYPE-002): bang beats plan, plan beats YOLO.
export function markOf({ bang = false, plan = false, yolo = false }) {
  if (bang) return "bang";
  if (plan) return "plan";
  if (yolo) return "yolo";
  return "normal";
}

// Shift+Tab (toggleInputMode, input_mode.go:16-36) as the steps to take:
//   {warn}                                busy or a switch in flight
//   {yoloOn, claim, switchTo: "code"}     from plan: YOLO on when it was off, and the
//                                         cycle claims it (claim) only then
//   {yoloOff, info}                       from code with YOLO the cycle turned on
//   {switchTo: "plan"}                    otherwise
export function cycleSteps({ plan, skip, cycleYolo, busy = false, switching = false }) {
  if (busy || switching) return { warn: BUSY_SWITCH };
  if (plan) return { yoloOn: !skip, claim: !skip, switchTo: "code" };
  if (skip && cycleYolo) return { yoloOff: true, info: CODE_INFO };
  return { switchTo: "plan" };
}

// The banner after a switch landed (applyModeSwitch, input_mode.go:96-123):
// yolo is whether permission skipping was on when the switch began.
export function bannerFor(target, yolo) {
  if (target === "plan") return { kind: "plan", text: PLAN_BANNER };
  if (yolo) return { kind: "yolo", text: YOLO_BANNER };
  return { kind: "info", text: CODE_INFO };
}

// The footer items the model button shows (composer_footer.go:99-139): unset
// is every item but "resets", so both of the button's.
export const DEFAULT_FOOTER = ["model", "settings", "tokens", "context", "limit_5h", "limit_weekly", "limit_other"];

// The model button's label (composer_footer.go:255-290): the model's name
// (or its id), then the effort (or "thinking"), "fast" and "ultracode". The
// terminal joins the settings with spaces; the button joins every part with
// " · ", as the approved picture's "Opus 5.5 · max".
export function modelLabel(agent, items = null) {
  if (!agent?.model && !agent?.model_cfg) return "";
  const list = Array.isArray(items) ? items : DEFAULT_FOOTER;
  const show = (id) => list.includes(id);
  const cfg = agent.model_cfg ?? {};
  const parts = [];
  const name = agent.model?.name || cfg.model || "";
  if (name && show("model")) parts.push(name);
  if (show("settings")) {
    const effort = cfg.reasoning_effort || agent.model?.default_reasoning_effort || "";
    if (effort) parts.push(effort.toLowerCase());
    else if (cfg.think) parts.push("thinking");
    if (cfg.service_tier === "fast") parts.push("fast");
    if (cfg.ultracode) parts.push("ultracode");
  }
  return parts.join(" · ");
}

// The model button's words (SIDE-006): "Unknown" until the agent has a model;
// then the label, which may be empty when the footer items hide both parts.
export function modelButtonText(agent, items = null) {
  if (!agent?.model && !agent?.model_cfg) return "Unknown";
  return modelLabel(agent, items);
}

// "exit" or "quit" asks to quit (TYPE-007, keypress.go:342-345).
export function isQuitWord(text) {
  const value = String(text ?? "").trim();
  return value === "exit" || value === "quit";
}
