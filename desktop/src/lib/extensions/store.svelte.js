// EXT's shared state (desktop-design-5-6 2.9): Docker MCP's availability for
// the palette's two entries (docker_mcp_version keeps its answer 10 s), and
// the MCP sign-in queue.
import { dockerMcpVersion } from "../engine/commands.js";
import { ws } from "../stores/workspace.svelte.js";

export const ext = $state({
  dockerAvailable: false,
  authQueue: [], // [{name, url}] waiting for sign-in
});

export const dockerConfigured = () => Boolean(ws.config?.mcp && Object.hasOwn(ws.config.mcp, "docker"));

let checking = false;
export async function checkDocker() {
  if (checking) return;
  checking = true;
  try {
    ext.dockerAvailable = (await dockerMcpVersion())?.ok === true;
  } catch {
    ext.dockerAvailable = false;
  } finally {
    checking = false;
  }
}
