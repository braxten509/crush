// EXT's registrations (desktop-design-5-6 2.8, 2.9; loaded by
// lib/plugins.js): the dialogs `skills`, `kept_skills`, `mcp_toggles` and
// `mcp_auth`; the actions `docker.enable` and `docker.disable` (enabled as
// the palette shows them: enable while Docker MCP is not configured and
// `docker mcp version` works, disable while it is configured); and the MCP
// sign-in sheet that opens by itself when a server needs it.
import KeptSkills from "../../components/extensions/KeptSkills.svelte";
import McpAuth from "../../components/extensions/McpAuth.svelte";
import McpToggles from "../../components/extensions/McpToggles.svelte";
import Skills from "../../components/extensions/Skills.svelte";
import { registerAction } from "../actions.js";
import { openDialog, registerDialog } from "../dialogs.js";
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";
import { listOf, pendingHere } from "../extensions/mcp.js";
import { checkDocker, dockerConfigured, ext } from "../extensions/store.svelte.js";
import { notify } from "../stores/ui.svelte.js";

registerDialog("skills", Skills);
registerDialog("kept_skills", KeptSkills);
registerDialog("mcp_toggles", McpToggles);
registerDialog("mcp_auth", McpAuth);

async function docker(enable) {
  try {
    await api.dockerMcp(enable);
    notify("info", enable ? "Docker MCP enabled and started successfully" : "Docker MCP disabled successfully");
  } catch (error) {
    notify("error", error?.message ?? String(error));
  }
}

registerAction({ id: "docker.enable", title: "Enable Docker MCP Catalog", enabled: () => !dockerConfigured() && ext.dockerAvailable, run: () => docker(true) });
registerAction({ id: "docker.disable", title: "Disable Docker MCP Catalog", enabled: () => dockerConfigured(), run: () => docker(false) });

// The palette asks for Docker's availability when it opens (commands.go:138).
onEvent("connection", (_kind, ev) => {
  if (ev?.state === "connected") checkDocker();
});

// A server that needs sign-in and is not disabled here (mcp_auth.go:45-96).
async function refreshAuthQueue() {
  try {
    const [pending, disabled, enabled] = await Promise.all([api.mcpPendingAuth(), api.mcpDisabled(), api.mcpEnabled()]);
    const queue = pendingHere(Array.isArray(pending) ? pending : [], listOf(disabled), listOf(enabled));
    const known = new Set(ext.authQueue.map((s) => s.name));
    ext.authQueue = [...ext.authQueue.filter((s) => queue.some((q) => q.name === s.name)), ...queue.filter((q) => !known.has(q.name))];
    if (ext.authQueue.length > 0) openDialog("mcp_auth");
  } catch {
    // Read again on the next event.
  }
}

onEvent("mcp_event", (_kind, event) => {
  const state = String(event?.state ?? "");
  if (state === "needs auth" || state === "needs_auth") refreshAuthQueue();
});
