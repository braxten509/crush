// The LSP, MCP and SKILLS rows of the right panel and the details overlay
// (SIDE-009 to SIDE-011; FILES rows are lib/modified.js's fileRows): ports of
// internal/ui/model/lsp.go:96-201, mcp.go:158-262 and skills.go:36-143. Pure.
// Each returns {rows, more}: the rows to draw and the "…and N more" count (0
// for none), cut at max rows as the terminal cuts its lists. The panel passes
// no max (its body scrolls); the details overlay passes 20 (SIDE-003).
//
// Row: {name, status, text, counts?}
//   status  "online" | "busy" | "offline" | "error" | "auth" | "disabled": the
//           terminal's icon (styles.Resource.*Icon)
//   text    the state words ("starting...", "error: …", "disabled", …) or ""
//   counts  LSP: {error, warning, hint, information}, the non-zero ones shown in
//           that order (lspDiagnostics); MCP: {tools, prompts, resources} and
//           channel (mcpCounts)

// strings.Compare: byte order of the UTF-8 text, which is code point order.
export function goCompare(a, b) {
  const x = String(a);
  const y = String(b);
  let i = 0;
  let j = 0;
  while (i < x.length && j < y.length) {
    const p = x.codePointAt(i);
    const q = y.codePointAt(j);
    if (p !== q) return p < q ? -1 : 1;
    i += p > 0xffff ? 2 : 1;
    j += q > 0xffff ? 2 : 1;
  }
  if (i < x.length) return 1;
  if (j < y.length) return -1;
  return 0;
}

// The LSP and MCP lists show max-1 rows and then "…and {len-max} more"
// (lsp.go:194-198, mcp.go:255-259: the count is one short of the rows left
// out, kept as the terminal prints it).
function cutStatus(rows, max) {
  if (max <= 0) return { rows: [], more: 0 };
  if (rows.length > max) return { rows: rows.slice(0, max - 1), more: rows.length - max };
  return { rows, more: 0 };
}

// lsp.ServerState on the wire (internal/lsp/client.go:302-311).
const LSP_STATE = ["unstarted", "starting", "ready", "error", "stopped", "disabled"];

// ws.lsps: name → LSPClientInfo plus diagnostics (workspace.svelte.js).
export function lspRows(lsps, { max = Infinity } = {}) {
  const states = Object.entries(lsps ?? {})
    .map(([key, info]) => ({ ...info, name: info?.name || key }))
    .sort((a, b) => goCompare(a.name, b.name));
  const rows = [];
  for (const l of states) {
    const state = typeof l.state === "number" ? LSP_STATE[l.state] : l.state;
    switch (state) {
      case "unstarted":
      case "stopped":
        rows.push({ name: l.name, status: "offline", text: state });
        break;
      case "starting":
        rows.push({ name: l.name, status: "busy", text: "starting..." });
        break;
      case "ready": {
        const d = l.diagnostics ?? {};
        rows.push({
          name: l.name,
          status: "online",
          text: "",
          counts: { error: d.error || 0, warning: d.warning || 0, hint: d.hint || 0, information: d.information || 0 },
        });
        break;
      }
      case "error":
        rows.push({ name: l.name, status: "error", text: l.error ? `error: ${l.error}` : "error" });
        break;
      case "disabled":
        rows.push({ name: l.name, status: "disabled", text: "disabled" });
        break;
      default:
        break; // any other state is left out (lsp.go:183-184)
    }
  }
  return cutStatus(rows, max);
}

// The name the terminal shows for a server (mcp.go:207-211;
// config.DockerMCPName is "docker").
export const mcpTitle = (name) => (name === "docker" ? "Docker MCP" : name);

// ws.mcps: name → MCPClientInfo; configured: ws.config.mcp (name → {type,
// disabled}). Only configured servers with a state show, sorted by name
// (mcp.go:164-168, config.MCPs.Sorted).
export function mcpRows(mcps, configured, { max = Infinity } = {}) {
  const names = Object.keys(configured ?? {}).sort(goCompare);
  const rows = [];
  for (const name of names) {
    const info = mcps?.[name];
    if (!info) continue;
    const title = mcpTitle(name);
    switch (info.state) {
      case "starting":
        rows.push({ name: title, status: "busy", text: "starting..." });
        break;
      case "connected":
        rows.push({
          name: title,
          status: "online",
          text: "",
          counts: { tools: info.tool_count || 0, prompts: info.prompt_count || 0, resources: info.resource_count || 0, channel: info.channel === true },
        });
        break;
      case "error":
        rows.push({ name: title, status: "error", text: info.error ? `error: ${info.error}` : "error" });
        break;
      case "needs auth":
        rows.push({ name: title, status: "auth", text: "needs authentication" });
        break;
      case "disabled":
        rows.push({ name: title, status: "disabled", text: "disabled" });
        break;
      default:
        rows.push({ name: title, status: "offline", text: "" });
    }
  }
  return cutStatus(rows, max);
}

// "3 tools 1 prompts 2 resources" without the zero counts (mcpCounts,
// mcp.go:182-195), then "channel".
export function mcpCountWords(counts) {
  const parts = [];
  if (counts.tools > 0) parts.push(`${counts.tools} tools`);
  if (counts.prompts > 0) parts.push(`${counts.prompts} prompts`);
  if (counts.resources > 0) parts.push(`${counts.resources} resources`);
  if (counts.channel) parts.push("channel");
  return parts.join(" ");
}

// The skill's folder name for a skill without a name: the last folder of its
// SKILL.md path (skills.go:79-82).
function folderName(path) {
  const parts = String(path ?? "").split(/[\\/]/).filter(Boolean);
  return parts.length >= 2 ? parts[parts.length - 2] : "";
}

// ws.skills (SkillInfo[], the engine's active skills, built-in ones among
// them) minus options.disabled_skills, one row per name (the first by
// SKILL.md path wins), sorted by name (skills.go:57-117). The list shows
// max-1 rows and "…and {len-(max-1)} more" (skills.go:124-133).
export function skillRows(skills, disabled = [], { max = Infinity } = {}) {
  const off = new Set(Array.isArray(disabled) ? disabled : []);
  const seen = new Set();
  const rows = [];
  const list = (Array.isArray(skills) ? skills : []).slice().sort((a, b) => goCompare(a?.id ?? "", b?.id ?? ""));
  for (const skill of list) {
    const name = skill?.name || folderName(skill?.id);
    if (!name || off.has(name) || seen.has(name)) continue;
    seen.add(name);
    rows.push({ name, status: "online", text: "" });
  }
  rows.sort((a, b) => goCompare(a.name, b.name));
  if (max <= 0) return { rows: [], more: 0 };
  if (rows.length > max) return { rows: rows.slice(0, max - 1), more: rows.length - (max - 1) };
  return { rows, more: 0 };
}
