// The hook lines above a tool (CHAT-055): "Hook name [matcher] → OK /
// Rewrote Output / Denied reason". Port of chat/tool_hooks.go:15-179.
import { cells } from "../diff.js";
import { decode, decodeAs, truncate } from "./go.js";

const HOOK_INFO = { name: "string", matcher: "string", decision: "string", halt: "bool", reason: "string", input_rewrite: "bool" };
const HOOK_META = {
  hook_count: "int",
  decision: "string",
  halt: "bool",
  reason: "string",
  input_rewrite: "bool",
  hooks: ["array", HOOK_INFO],
};

// The "hook" field, null when the metadata has none.
function hookField(value, state) {
  const decoded = decodeAs(value, HOOK_META);
  if (!decoded.ok) state.ok = false;
  return decoded.value;
}
hookField.zero = null;

// The name column is at most this wide.
export const MAX_HOOK_NAME = 30;

// isLikelyPath: absolute, or with a slash and no shell characters.
export function isLikelyPath(s) {
  if (s === "" || /[ \t\n¶'"|&;<>$`*?(){}[\]\\]/.test(s)) return false;
  return s.startsWith("/") || s.includes("/");
}

// truncateHookName: a path keeps its end ("…/format.sh"), anything else its start.
export function truncateHookName(name, width) {
  if (cells(name) <= width) return name;
  if (isLikelyPath(name)) {
    const chars = [...name];
    let cut = cells(name) - width + 1;
    let i = 0;
    while (i < chars.length && cut > 0) cut -= cells(chars[i++]);
    return "…" + chars.slice(i).join("");
  }
  return truncate(name, width, "…");
}

// hookDetail: OK (with "Rewrote Output" when the hook changed the input), or
// Denied and the reason.
export function hookDetail(hook) {
  if (hook.decision === "deny") return { text: "Denied", tone: "denied", reason: hook.reason };
  return { text: "OK", tone: "ok", rewrote: hook.input_rewrite ? "Rewrote Output" : "" };
}

// The lines for a result's metadata: [{name, matcher, detail, denied}], []
// when no hook ran. Newlines in a name show as "¶".
export function hookLines(metadata) {
  if (!metadata) return [];
  const { value, ok } = decode(metadata, { hook: hookField });
  if (!ok || !value.hook || value.hook.hooks.length === 0) return [];
  return value.hook.hooks.map((h) => ({
    name: truncateHookName(h.name.replaceAll("\n", "¶"), MAX_HOOK_NAME),
    matcher: h.matcher,
    detail: hookDetail(h),
    denied: h.decision === "deny",
  }));
}
