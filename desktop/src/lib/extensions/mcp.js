// MCP toggles and sign-in rules (desktop-design-5-6 2.9;
// internal/ui/model/mcp.go:280-363, mcp_auth.go:45-96).

// The status text of a server (mcp.go:343-363).
export function statusText(info, { locallyDisabled = false, globallyDisabled = false } = {}) {
  if (locallyDisabled || globallyDisabled) return "disabled";
  const state = String(info?.state ?? "");
  switch (state) {
    case "starting":
      return "starting";
    case "connected":
      return "connected";
    case "error":
      return info?.error ? `error: ${info.error}` : "error";
    case "needs auth":
    case "needs_auth":
      return "needs authentication";
    case "disabled":
      return "disabled";
    default:
      return "offline";
  }
}

// The side panel's StatusIcon kind for a status text (5.2, DLG-237).
export function markOf(text) {
  if (text === "connected") return "online";
  if (text === "starting") return "busy";
  if (text.startsWith("error")) return "error";
  if (text === "needs authentication") return "auth";
  if (text === "disabled") return "disabled";
  return "offline";
}

// Pending sign-ins this repository has not disabled (mcp_auth.go:45-96).
export function pendingHere(pending, disabled, enabled) {
  const off = new Set(disabled ?? []);
  const on = new Set(enabled ?? []);
  return (pending ?? []).filter((p) => !off.has(p.name) || on.has(p.name));
}

export const listOf = (answer) => (Array.isArray(answer) ? answer : Array.isArray(answer?.names) ? answer.names : []);
