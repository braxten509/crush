// Every sign-in's usage in the right panel (owner, 2026-10-21): a switch in
// Settings > Appearance, off by default, kept in this window's local storage.
// Only tools with two or more sign-ins are listed, one line per account with
// its 5-hour and weekly part left. While the switch is on and the window is
// connected, those tools are read again on a timer: Codex every minute,
// Claude every 10 minutes, since Anthropic's usage address refuses frequent
// reads and a refusal would also hide the main meters. The engine's refresh
// route reads every account of a tool (internal/backend/live_limits.go); the
// readings come back as limits events into limits.svelte.js.
import { api } from "../engine/api.js";
import { ACCOUNT_KINDS, CLAUDE, CODEX } from "../fallback/blocks.js";
import { fallback, loadFallback } from "../fallback/store.svelte.js";
import { meterLevel } from "../footer.js";
import { leftPercent, limitsKey, readTime } from "./limits.svelte.js";

const KEY = "crush-desktop.account-meters";

function readOn() {
  try {
    return globalThis.localStorage?.getItem(KEY) === "on";
  } catch {
    return false;
  }
}

export const accountMeters = $state({ on: readOn() });

export const READ_EVERY_MS = { [CODEX]: 60_000, [CLAUDE]: 10 * 60_000 };

const TOOL_NAMES = { [CLAUDE]: "Claude Code", [CODEX]: "Codex" };

export function setAccountMeters(on) {
  accountMeters.on = Boolean(on);
  try {
    globalThis.localStorage?.setItem(KEY, on ? "on" : "off");
  } catch {
    // Not kept past this run; the switch still works.
  }
  restart();
}

// The tools with two or more sign-ins, in ACCOUNT_KINDS order.
export function listedKinds(accounts) {
  return ACCOUNT_KINDS.filter((kind) => (accounts ?? []).filter((a) => a.kind === kind).length >= 2);
}

function windowOf(reading, name, now) {
  const window = (reading?.windows ?? []).find((w) => !w.model && w.name === name);
  if (!window) return null;
  const left = Math.round(leftPercent(window, now));
  return { left, level: meterLevel(left) };
}

// The panel's groups: [{kind, title, rows: [{id, name, inUse, state, five,
// week}]}]. state is "ready", "checking" (no reading yet), "idle" (the last
// read failed, so the numbers are old: an unused Claude login expires until
// Claude Code runs on it again) or "signed-out". five and week are {left,
// level} or null.
export function accountGroups({ accounts = [], byKind = {}, inUse = { kind: "", account: "" }, now = Date.now() } = {}) {
  return listedKinds(accounts).map((kind) => ({
    kind,
    title: TOOL_NAMES[kind] ?? kind,
    rows: accounts
      .filter((a) => a.kind === kind)
      .map((a) => {
        const reading = byKind[limitsKey(kind, a.id)] ?? null;
        const read = Boolean(readTime(reading?.read_at));
        let state = "ready";
        if (!a.signed_in) state = "signed-out";
        else if (!read) state = "checking";
        else if (reading.state === "error") state = "idle";
        return {
          id: a.id,
          name: a.name || a.label || TOOL_NAMES[kind],
          inUse: inUse.kind === kind && (inUse.account ?? "") === a.id,
          state,
          five: read ? windowOf(reading, "5h", now) : null,
          week: read ? windowOf(reading, "Weekly", now) : null,
        };
      }),
  }));
}

// -- The timer --

let connected = false;
let timers = [];

// connection.svelte.js says when the window joins or leaves its engine.
export function accountPollConnected(on) {
  connected = Boolean(on);
  restart();
}

function readKind(kind) {
  if (globalThis.document?.visibilityState === "hidden") return;
  if (!listedKinds(fallback.accounts).includes(kind)) return;
  api.refreshLimits(kind).catch((error) => console.error("account limits refresh failed", error));
}

function restart() {
  for (const timer of timers) clearInterval(timer);
  timers = [];
  if (!connected || !accountMeters.on) return;
  const first = fallback.loaded ? Promise.resolve() : loadFallback();
  first.then(() => {
    if (!connected || !accountMeters.on) return;
    for (const kind of ACCOUNT_KINDS) readKind(kind);
  });
  for (const kind of ACCOUNT_KINDS) timers.push(setInterval(() => readKind(kind), READ_EVERY_MS[kind]));
}
