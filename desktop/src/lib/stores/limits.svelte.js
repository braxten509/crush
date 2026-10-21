// The AI tools' usage limits (desktop-design 1.5.3, SIDE-007; engine design
// 5.9(e)). Limits belong to the account, not the project: one reading per
// kind, the one with the newest read_at from any engine. A kind without a
// reading is "checking…", never 0.
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";

export const limits = $state({ byKind: {} }); // limitsKey → CLILimits with the newest read_at (engine design 5.9)

// The key of one account's reading: the tool's own login is its kind, any
// other account "kind#id" (options.accounts, the fallback order).
export function limitsKey(kind, account = "") {
  return account ? `${kind}#${account}` : kind;
}

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
  if (!reading?.kind) return;
  const key = limitsKey(reading.kind, reading.account);
  const at = readTime(reading.read_at);
  const storedAt = readTime(limits.byKind[key]?.read_at);
  if (storedAt && (!at || compareTimes(at, storedAt) < 0)) return;
  limits.byKind[key] = reading;
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

const askedAt = new Map(); // kind → when this window last asked for a refresh (ms)

function refreshLimitsFor(kind) {
  if (!loaded) {
    wanted = kind;
    return;
  }
  if (!limits.byKind[kind]) return;
  askedAt.set(kind, Date.now());
  api.refreshLimits(kind).catch((error) => console.error("limits refresh failed", error));
}

// The terminal's poll (internal/ui/model/polling.go:15, update_ui.go:268-280):
// every 60 s while the window is visible and the shown chat is idle, the
// current provider's kind is refreshed once its reading is 3 min old
// (cliagent/limits.go:40,207). The engine's refresh route skips that throttle
// (internal/server/handlers_live.go:149), so the page keeps it: a kind is also
// asked again only 3 min after this window last asked, as cliagent backs off a
// failed fetch like a success (limits.go:224-228), since a failure keeps the
// old read_at. A kind without read_at counts as stale, as the terminal's
// throttle is on the last fetch, not on a reading. connection.svelte.js runs
// the poll while connected; idle() tells whether the shown chat is idle.
const POLL_MS = 60_000;
const STALE_MS = 3 * 60_000;
let poll = null;

export function startLimitsPoll({ idle = () => true } = {}) {
  stopLimitsPoll();
  poll = setInterval(() => {
    if (globalThis.document?.visibilityState === "hidden" || !idle()) return;
    const kind = lastProvider;
    const read = readTime(limits.byKind[kind]?.read_at);
    const now = Date.now();
    if (read && now - read[0] < STALE_MS) return;
    if (askedAt.has(kind) && now - askedAt.get(kind) < STALE_MS) return;
    refreshLimitsFor(kind);
  }, POLL_MS);
}

export function stopLimitsPoll() {
  clearInterval(poll);
  poll = null;
}

// The windows that apply to a model, as cliagent.Limits does
// (internal/agent/cliagent/limits.go:145-157): those for every model and
// those for this one. null until the kind has a reading.
export function limitsFor(kind, model, account = "") {
  const reading = limits.byKind[limitsKey(kind, account)];
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
