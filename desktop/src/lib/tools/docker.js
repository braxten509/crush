// Docker MCP tools (CHAT-074). Port of chat/docker_mcp.go:38-271; the server
// name is config.DockerMCPName, "docker".
import { compareGoStrings, decode, decodeMap, goMarshal, goTitle } from "./go.js";

export const DOCKER_PREFIX = "mcp_docker_";

export const isDockerTool = (name) => String(name ?? "").startsWith(DOCKER_PREFIX);

// The leading parameter of each Docker MCP tool, and whether the others follow.
const LEADS = {
  "mcp-find": { key: "query", extras: true },
  "mcp-add": { key: "name", extras: true },
  "mcp-remove": { key: "name", extras: true },
  "mcp-exec": { key: "name", extras: false },
  "mcp-config-set": { key: "server", extras: false },
};

// dockerMCPHeaderParams: [main, key, value, …]; other tools show their raw input.
export function dockerParams(tool, input) {
  const { value: params } = decodeMap(input ?? "");
  const spec = LEADS[tool];
  if (!spec) return [String(input ?? "")];
  const lead = params[spec.key];
  const out = [typeof lead === "string" ? lead : String(input ?? "")];
  if (!spec.extras) return out;
  const keys = Object.keys(params)
    .filter((k) => k !== spec.key)
    .sort(compareGoStrings);
  for (const k of keys) out.push(k, goMarshal(params[k]));
  return out;
}

// formatToolName: "Docker MCP → Find"; Add is drawn as a create, Remove as a destroy.
export function dockerAction(tool) {
  switch (tool) {
    case "mcp-exec":
      return { action: "Exec", tone: "" };
    case "mcp-config-set":
      return { action: "Config Set", tone: "" };
    case "mcp-find":
      return { action: "Find", tone: "" };
    case "mcp-add":
      return { action: "Add", tone: "create" };
    case "mcp-remove":
      return { action: "Remove", tone: "destroy" };
    case "code-mode":
      return { action: "Code Mode", tone: "" };
  }
  return { action: goTitle(tool.replaceAll("-", " ").replaceAll("_", " ")), tone: "" };
}

// makeCompactHeader: "Docker MCP: find" for nested rows.
export function dockerCompactName(tool) {
  const names = {
    "mcp-exec": "exec",
    "mcp-config-set": "config-set",
    "mcp-find": "find",
    "mcp-add": "add",
    "mcp-remove": "remove",
    "code-mode": "code-mode",
  };
  return "Docker MCP: " + (names[tool] ?? tool.replaceAll("-", " ").replaceAll("_", " "));
}

const FIND_RESPONSE = { servers: ["array", { name: "string", description: "string" }] };

// renderMCPServers: the servers mcp-find found, at most 10, else a note; a
// result that does not decode shows as plain text.
export function serversBody(content) {
  if (!content) return null;
  const { value, ok } = decode(content, FIND_RESPONSE);
  if (!ok) return { type: "plain", text: content };
  if (value.servers.length === 0) return { type: "empty", text: "No MCP servers found." };
  return {
    type: "servers",
    rows: value.servers.slice(0, 10),
    more: value.servers.length > 10 ? `... and ${value.servers.length - 10} more` : "",
  };
}
