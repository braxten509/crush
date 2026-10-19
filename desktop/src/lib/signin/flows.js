// The sign-in flows (desktop-design-5-6 2.4): which sheet a provider needs
// (internal/ui/model/model_select.go:299-353, openAuthenticationDialog),
// the four platforms G-5's flow routes know, and the names the sheets show.
// Every credential goes through apiSecret (6.4) and never into a store.
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";
import { modelData, refreshModels } from "../models/store.svelte.js";

// providerId → the platform of POST …/auth/flows and the sheet's name.
export const PLATFORMS = {
  hyper: { platform: "hyper", name: "Hyper", logout: "Charm Hyper" },
  copilot: { platform: "copilot", name: "GitHub Copilot", logout: "GitHub Copilot" },
  openai: { platform: "openai", name: "ChatGPT", logout: "ChatGPT" },
  xai: { platform: "grok", name: "Grok", logout: "Grok" },
};

export const providerOfPlatform = (platform) => Object.keys(PLATFORMS).find((id) => PLATFORMS[id].platform === platform) ?? "";

// The configured provider, read fresh enough to decide.
export async function configuredProvider(providerId) {
  if (!modelData.cfg) await refreshModels();
  return modelData.cfg?.providers.get(providerId) ?? null;
}

// openAuthenticationDialog: "oauth", "auth_method" (the method cards) or
// "api_key_input". `model` is the chosen SelectedModel, empty for a plain
// sign-in (Settings → Accounts).
export function pickSheet(providerId, configured, model) {
  if (providerId === "hyper" || providerId === "copilot") return "oauth";
  if (providerId === "openai" || providerId === "xai") {
    if (!model?.model || configured?.has_oauth) return "oauth";
    if (!configured?.has_api_key) return "auth_method";
    return "api_key_input";
  }
  return "api_key_input";
}

// -- Flow events (G-5: auth_flow {flow_id, state, error}) --

const listeners = new Map(); // flow_id → fn(event)
const early = new Map(); // flow_id → the newest event that came before its sheet listened

onEvent("auth_flow", (_kind, event) => {
  const id = event?.flow_id;
  if (!id) return;
  const fn = listeners.get(id);
  if (fn) fn(event);
  else early.set(id, event);
});

// Starts a flow → {flow_id, url, user_code, code_entry}; onState gets its events.
export async function startFlow(platform, onState) {
  const flow = await api.startAuthFlow({ platform });
  listeners.set(flow.flow_id, onState);
  const before = early.get(flow.flow_id);
  if (before) {
    early.delete(flow.flow_id);
    onState(before);
  }
  return flow;
}

export function stopListening(flowId) {
  listeners.delete(flowId);
  early.delete(flowId);
}

// The verify call's spinner shows at least this long (api_key_input.go:303-305).
export const MIN_VERIFY_MS = 750;
