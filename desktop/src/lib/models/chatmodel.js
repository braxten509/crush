// The model a chat runs on (owner, 2026-10-23: "when changing the model in
// this same folder, it also changed you here even though I was in a different
// chat"). The engine saves a model with each chat (session.Model, a
// config.SelectedModel), so the model UI reads the shown chat's own model
// whenever it has one, and the workspace's default otherwise. Pure, so the
// rule is unit-tested.

// shownChatModel is the model saved with a chat record, or null when the chat
// follows the workspace's model.
export function shownChatModel(chat) {
  const model = chat?.model;
  return model && model.model && model.provider ? model : null;
}

// The catwalk entry of a model (its name, window and what it can do).
function catwalkModel(providers, provider, model) {
  const found = (Array.isArray(providers) ? providers : []).find((p) => p?.id === provider);
  return (found?.models ?? []).find((m) => m?.id === model) ?? null;
}

// agentWithModel is the agent info as the shown chat sees it: its own model
// when it has one, else the workspace's saved model (workspace, the config's
// coder model), else the agent's. The saved model beats the agent's because
// the engine keeps its old model while another chat runs (UpdateModels skips
// a busy agent), and the menu already shows the saved one (owner,
// 2026-10-24). infoOf(selected) may name a model the catalog lacks, as the
// CLI providers' models.
export function agentWithModel(agent, chat, providers = [], { workspace = null, infoOf = null } = {}) {
  const own = shownChatModel(chat) ?? shownChatModel({ model: workspace });
  if (!own || !agent) return agent;
  const cfg = agent.model_cfg ?? {};
  const same = cfg.provider === own.provider && cfg.model === own.model;
  const known = infoOf?.(own) ?? catwalkModel(providers, own.provider, own.model);
  return {
    ...agent,
    // An unknown model (a provider this window has not read) shows its id,
    // never another model's name.
    model: known ? { ...agent.model, ...known } : same ? agent.model : { ...agent.model, id: own.model, name: own.model },
    model_cfg: { ...own },
  };
}
