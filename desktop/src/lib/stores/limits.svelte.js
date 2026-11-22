// The AI tools' usage limits (desktop-design 1.5.3, SIDE-007; engine design
// 5.9(e)). Limits belong to the account, not the project: one reading per
// kind, the one with the newest read_at from any engine. A kind without a
// reading is "checking…", never 0.
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";
import { CODEX } from "../fallback/blocks.js";

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
let wanted = null; // the selection to refresh once the snapshot arrives

// GET /v1/limits ({clis: CLILimits[]}): at start and on every resync.
export async function loadLimits() {
  const snapshot = await api.limits();
  for (const reading of snapshot?.clis ?? []) applyLimits(reading);
  loaded = true;
  if (wanted) {
    const { kind, account } = wanted;
    wanted = null;
    refreshLimitsFor(kind, { account });
  }
}

onEvent("limits", (_kind, reading) => applyLimits(reading));
onResync(loadLimits);

let lastProvider = "";
let lastAccount;

// The agent's provider, after every agent read: a change asks the engine for
// a fresh reading of that kind, as the terminal does when the selected
// provider changes (internal/ui/model/workspace_cache.go:200-203; first read
// included). A built-in agent CLI's provider id is its kind (cli_providers.go).
// Codex follows the selected sign-in even when the provider stays the same.
// Only kinds the snapshot lists report limits; the answer comes as an event.
export function agentProviderChanged(provider, account = "") {
  const selectedAccount = provider === CODEX ? account : undefined;
  if (provider === lastProvider && selectedAccount === lastAccount) return;
  lastProvider = provider;
  lastAccount = selectedAccount;
  if (!provider) {
    wanted = null;
    return;
  }
  refreshLimitsFor(provider, { account: selectedAccount });
}

const askedAt = new Map(); // limitsKey → when this window last asked for a refresh (ms)

function refreshLimitsFor(kind, { due = false, account } = {}) {
  if (!loaded) {
    wanted = { kind, account };
    return;
  }
  if (!limits.byKind[kind]) return;
  askedAt.set(limitsKey(kind, account), Date.now());
  api.refreshLimits(kind, { due, account }).catch((error) => console.error("limits refresh failed", error));
}

// The terminal's poll (internal/ui/model/polling.go:15, update_ui.go:268-280):
// every 60 s while the window is visible and the shown chat is idle, the
// current provider's kind is refreshed once its reading is 3 min old
// (cliagent/limits.go:40,207). The poll asks with due, so the engine keeps
// that throttle too (internal/server/handlers_live.go); the page also keeps
// it, for a forced ask from a model change: a kind is also
// asked again only 3 min after this window last asked, as cliagent backs off a
// failed fetch like a success (limits.go:224-228), since a failure keeps the
// old read_at. A kind without read_at counts as stale, as the terminal's
// throttle is on the last fetch, not on a reading. connection.svelte.js runs
// the poll while connected; idle() tells whether the shown chat is idle.
const POLL_MS = 60_000;
const STALE_MS = 3 * 60_000;
const RETURN_REFRESH_GAP_MS = 5_000;
let poll = null;

function refreshCodexOnReturn() {
  if (lastProvider !== CODEX || globalThis.document?.visibilityState === "hidden") return;
  const asked = askedAt.get(limitsKey(CODEX, lastAccount));
  // Restoring a window can fire both visibilitychange and focus.
  if (asked !== undefined && Date.now() - asked < RETURN_REFRESH_GAP_MS) return;
  refreshLimitsFor(CODEX, { account: lastAccount });
}

export function startLimitsPoll({ idle = () => true } = {}) {
  stopLimitsPoll();
  globalThis.window?.addEventListener("focus", refreshCodexOnReturn);
  globalThis.document?.addEventListener("visibilitychange", refreshCodexOnReturn);
  poll = setInterval(() => {
    if (globalThis.document?.visibilityState === "hidden" || !idle()) return;
    const kind = lastProvider;
    if (!kind) return;
    const account = lastAccount;
    const key = limitsKey(kind, account);
    const read = readTime(limits.byKind[key]?.read_at);
    const now = Date.now();
    if (read && now - read[0] < STALE_MS) return;
    if (askedAt.has(key) && now - askedAt.get(key) < STALE_MS) return;
    // A timer, so only what is due: the engine first takes any newer
    // reading another engine saved, and asks the tool only after 3 min.
    refreshLimitsFor(kind, { due: true, account });
  }, POLL_MS);
}

export function stopLimitsPoll() {
  clearInterval(poll);
  poll = null;
  globalThis.window?.removeEventListener("focus", refreshCodexOnReturn);
  globalThis.document?.removeEventListener("visibilitychange", refreshCodexOnReturn);
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
