// The main agent's capabilities (desktop-design-5-6 2.3, 7.4; GET
// agent/capabilities). Placeholder from scaffold S5 with the pinned exports;
// MOD owns it (7.3).

// An AgentCapabilities (lib/engine/wire.js).
export const capabilities = $state({ kind: "", can_background: false });

export async function refreshCapabilities() {}
