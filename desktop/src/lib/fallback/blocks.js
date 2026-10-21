// The fallback order's blocks (config.FallbackBlock, internal/config/
// accounts.go): plain data the editor shows and changes. A use block runs
// chats on an account and model, a wait block waits for an account to
// refill, a refill block spends a Codex account's free refill. Account ""
// is each tool's own login (Claude 1, Codex 1).

export const CLAUDE = "claude-code";
export const CODEX = "codex-cli";
export const ACCOUNT_KINDS = [CLAUDE, CODEX];

// The providers a use block may pick: the configured ones (readModelConfig's
// Map), as the engine checks a block against cfg.Providers. The catalog has
// no agent CLIs, so it only adds names, types and models to a configured
// provider. Disabled ones and ones without models are left out.
// [{id, name, type, models}]
export function fallbackProviders(cfg, catalog = []) {
  const out = [];
  const list = Array.isArray(catalog) ? catalog : [];
  for (const [id, p] of cfg?.providers ?? []) {
    if (p?.disable) continue;
    const fromCatalog = list.find((c) => c?.id === id);
    const models = [];
    for (const m of [...(p?.models ?? []), ...(p?.chatgpt_models ?? []), ...(p?.grok_models ?? []), ...(fromCatalog?.models ?? [])]) {
      if (m?.id && !models.some((x) => x.id === m.id)) models.push(m);
    }
    if (models.length) out.push({ id, name: p?.name || fromCatalog?.name || id, type: p?.type || fromCatalog?.type || "", models });
  }
  return out;
}

// The tool behind a provider id (its type), or "".
export function providerKind(providers, id) {
  return (providers ?? []).find((p) => p?.id === id)?.type ?? "";
}

export function hasAccounts(kind) {
  return ACCOUNT_KINDS.includes(kind);
}

// The tool a block's account belongs to.
export function blockKind(block, providers) {
  return block?.type === "use" ? providerKind(providers, block.provider) : (block?.kind ?? "");
}

// An account where both tools' accounts are named together: its label (the
// engine adds the tool when another account has the same name, owner
// 2026-10-21), else its name.
export function accountName(accounts, kind, id) {
  const found = (accounts ?? []).find((a) => a.kind === kind && a.id === (id ?? ""));
  if (found) return found.label || found.name;
  return `${kind === CODEX ? "Codex" : kind === CLAUDE ? "Claude" : kind} ${id ? `(${id})` : "1"}`;
}

export function modelName(providers, provider, model) {
  const p = (providers ?? []).find((x) => x?.id === provider);
  return p?.models?.find((m) => m?.id === model)?.name || model || "no model";
}

// "1 h", "90 min", "2 h 30 min".
export function minutesText(minutes) {
  const n = Math.max(0, Math.round(Number(minutes) || 0));
  const h = Math.floor(n / 60);
  const m = n % 60;
  if (!h) return `${m} min`;
  return m ? `${h} h ${m} min` : `${h} h`;
}

// The effort levels of a use block's model, or [].
export function effortsOf(block, providers) {
  const p = (providers ?? []).find((x) => x?.id === block?.provider);
  return p?.models?.find((m) => m?.id === block?.model)?.reasoning_levels ?? [];
}

// The accounts a block's account word may pick: a use block's tool's own, a
// refill block's Codex ones, a wait block's every one.
export function accountChoices(block, { accounts = [], providers = [] } = {}) {
  if (block?.type === "use") return accountsOf(accounts, blockKind(block, providers));
  if (block?.type === "refill") return accountsOf(accounts, CODEX);
  return (accounts ?? []).filter((a) => hasAccounts(a.kind));
}

// How the chat leaves a use block: the end of its sentence.
export function leaveWords(block) {
  let text = Number(block?.move_at_percent) > 0 ? `at ${Number(block.move_at_percent)}% used` : "when it runs out";
  if (block?.refill_first) text += ", after a free refill";
  if (block?.come_back) text += ", and come back when it refills";
  return text;
}

// The block as the sentence the page shows (owner, 2026-10-21, look C):
// plain text ({text}) and words that open a menu ({text, menu}), menu naming
// what the word changes: "model", "account", "effort", "leave" (how the
// chat leaves a use block), "wait" (how long a wait block waits) or
// "meanwhile". An account word with nothing else to pick stays plain text.
export function sentence(block, ctx = {}) {
  const { accounts = [], providers = [] } = ctx;
  const kind = blockKind(block, providers);
  const account = { text: accountName(accounts, kind, block?.account) };
  if (accountChoices(block, ctx).length > 1) account.menu = "account";
  switch (block?.type) {
    case "use": {
      const out = [{ text: "Use " }, { text: modelName(providers, block.provider, block.model), menu: "model" }];
      if (hasAccounts(kind)) out.push({ text: " on " }, account);
      if (effortsOf(block, providers).length) out.push({ text: " at " }, { text: block.effort || "default", menu: "effort" }, { text: " effort" });
      out.push({ text: ". Move on " }, { text: leaveWords(block), menu: "leave" }, { text: "." });
      return out;
    }
    case "wait": {
      const most = Number(block.max_wait_minutes) > 0 ? `at most ${minutesText(block.max_wait_minutes)}` : "with no time limit";
      const meanwhile = block.meanwhile === "next" ? "using the next blocks meanwhile" : "just waiting";
      return [{ text: "Wait for " }, account, { text: " to refill, " }, { text: most, menu: "wait" }, { text: ", " }, { text: meanwhile, menu: "meanwhile" }, { text: "." }];
    }
    case "refill":
      return [{ text: "Use a free refill on " }, account, { text: "." }];
  }
  return [{ text: "Unknown block" }];
}

// The sentence as one string.
export function sentenceText(block, ctx) {
  return sentence(block, ctx)
    .map((piece) => piece.text)
    .join("");
}

// A new block of type, filled in from what exists.
export function newBlock(type, { providers = [] } = {}) {
  if (type === "use") {
    const p = (providers ?? []).find((x) => hasAccounts(x?.type)) ?? (providers ?? [])[0];
    return { type: "use", provider: p?.id ?? "", model: p?.models?.[0]?.id ?? "", account: "" };
  }
  if (type === "wait") return { type: "wait", kind: CLAUDE, account: "" };
  return { type: "refill", kind: CODEX, account: "" };
}

// The blocks with the one at `from` moved to `to`.
export function moveBlock(blocks, from, to) {
  const list = [...blocks];
  if (from < 0 || from >= list.length) return list;
  const [block] = list.splice(from, 1);
  list.splice(Math.max(0, Math.min(to, list.length)), 0, block);
  return list;
}

// The block with fields changed; empty values are left out, as the engine
// writes them (omitempty), so a saved order reads back the same.
export function changeBlock(block, fields) {
  const next = { ...block, ...fields };
  for (const [key, value] of Object.entries(next)) {
    if (key !== "type" && (value === "" || value === 0 || value === false || value === null || value === undefined)) delete next[key];
  }
  if (next.type === "use") {
    delete next.kind;
    delete next.max_wait_minutes;
    delete next.meanwhile;
  } else {
    for (const key of ["provider", "model", "effort", "move_at_percent", "refill_first", "come_back"]) delete next[key];
  }
  return next;
}

// What a block may pick: the accounts of its tool.
export function accountsOf(accounts, kind) {
  return (accounts ?? []).filter((a) => a.kind === kind);
}
