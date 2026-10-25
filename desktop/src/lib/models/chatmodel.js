// The model a chat runs on (owner, 2026-10-23: "when changing the model in
// this same folder, it also changed you here even though I was in a different
// chat"). The engine saves a model with each chat (session.Model, a
// config.SelectedModel), so the model UI reads the shown chat's own model
// whenever it has one, and with no chat open the last pick a new chat starts
// on. Pure, so the rule is unit-tested.

// shownChatModel is the model saved with a chat record, or null when it has
// none yet.
export function shownChatModel(chat) {
  const model = chat?.model;
  return model && model.model && model.provider ? model : null;
}

// The catwalk entry of a model (its name, window and what it can do).
function catwalkModel(providers, provider, model) {
  const found = (Array.isArray(providers) ? providers : []).find((p) => p?.id === provider);
  return (found?.models ?? []).find((m) => m?.id === model) ?? null;
}

// agentWithModel is the agent info as the shown chat sees it: the agent's
// state (ready, busy, main agent) with the chat's own model, else with no
// chat open the last pick (workspace). The agent info carries no model of its
// own (owner, 2026-10-24: no shared model anywhere); with neither, the model
// fields stay empty. infoOf(selected) may name a model the catalog lacks, as
// the CLI providers' models.
export function agentWithModel(agent, chat, providers = [], { workspace = null, infoOf = null } = {}) {
  const own = shownChatModel(chat) ?? shownChatModel({ model: workspace });
  if (!own) return agent ? { ...agent, model: null, model_cfg: null } : null;
  const known = infoOf?.(own) ?? catwalkModel(providers, own.provider, own.model);
  return {
    ...agent,
    // An unknown model (a provider this window has not read) shows its id,
    // never another model's name.
    model: known ? { ...known } : { id: own.model, name: own.model },
    model_cfg: { ...own },
  };
}
