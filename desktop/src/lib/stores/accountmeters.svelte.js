// Every sign-in's usage in the right panel (owner, 2026-10-21): a switch in
// Settings > Appearance, off by default, kept in this window's local storage.
// Every tool that reports usage is listed (owner, 2026-10-25): each Claude
// Code, Codex and Grok login, signed in or not, and Antigravity, OpenCode and
// Kimi, which have no sign-in list, once they have given a reading. While the
// switch is on and the window is connected, those tools are read again on a
// timer: Codex every minute, the others every 10 minutes, since Anthropic's
// usage address refuses frequent reads and a refusal would also hide the main
// meters. The engine's refresh route reads every account of a tool
// (internal/backend/live_limits.go); the readings come back as limits events
// into limits.svelte.js.
import { api } from "../engine/api.js";
import { ACCOUNT_KINDS, CLAUDE, CODEX } from "../fallback/blocks.js";
import { fallback, loadFallback } from "../fallback/store.svelte.js";
import { meterLevel } from "../footer.js";
import { leftPercent, limits, limitsKey, readTime } from "./limits.svelte.js";

const KEY = "crush-desktop.account-meters";

function readOn() {
  try {
    return globalThis.localStorage?.getItem(KEY) === "on";
  } catch {
    return false;
  }
}

export const accountMeters = $state({ on: readOn() });

// The tools with sign-ins (config.AccountKinds), then those without.
const GROK = "grok-cli";
const SIGN_IN_KINDS = [...ACCOUNT_KINDS, GROK];
const LOGINLESS_KINDS = ["agy-cli", "opencode-cli", "kimi-coding"];
const LIST_KINDS = [...SIGN_IN_KINDS, ...LOGINLESS_KINDS];

const TEN_MINUTES = 10 * 60_000;
export const READ_EVERY_MS = Object.fromEntries(LIST_KINDS.map((kind) => [kind, kind === CODEX ? 60_000 : TEN_MINUTES]));

// A tool with an account still checking is asked again this often: Codex
// within a minute, Claude at the terminal's 3-minute floor, since its usage
// address refuses frequent reads. A reading that arrives stops the retries.
// Any other tool waits its normal interval.
export const MISSING_EVERY_MS = { ...READ_EVERY_MS, [CODEX]: 60_000, [CLAUDE]: 3 * 60_000 };

const TOOL_NAMES = {
  [CLAUDE]: "Claude Code",
  [CODEX]: "Codex",
  [GROK]: "Grok",
  "agy-cli": "Antigravity",
  "opencode-cli": "OpenCode",
  "kimi-coding": "Kimi",
};

// A usage window's column heading, and the order the known ones go in.
const SPAN_WORDS = { "5h": "5h", Weekly: "Week", Monthly: "Month" };
const SPAN_ORDER = Object.keys(SPAN_WORDS);

export function setAccountMeters(on) {
  accountMeters.on = Boolean(on);
  try {
    globalThis.localStorage?.setItem(KEY, on ? "on" : "off");
  } catch {
    // Not kept past this run; the switch still works.
  }
  restart();
}

// The listed tools, in LIST_KINDS order: those with a sign-in, and those
// without sign-ins that have a reading (one never set up stays hidden).
export function listedKinds(accounts, byKind = {}) {
  return LIST_KINDS.filter((kind) =>
    SIGN_IN_KINDS.includes(kind) ? (accounts ?? []).some((a) => a.kind === kind) : Boolean(readTime(byKind[kind]?.read_at)),
  );
}

// The listed tools with a signed-in account that has no reading yet: their
// row says "checking…". A tool whose accounts all have a reading is left to
// its normal interval.
export function checkingKinds(accounts = [], byKind = {}) {
  return listedKinds(accounts, byKind).filter((kind) =>
    (accounts ?? []).some((a) => a.kind === kind && a.signed_in && !readTime(byKind[limitsKey(kind, a.id)]?.read_at)),
  );
}

// One span's part left: the window for the whole account, else the lowest
// of the per-model ones (Antigravity counts each model apart).
function partOf(reading, name, now) {
  const windows = (reading?.windows ?? []).filter((w) => w.name === name);
  const whole = windows.filter((w) => !w.model);
  const pick = whole.length > 0 ? whole : windows;
  if (pick.length === 0) return null;
  const left = Math.round(Math.min(...pick.map((w) => leftPercent(w, now))));
  return { left, level: meterLevel(left) };
}

// The window names a tool's readings carry: 5h, Weekly, Monthly first, then
// any other in the order read.
function spanNames(readings) {
  const names = [...new Set(readings.flatMap((r) => (r?.windows ?? []).map((w) => w.name)))];
  const known = SPAN_ORDER.filter((name) => names.includes(name));
  return [...known, ...names.filter((name) => !SPAN_ORDER.includes(name))];
}

// The panel's groups: [{kind, title, spans, rows: [{id, name, inUse, state,
// parts}]}]. spans are the column headings ("5h", "Week", "Month" or the
// tool's own name for a span); parts line up with them, each {left, level}
// or null. state is "ready", "checking" (no reading yet), "error" (the first
// read failed), "idle" (the last read failed, so the numbers are old: an
// unused Claude login expires until Claude Code runs on it again) or
// "signed-out".
export function accountGroups({ accounts = [], byKind = {}, inUse = { kind: "", account: "" }, now = Date.now() } = {}) {
  return listedKinds(accounts, byKind).map((kind) => {
    const logins = SIGN_IN_KINDS.includes(kind)
      ? accounts.filter((a) => a.kind === kind)
      : [{ id: "", kind, name: TOOL_NAMES[kind], signed_in: true }];
    const readings = logins.map((a) => {
      const reading = byKind[limitsKey(kind, a.id)] ?? null;
      return readTime(reading?.read_at) ? reading : null;
    });
    const names = spanNames(readings);
    return {
      kind,
      title: TOOL_NAMES[kind] ?? kind,
      spans: names.map((name) => SPAN_WORDS[name] ?? name),
      rows: logins.map((a, at) => {
        const reading = byKind[limitsKey(kind, a.id)] ?? null;
        const read = readings[at] !== null;
        let state = "ready";
        if (!a.signed_in) state = "signed-out";
        else if (!read) state = reading?.state === "error" ? "error" : "checking";
        else if (reading.state === "error") state = "idle";
        return {
          id: a.id,
          name: a.name || a.label || TOOL_NAMES[kind],
          inUse: inUse.kind === kind && (inUse.account ?? "") === a.id,
          state,
          parts: names.map((name) => (read ? partOf(reading, name, now) : null)),
        };
      }),
    };
  });
}

// -- The timer --

let connected = false;
let timers = [];
const askedAt = new Map(); // kind → when this window last asked (ms)

// connection.svelte.js says when the window joins or leaves its engine.
export function accountPollConnected(on) {
  connected = Boolean(on);
  restart();
}

function readKind(kind) {
  if (globalThis.document?.visibilityState === "hidden") return;
  // A tool without sign-ins is read even before it is listed: its first
  // reading is what lists it.
  if (SIGN_IN_KINDS.includes(kind) && !listedKinds(fallback.accounts).includes(kind)) return;
  askedAt.set(kind, Date.now());
  api.refreshLimits(kind).catch((error) => console.error("account limits refresh failed", error));
}

// Ask again for the tools with an account still checking, no more often than
// its safe interval. A reading that arrives removes the tool from this list.
function readMissing() {
  if (!connected || !accountMeters.on) return;
  if (globalThis.document?.visibilityState === "hidden") return;
  const now = Date.now();
  for (const kind of checkingKinds(fallback.accounts, limits.byKind)) {
    if (now - (askedAt.get(kind) ?? 0) < MISSING_EVERY_MS[kind]) continue;
    readKind(kind);
  }
}

function restart() {
  for (const timer of timers) clearInterval(timer);
  timers = [];
  if (!connected || !accountMeters.on) return;
  const first = fallback.loaded ? Promise.resolve() : loadFallback();
  first.then(() => {
    if (!connected || !accountMeters.on) return;
    for (const kind of LIST_KINDS) readKind(kind);
    readMissing();
  });
  for (const kind of LIST_KINDS) timers.push(setInterval(() => readKind(kind), READ_EVERY_MS[kind]));
  timers.push(setInterval(readMissing, 60_000));
}

// A window that was hidden while the first read was due takes one as soon as
// it is shown again, instead of waiting for the next timer.
globalThis.document?.addEventListener?.("visibilitychange", () => {
  if (globalThis.document?.visibilityState !== "hidden") readMissing();
});
