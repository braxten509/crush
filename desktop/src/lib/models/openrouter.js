// OpenRouter's "+" in the model menu (owner, 2026-10-22, pick B): its
// catalog is large, so only the models saved under the provider show in the
// menu; "+ Add model" under them opens a sheet that searches the account's
// catalog (GET …/openrouter/catalog, the account's preferences applied).

// The configured OpenRouter providers that take saved models: [{id, name}].
export function addableProviders(cfg) {
  const out = [];
  for (const [id, p] of cfg?.providers ?? []) if (p?.type === "openrouter" && !p.disable) out.push({ id, name: p.name || id });
  return out;
}

const squash = (text) => String(text ?? "").replace(/\s+/g, "").toLowerCase();

// The catalog models whose name or id hold the words, spaces and case ignored.
export function catalogRows(catalog, query) {
  const words = squash(query);
  if (!words) return catalog;
  return catalog.filter((m) => squash(`${m.name}${m.id}`).includes(words));
}

const dollars = (n) => `$${Number(n.toFixed(2))}`;

// "$0.6 / $2.5" per million tokens in and out, or "free".
export function priceText(model) {
  const into = Number(model?.cost_per_1m_in ?? 0);
  const out = Number(model?.cost_per_1m_out ?? 0);
  if (into <= 0 && out <= 0) return "free";
  return `${dollars(into)} / ${dollars(out)}`;
}

// "262K", "1M": the context size in thousands or millions of tokens.
export function contextText(tokens) {
  const n = Number(tokens ?? 0);
  if (n <= 0) return "";
  if (n >= 1_000_000) return `${Number((n / 1_000_000).toFixed(1))}M`;
  return `${Math.round(n / 1000)}K`;
}

// The saved list with the picked models added after it, each once.
export function savedWith(saved, picked) {
  const out = [...(saved ?? [])];
  const have = new Set(out.map((m) => m.id));
  for (const m of picked) {
    if (have.has(m.id)) continue;
    have.add(m.id);
    out.push(m);
  }
  return out;
}

export const savedWithout = (saved, id) => (saved ?? []).filter((m) => m.id !== id);
