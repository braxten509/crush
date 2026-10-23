// The model picker's data (desktop-design-5-6 2.3): the configured providers
// without secrets and the catalog, read when a model sheet opens and again on
// config_changed while one is open (the terminal app may change them), and
// the palette's labels that depend on the coder model.
import { onEvent } from "../engine/events.js";
import { sessions } from "../stores/sessions.svelte.js";
import { shownChatModel } from "./chatmodel.js";
import { modelInfo, readCatalog, readModelConfig } from "./providers.js";

export const modelData = $state({
  cfg: null, // readModelConfig()'s answer, or null before the first read
  catalog: [],
  error: null, // the last failed read's text
  revision: 0, // grows with each read that landed
});

let watchers = 0;
let seq = 0;

// Reads both; a failed read keeps what was there and sets error.
export async function refreshModels() {
  const mine = ++seq;
  try {
    const [cfg, catalog] = await Promise.all([readModelConfig(), readCatalog()]);
    if (mine !== seq) return modelData.cfg !== null;
    modelData.cfg = cfg;
    modelData.catalog = catalog;
    modelData.error = null;
    modelData.revision += 1;
    return true;
  } catch (error) {
    if (mine === seq) modelData.error = error?.message ?? String(error);
    return false;
  }
}

// An open sheet keeps the data fresh; → stop().
export function watchModels() {
  watchers += 1;
  refreshModels();
  let stopped = false;
  return () => {
    if (stopped) return;
    stopped = true;
    watchers -= 1;
  };
}

onEvent("config_changed", () => {
  if (watchers > 0 || modelData.cfg !== null) refreshModels();
});

// The coder's selected model and its catalog entry: the shown chat's own
// model when it has one (owner, 2026-10-23: a model picked in one chat must
// not reach another), else the workspace's.
export function coderModel(data = modelData, chat = null) {
  const cfg = data.cfg;
  if (!cfg) return { type: "large", selected: null, info: null };
  const type = cfg.coderType;
  const shown = chat ?? sessions.byId[sessions.currentId];
  const selected = shownChatModel(shown) ?? cfg.models?.[type] ?? null;
  return { type, selected, info: modelInfo(cfg, data.catalog, selected) };
}
