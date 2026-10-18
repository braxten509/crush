// The project's config, agent, providers, LSPs, MCPs, skills, modified files
// and Git branch (desktop-design 1.5.3). Scaffold S3: the pinned exports with
// empty bodies; piece P3.3 writes them.

export const ws = $state({
  config: null, // pickConfig(GET config)
  agent: null,
  providers: [],
  lsps: {},
  mcps: {},
  skills: [],
  files: {},
  branch: "",
});

// Keeps models, recent_models, options, permissions, and for each mcp and lsp
// entry only {type, disabled}; drops providers, env, hooks, tools and every
// other field (internal/config/config.go:85-111), so no key or secret header
// sits in a store, the probe or a recording.
export function pickConfig(config) {}
