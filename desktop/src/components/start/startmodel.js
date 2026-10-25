// The start screens' rules (desktop-design 2.5, 2.9, 4.12), kept apart from
// the components so they are unit-tested: the gate's words, which screen the
// middle column shows, the init prompt's keys, and the landing page's LSP /
// MCP / skill rows.

export const STARTING = "Starting Crush…";
export const TRY_AGAIN = "Try again";

// The engine gate's words for a join that failed before the window ever
// joined (2.9, 4.12): the body is always the error's own message.
export function gateWords(error) {
  const body = error?.message ?? "";
  switch (error?.code) {
    case "crush_not_found":
      // Phase 5 points at Settings instead (2.9).
      return { title: "Crush not found", body, fix: "Install Crush, or set CRUSH_DESKTOP_CRUSH to its path." };
    case "crush_too_old":
      return { title: "Crush is too old", body, fix: "Update Crush, then press Try again." };
    default:
      return { title: "Crush could not open this project", body, fix: "" };
  }
}

// What the gate shows: null once the window has joined (the conversation
// shows, and stays through a reconnect, 2.9); "blank" while the first join is
// young; "starting" after 400 ms of it; "failed" with the words above.
export function gateView(conn) {
  if (conn.workspace) return null;
  if (conn.state === "failed") return { kind: "failed", ...gateWords(conn.error) };
  if (conn.state === "connecting" && conn.slow) return { kind: "starting" };
  if (conn.state === "connected") return null;
  return { kind: "blank" };
}

// Which start screen the middle column shows once joined (the terminal
// decides the same way at start, internal/ui/model/ui.go:522-528, and shows
// the opening line instead of the chat while a switch is pending,
// draw.go:76-81,133-136):
//   "opening"    a chat is being opened (LIST-034)
//   "chat"       a chat is shown: no start screen
//   "hold"       nothing to show yet: the list, a launch chat (-s / -C) or the
//                init question is still on its way, so no screen flashes
//   "onboarding" no usable model: the agent is not ready and no provider is set up
//   "init"       the project still needs initializing (START-002)
//   "landing"    otherwise (START-003)
export function chooseScreen({ currentId, opening, loaded, launch, needsInit, agentReady, configured }) {
  if (opening) return "opening";
  if (currentId) return "chat";
  if (!loaded || launch === "pending" || needsInit === null) return "hold";
  if (agentReady === false && configured === false) return "onboarding";
  if (needsInit === true) return "init";
  return "landing";
}

// The init prompt's keys (internal/ui/model/keys.go:323-338): Enter answers
// with the chosen button; ← → Tab switch; y Y yes; n N Esc Alt+Esc no.
export function initKey(event) {
  const k = event.key;
  if (event.ctrlKey || event.metaKey) return null;
  if (k === "Escape") return "no";
  if (event.altKey) return null;
  if (k === "Enter") return "enter";
  if (k === "ArrowLeft" || k === "ArrowRight" || (k === "Tab" && !event.shiftKey)) return "switch";
  // Y as the terminal's Yep!; I for the button's word, Initialize (owner, 2026-10-20).
  if (k === "y" || k === "Y" || k === "i" || k === "I") return "yes";
  if (k === "n" || k === "N") return "no";
  return null;
}

// common.FormatReasoningEffort (internal/ui/common/elements.go:43-48).
export function formatEffort(effort) {
  if (effort === "xhigh") return "X-High";
  const text = String(effort ?? "");
  return text ? text[0].toUpperCase() + text.slice(1).toLowerCase() : "";
}

// The provider's display name: as configured, else the catalog's, else its id.
export function providerName(id, configured = {}, catalog = []) {
  if (!id) return "";
  return configured[id] || catalog.find((p) => p?.id === id)?.name || id;
}

// lsp.ServerState travels as a number (internal/lsp/client.go:303-311 has no
// text form); a name is taken as it is.
const LSP_STATES = ["unstarted", "starting", "ready", "error", "stopped", "disabled"];
export function lspState(state) {
  return typeof state === "number" ? (LSP_STATES[state] ?? "") : String(state ?? "");
}

// The landing's LSP rows (lspList, internal/ui/model/lsp.go:151-200), sorted
// by name. icon is an Icon name; tone "error" or "warn" colors it.
export function lspRows(lsps) {
  const rows = [];
  for (const [key, info] of Object.entries(lsps ?? {})) {
    const name = info?.name || key;
    const d = info?.diagnostics ?? {};
    switch (lspState(info?.state)) {
      case "unstarted":
        rows.push({ name, icon: "circle", text: "unstarted" });
        break;
      case "stopped":
        rows.push({ name, icon: "circle", text: "stopped" });
        break;
      case "starting":
        rows.push({ name, icon: "half-circle", text: "starting..." });
        break;
      case "ready":
        rows.push({ name, icon: "dot", text: "", counts: { error: d.error ?? 0, warning: d.warning ?? 0, hint: d.hint ?? 0, information: d.information ?? 0 } });
        break;
      case "error":
        rows.push({ name, icon: "cross", tone: "error", text: info?.error ? `error: ${info.error}` : "error" });
        break;
      case "disabled":
        rows.push({ name, icon: "offline", text: "disabled" });
        break;
      default:
        break; // the terminal skips a state it does not know
    }
  }
  return rows.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
}

// The landing's MCP rows (mcpList, internal/ui/model/mcp.go:199-262), sorted by name.
export function mcpRows(mcps) {
  const rows = [];
  for (const [key, info] of Object.entries(mcps ?? {})) {
    const name = (info?.name || key) === "docker" ? "Docker MCP" : info?.name || key;
    switch (info?.state) {
      case "starting":
        rows.push({ name, icon: "half-circle", text: "starting..." });
        break;
      case "connected": {
        const parts = [];
        if (info.tool_count > 0) parts.push(`${info.tool_count} tools`);
        if (info.prompt_count > 0) parts.push(`${info.prompt_count} prompts`);
        if (info.resource_count > 0) parts.push(`${info.resource_count} resources`);
        if (info.channel) parts.push("channel");
        rows.push({ name, icon: "dot", text: parts.join(" ") });
        break;
      }
      case "error":
        rows.push({ name, icon: "cross", tone: "error", text: info.error ? `error: ${info.error}` : "error" });
        break;
      case "needs auth":
        rows.push({ name, icon: "info", tone: "warn", text: "needs authentication" });
        break;
      case "disabled":
        rows.push({ name, icon: "offline", text: "disabled" });
        break;
      default:
        rows.push({ name, icon: "circle", text: "" });
    }
  }
  return rows.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
}

// The landing's skill rows (skillsList, internal/ui/model/skills.go:100-140), sorted by name.
export function skillRows(skills) {
  return (skills ?? [])
    .filter((s) => s && s.name)
    .map((s) => ({ name: s.name, icon: "dot", text: "" }))
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
}

// At most max rows: when more are listed, the last place says "…and N more",
// N being every row not shown (the terminal's skills list counts this way,
// skills.go:125-131; its LSP and MCP lists say one fewer, lsp.go:196, mcp.go:257).
export function fitRows(rows, max) {
  const room = Math.max(1, Math.floor(max));
  if (rows.length <= room) return { shown: rows, more: 0 };
  const shown = rows.slice(0, room - 1);
  return { shown, more: rows.length - shown.length };
}

// LIST-034: the opening line (sessionLoadingView, internal/ui/model/session_load.go:580-594).
export function openingText(title) {
  const words = String(title ?? "").split(/\s+/u).filter(Boolean).join(" ");
  return words ? `Opening ${words}…` : "Opening conversation…";
}

export function openingElapsed(startedAt, now) {
  const seconds = Math.floor((now - startedAt) / 1000);
  return seconds >= 1 ? `${seconds}s` : "";
}

// The folder and its Git branch, as one line.
export function pathLine(path, branch) {
  return branch ? `${path} · ${branch}` : String(path ?? "");
}

// The last part of a path.
export function baseName(path) {
  const parts = String(path ?? "").split(/[\\/]/u).filter(Boolean);
  return parts[parts.length - 1] ?? "";
}
