// The AI tools' usage limits (desktop-design 1.5.3, SIDE-007; engine design
// 5.9(e)). Limits belong to the account, not the project: one reading per
// kind, the one with the newest read_at from any engine. A kind without a
// reading is "checking…", never 0.
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";

export const limits = $state({ byKind: {} }); // kind → CLILimits with the newest read_at (engine design 5.9)

// read_at is Go's RFC 3339 with up to nine fraction digits, which Date.parse
// does not keep: [ms since 1970, ns within that ms], or null for no time.
const RFC3339 = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/iu;
export function readTime(value) {
  const m = typeof value === "string" ? RFC3339.exec(value.trim()) : null;
  if (!m || m[1].startsWith("0001-01-01")) return null; // Go's zero time
  const ms = Date.parse(m[1] + (m[3].toUpperCase() === "Z" ? "Z" : m[3]));
  if (!Number.isFinite(ms)) return null;
  const ns = Number((m[2] ?? "").padEnd(9, "0"));
  return [ms + Math.floor(ns / 1e6), ns % 1e6];
}

function compareTimes(a, b) {
  return a[0] - b[0] || a[1] - b[1];
}

// One reading, from the start snapshot, a resync or a `limits` event. A
// reading as new as the stored one replaces it (a failed refresh after a
// reading keeps the reading's read_at and adds its error); one without
// read_at ("checking" or "error") never hides a reading.
export function applyLimits(reading) {
  const kind = reading?.kind;
  if (!kind) return;
  const at = readTime(reading.read_at);
  const storedAt = readTime(limits.byKind[kind]?.read_at);
  if (storedAt && (!at || compareTimes(at, storedAt) < 0)) return;
  limits.byKind[kind] = reading;
}

let loaded = false;
let wanted = ""; // a kind to refresh once the snapshot shows it reports limits

// GET /v1/limits ({clis: CLILimits[]}): at start and on every resync.
export async function loadLimits() {
  const snapshot = await api.limits();
  for (const reading of snapshot?.clis ?? []) applyLimits(reading);
  loaded = true;
  if (wanted) {
    const kind = wanted;
    wanted = "";
    refreshLimitsFor(kind);
  }
}

onEvent("limits", (_kind, reading) => applyLimits(reading));
onResync(loadLimits);

let lastProvider = "";

// The agent's provider, after every agent read: a change asks the engine for
// a fresh reading of that kind, as the terminal does when the selected
// provider changes (internal/ui/model/workspace_cache.go:200-203; first read
// included). A built-in agent CLI's provider id is its kind (cli_providers.go).
// Only kinds the snapshot lists report limits; the answer comes as an event.
export function agentProviderChanged(provider) {
  if (!provider || provider === lastProvider) return;
  lastProvider = provider;
  refreshLimitsFor(provider);
}

function refreshLimitsFor(kind) {
  if (!loaded) {
    wanted = kind;
    return;
  }
  if (!limits.byKind[kind]) return;
  api.refreshLimits(kind).catch((error) => console.error("limits refresh failed", error));
}

// The windows that apply to a model, as cliagent.Limits does
// (internal/agent/cliagent/limits.go:145-157): those for every model and
// those for this one. null until the kind has a reading.
export function limitsFor(kind, model) {
  const reading = limits.byKind[kind];
  if (!reading || !readTime(reading.read_at)) return null;
  const windows = (reading.windows ?? []).filter((w) => !w.model || w.model === model);
  return { kind, state: reading.state, readAt: reading.read_at, error: reading.error ?? "", windows };
}

// Percent of a window still available at `now` (ms), as Limit.Left
// (limits.go:30-36): a window past its reset is whole again.
export function leftPercent(window, now = Date.now()) {
  const resets = readTime(window?.resets_at);
  if (resets && now > resets[0]) return 100;
  return Math.max(0, 100 - (Number(window?.used) || 0));
}
