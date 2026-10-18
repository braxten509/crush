// The engine connection (desktop-design 1.5.3). Scaffold S3: the pinned
// exports with empty bodies; piece P3.3 writes them.

export const conn = $state({
  state: "idle",
  attempt: 0,
  error: null,
  workspace: null, // OpenResult.workspace, which has no env and no config (1.2.5)
  engine: null,
  clientId: "",
  project: { cwd: "", path: "", dataDir: "", name: "" },
  notices: [],
});

// app_info → engine_open(cwd) → seeds the other stores. App.svelte calls it once.
export async function start() {}

export async function retry() {}
