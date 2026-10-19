// The terminal's composer footer and model info as words for the right
// panel (SIDE-006, SIDE-014, SIDE-019 to SIDE-024): ports of
// internal/ui/model/composer_footer.go:99-139,250-349, sidebar.go:25-160 and
// internal/ui/common/elements.go:43-149. Pure: the panel gives it the
// stores' values, and limit windows already measured by limitWindows
// ({name, left, resetsIn}: resetsIn the milliseconds to the reset, or null).
//
//   FOOTER_CHOICES, DEFAULT_FOOTER_ITEMS, footerItems(config)   the Customize rows and the chosen ids
//   footerAnswer(picked)                                          what Customize saves
//   cliKind(agent, providers), limitWindows(reading, model, now)  the agent CLI's windows from limits.byKind
//   footerFields({...}) → [{id, text}], footerText(fields)        renderFooterItems, joined with " · "
//   meterModel({...}) → {meters, lines, tokens}                   the panel's meters, detail lines and strip count
//   modelInfo({...})                                              the details overlay's model block
//   goFixed, footerTokenCount, shortDuration, formatReasoningEffort
import { CLI_KINDS, providerKind } from "./compaction.js";

export const FOOTER = {
  model: "model",
  settings: "settings",
  tokens: "tokens",
  context: "context",
  limit5h: "limit_5h",
  limitWeekly: "limit_weekly",
  limitOther: "limit_other",
  resets: "resets",
  none: "none", // a footer left empty on purpose: no list at all means the defaults
};

// The Customize Composer rows (composer_footer.go:116-125).
export const FOOTER_CHOICES = [
  { id: FOOTER.model, label: "Model name" },
  { id: FOOTER.settings, label: "Reasoning, fast and Ultracode" },
  { id: FOOTER.tokens, label: "Tokens used" },
  { id: FOOTER.context, label: "Context left" },
  { id: FOOTER.limit5h, label: "5-hour usage left" },
  { id: FOOTER.limitWeekly, label: "Weekly usage left" },
  { id: FOOTER.limitOther, label: "Other usage limits" },
  { id: FOOTER.resets, label: "When usage limits reset" },
];

// Every stat except reset times.
export const DEFAULT_FOOTER_ITEMS = [
  FOOTER.model,
  FOOTER.settings,
  FOOTER.tokens,
  FOOTER.context,
  FOOTER.limit5h,
  FOOTER.limitWeekly,
  FOOTER.limitOther,
];

// options.tui.composer_footer, else the defaults. A saved empty list shows
// nothing, as a saved ["none"] does.
export function footerItems(config) {
  const list = config?.options?.tui?.composer_footer;
  return Array.isArray(list) ? list : DEFAULT_FOOTER_ITEMS;
}

// The ids a Customize answer saves: the picked ones in the rows' order, or
// ["none"] when nothing is picked (composer_footer.go:162-170).
export function footerAnswer(picked) {
  const items = FOOTER_CHOICES.filter((c) => picked.includes(c.id)).map((c) => c.id);
  return items.length > 0 ? items : [FOOTER.none];
}

// Go's strconv 'f' formatting: correctly rounded, and a value exactly half
// way rounds to even (JavaScript's toFixed rounds it up).
export function goFixed(value, digits = 0) {
  const x = Number(value) || 0;
  const sign = x < 0 ? "-" : "";
  const abs = Math.abs(x);
  // Exactly half way at `digits` decimals means abs * 2^(digits+1) is an odd integer.
  const halves = abs * 2 ** (digits + 1);
  if (Number.isInteger(halves) && halves % 2 === 1) {
    const scaled = Math.floor(abs * 10 ** digits);
    const even = scaled % 2 === 0 ? scaled : scaled + 1;
    return sign + (even / 10 ** digits).toFixed(digits);
  }
  const text = abs.toFixed(digits);
  return /[1-9]/.test(text) ? sign + text : text;
}

// "12.3k", "1.5m", "999" (composer_footer.go:338-349).
export function footerTokenCount(tokens) {
  const n = Math.trunc(Number(tokens) || 0);
  let divisor;
  let suffix;
  if (n >= 1_000_000) [divisor, suffix] = [1_000_000, "m"];
  else if (n >= 1_000) [divisor, suffix] = [1_000, "k"];
  else return String(n);
  return goFixed(n / divisor, 2).replace(/0+$/u, "").replace(/\.+$/u, "") + suffix;
}

// The two largest units of a time span: "2h41m", "3d4h", "7m" (at least
// "1m"); minutes rounded half away from zero (sidebar.go:149-159).
export function shortDuration(ms) {
  const m = Math.floor((Math.max(0, Number(ms) || 0) + 30_000) / 60_000);
  if (m >= 24 * 60) return `${Math.floor(m / (24 * 60))}d${Math.floor((m % (24 * 60)) / 60)}h`;
  if (m >= 60) return `${Math.floor(m / 60)}h${m % 60}m`;
  return `${Math.max(m, 1)}m`;
}

// "X-High", else the effort in title case (elements.go:43-48).
export function formatReasoningEffort(effort) {
  if (effort === "xhigh") return "X-High";
  return String(effort ?? "")
    .toLowerCase()
    .replace(/(^|[^\p{L}\p{N}'])(\p{L})/gu, (_, before, letter) => before + letter.toUpperCase());
}

// A window's id among the footer's stats.
function windowId(name) {
  if (name === "5h") return FOOTER.limit5h;
  if (name === "Weekly") return FOOTER.limitWeekly;
  return FOOTER.limitOther;
}

// The tokens and context words of a chat (composer_footer.go:289-317).
function tokenWords(session, limit) {
  const used = Math.max(0, (Number(session.prompt_tokens) || 0) + (Number(session.completion_tokens) || 0));
  let prefix = session.estimated_usage ? "~" : "";
  const budget = session.context_budget ?? {};
  const estimatedLimit = budget.estimated === true && Number(budget.tokens) === limit;
  let tokens = prefix + footerTokenCount(used);
  if (limit > 0) tokens += "/" + (estimatedLimit ? "~" : "") + footerTokenCount(limit);
  tokens += " tokens";
  let context = "Context unknown";
  let left = null;
  if (limit > 0) {
    if (estimatedLimit) prefix = "~";
    left = Math.max(0, Math.min(100, 100 - (used * 100) / limit));
    context = `Context ${prefix}${goFixed(left, 0)}% left`;
  }
  return { used, tokens, context, left };
}

// A limit window's words: "5h 14% left", and " resets 2h41m" when asked for
// and the reset is still ahead (composer_footer.go:318-334).
function windowWords(window, showResets) {
  const text = `${window.name} ${goFixed(window.left, 0)}% left`;
  const resets = showResets && window.resetsIn != null && window.resetsIn > 0 ? `resets ${shortDuration(window.resetsIn)}` : "";
  return { text, resets };
}

// renderFooterItems: the chosen stats in the footer's fixed order. agent is
// AgentInfo (model and model_cfg), session the shown chat or null, limit
// compactionLimit's tokens, windows the agent CLI's limit windows.
export function footerFields({ items = DEFAULT_FOOTER_ITEMS, agent = null, session = null, limit = 0, windows = [] } = {}) {
  const show = (id) => items.includes(id);
  const fields = [];
  if (agent?.model_cfg) {
    const name = agent.model?.name || agent.model_cfg.model || "";
    let field = name && show(FOOTER.model) ? name : "";
    if (show(FOOTER.settings)) {
      const settings = [];
      const effort = agent.model_cfg.reasoning_effort || agent.model?.default_reasoning_effort || "";
      if (effort) settings.push(effort.toLowerCase());
      else if (agent.model_cfg.think) settings.push("thinking");
      if (agent.model_cfg.service_tier === "fast") settings.push("fast");
      if (agent.model_cfg.ultracode) settings.push("ultracode");
      if (settings.length > 0) field += (field ? " " : "") + settings.join(" ");
    }
    if (field) fields.push({ id: FOOTER.model, text: field });
  }
  if (session) {
    const words = tokenWords(session, limit);
    if (show(FOOTER.tokens)) fields.push({ id: FOOTER.tokens, text: words.tokens });
    if (show(FOOTER.context)) fields.push({ id: FOOTER.context, text: words.context });
  }
  for (const window of windows) {
    const id = windowId(window.name);
    if (!show(id)) continue;
    const words = windowWords(window, show(FOOTER.resets));
    fields.push({ id, name: window.name, text: words.resets ? `${words.text} ${words.resets}` : words.text });
  }
  return fields;
}

export function footerText(fields) {
  return fields.map((f) => f.text).join(" · ");
}

// The panel's meters (SIDE-007, SIDE-014, SIDE-019 to SIDE-024):
//   meters  in the footer's order, CONTEXT then one per limit window:
//           {id, label, used (0-100, the bar), text (the value shown), warn, resets}
//   lines   the detail popover's lines: "Context 38% left · ~124k/200k tokens",
//           "5h 14% left · resets 2h41m", …
//   tokens  the token count for the strip ("" when not chosen or no chat)
// limits are the CLI's windows for the model ({name, left, resetsIn}); null
// while the CLI's first reading is on its way ("checking…"), [] when the
// model's CLI reports none (an API model). Without a chat the context is
// unknown ("–"). The shown value is the used part, 100 minus the rounded
// left part, so the two always add up to 100.
export function meterModel({ items = DEFAULT_FOOTER_ITEMS, session = null, limit = 0, limits = [] } = {}) {
  const show = (id) => items.includes(id);
  const meters = [];
  const lines = [];
  const words = session ? tokenWords(session, limit) : null;
  const contextLine = [];
  if (show(FOOTER.context)) {
    const left = words?.left ?? null;
    meters.push({
      id: FOOTER.context,
      label: "CONTEXT",
      // The owner's call (2026-10-19): the meters show what is left.
      left: left === null ? 0 : left,
      text: left === null ? "–" : `${goFixed(left, 0)}% left`,
      // As the sidebar's context line (elements.go:144): over 80 % used.
      warn: left !== null && left < 20,
      resets: "",
    });
    contextLine.push(words ? words.context : "Context unknown");
  }
  const tokens = show(FOOTER.tokens) && words ? words.tokens : "";
  if (tokens) contextLine.push(tokens);
  if (contextLine.length > 0) lines.push(contextLine.join(" · "));
  if (limits === null) {
    for (const [id, label, name] of [
      [FOOTER.limit5h, "5-HOUR", "5h"],
      [FOOTER.limitWeekly, "WEEK", "Weekly"],
    ]) {
      if (!show(id)) continue;
      meters.push({ id, label, left: 0, text: "checking…", warn: false, resets: "" });
      lines.push(`${name} checking…`);
    }
    return { meters, lines, tokens };
  }
  for (const window of limits) {
    const id = windowId(window.name);
    if (!show(id)) continue;
    const limitWords = windowWords(window, show(FOOTER.resets));
    meters.push({
      id: id === FOOTER.limitOther ? `${id}:${window.name}` : id,
      label: id === FOOTER.limit5h ? "5-HOUR" : id === FOOTER.limitWeekly ? "WEEK" : String(window.name).toUpperCase(),
      left: window.left,
      text: `${goFixed(window.left, 0)}% left`,
      // Under 20 % left (sidebar.go:136-139).
      warn: window.left < 20,
      resets: limitWords.resets,
    });
    lines.push(limitWords.resets ? `${limitWords.text} · ${limitWords.resets}` : limitWords.text);
  }
  return { meters, lines, tokens };
}

// The kind whose limits apply: the agent's provider type when it is an
// agent CLI (sidebar.go:88-99), else "".
export function cliKind(agent, providers) {
  const provider = agent?.model_cfg?.provider;
  if (!provider) return "";
  const kind = providerKind(provider, providers);
  return CLI_KINDS.has(kind) ? kind : "";
}

// Go's RFC 3339 time (up to nine fraction digits) in ms, or null for none or
// Go's zero time.
const RFC3339 = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/iu;
function goTime(value) {
  const m = typeof value === "string" ? RFC3339.exec(value.trim()) : null;
  if (!m || m[1].startsWith("0001-01-01")) return null;
  const ms = Date.parse(m[1] + (m[3].toUpperCase() === "Z" ? "Z" : m[3]));
  return Number.isFinite(ms) ? ms + Math.floor(Number((m[2] ?? "").padEnd(9, "0")) / 1e6) : null;
}

// The windows of a kind's reading (limits.byKind[kind], CLILimits) that
// apply to the model, measured at now: those for every model and those for
// this one (cliagent.Limits, internal/agent/cliagent/limits.go:145-157); a
// window past its reset is whole again (Limit.Left, limits.go:30-36). null
// while the kind is checking (a reading without read_at); [] when the kind
// reports no limits at all.
export function limitWindows(reading, model, now = Date.now()) {
  if (!reading) return [];
  if (goTime(reading.read_at) === null) return null;
  return (reading.windows ?? [])
    .filter((w) => !w.model || w.model === model)
    .map((w) => {
      const resets = goTime(w.resets_at);
      const left = resets !== null && now > resets ? 100 : Math.max(0, 100 - (Number(w.used) || 0));
      return { name: String(w.name ?? ""), left, resetsIn: resets === null ? null : resets - now };
    });
}

const ABACUS = "abacus";

// config.AbacusAPIFormat from the model id (the window's AgentInfo has no
// provider options).
function abacusFormat(id) {
  if (id.startsWith("claude-")) return "anthropic";
  if (/^(gpt-5|gpt-6|o3|o4)/u.test(id)) return "responses";
  return "openai";
}

function supportsThinkingToggle(provider, model) {
  if (!model.can_reason || (model.reasoning_levels ?? []).length > 0) return false;
  if (provider.id !== ABACUS) return true;
  const id = String(model.id ?? "").toLowerCase();
  return abacusFormat(id) === "anthropic" || id.includes("deepseek") || id.startsWith("zai-org/") || id.includes("minimax");
}

function supportsFastMode(provider, model) {
  if (provider.type === "claude-code" || provider.type === "codex-cli") return true;
  const id = String(model.id ?? "");
  return provider.id === ABACUS && abacusFormat(id) === "responses" && !id.includes("nano") && !id.endsWith("-pro") && /^(gpt-|o3|o4)/u.test(id);
}

function supportsUltracode(provider, model) {
  return provider.type === "claude-code" && (model.reasoning_levels ?? []).length > 0;
}

// "(124K)": one decimal, ".0" dropped (elements.go:110-128).
function shortTokens(tokens) {
  let text;
  if (tokens >= 1_000_000) text = `${goFixed(tokens / 1_000_000, 1)}M`;
  else if (tokens >= 1_000) text = `${goFixed(tokens / 1_000, 1)}K`;
  else text = String(tokens);
  return text.replace(/\.0([KM])$/u, "$1");
}

// The details overlay's model block (sidebar.go:25-86, elements.go:58-149):
//   {name, via ("via Claude Code" or ""), reasoning ("Reasoning Max · FAST"),
//    usage: {percent: "62%", tokens: "(124K)", cost: "$0.00", warn} or null,
//    limits: [{name, left: " 14%", warn, resets}]}
// The provider comes from GET providers; one that list lacks keeps its id
// as its type and shows no "via" name.
export function modelInfo({ agent = null, providers = [], session = null, limits = [] } = {}) {
  const model = agent?.model_cfg ? agent : null;
  if (!model) return { name: "", via: "", reasoning: "", usage: null, limits: [] };
  const cfg = model.model_cfg;
  const catwalk = model.model ?? {};
  const found = (Array.isArray(providers) ? providers : []).find((p) => p?.id === cfg.provider);
  const provider = found ?? { id: cfg.provider, type: cfg.provider, name: "" };
  const parts = [];
  if (catwalk.can_reason) {
    if ((catwalk.reasoning_levels ?? []).length === 0) {
      if (!supportsThinkingToggle(provider, catwalk)) parts.push("Thinking");
      else parts.push(cfg.think ? "Thinking On" : "Thinking Off");
    } else {
      parts.push(`Reasoning ${formatReasoningEffort(cfg.reasoning_effort || catwalk.default_reasoning_effort || "")}`);
    }
  }
  if (supportsFastMode(provider, catwalk) && cfg.service_tier === "fast") parts.push("FAST");
  if (supportsUltracode(provider, catwalk) && cfg.ultracode) parts.push("Ultracode");
  let usage = null;
  if (session) {
    const tokens = (Number(session.completion_tokens) || 0) + (Number(session.prompt_tokens) || 0);
    const window = Number(catwalk.context_window) || 0;
    const percentage = window > 0 ? (tokens / window) * 100 : 0;
    usage = {
      percent: `${session.estimated_usage ? "~" : ""}${Math.trunc(percentage)}%`,
      tokens: `(${shortTokens(tokens)})`,
      cost: `$${goFixed(Number(session.cost) || 0, 2)}`,
      warn: percentage > 80,
    };
  }
  return {
    name: catwalk.name ?? "",
    via: provider.name ? `via ${provider.name}` : "",
    reasoning: parts.join(" · "),
    usage,
    limits: limits.map((w) => ({
      name: w.name,
      left: `${goFixed(w.left, 0)}%`,
      warn: w.left < 20,
      resets: w.resetsIn != null && w.resetsIn > 0 ? `resets ${shortDuration(w.resetsIn)}` : "",
    })),
  };
}
