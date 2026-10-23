// The shown chat's helpers (desktop-design-5-6 2.15; LIVE-001, LIVE-008,
// DLG-224, DLG-226, DLG-229): GET …/sessions/{sid}/helpers when the chat
// opens, on reconnect and every second while one of them runs (the terminal's
// cadence, internal/ui/model/tasks.go:146-152), kept current between reads by
// `helper` events. A helper's own helpers belong to its chat, so only the
// chat's direct helpers count (tasks.go:56-69). Also the words both sheets,
// the tray and the helper view share, a helper's model as the model box and
// the helper view's footer read it, the stop call, opening the two sheets,
// the tray entries' hint, a one-second clock for the elapsed times, and the
// probe's hooks for the open sheet and view.
import { untrack } from "svelte";
import { createSubscriber, SvelteMap } from "svelte/reactivity";
import { closeDialog, openDialog } from "../dialogs.js";
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";
import { refreshCapabilities } from "../models/capabilities.svelte.js";
import { focusField } from "../stores/composer.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { notify, ui } from "../stores/ui.svelte.js";
import { traySections, traySelected } from "../tray.js";

const READ_EVERY_MS = 1000;
// A row stopped here leaves at once (internal/ui/dialog/background.go:163-171)
// and stays out until the engine no longer lists it as running, or this long
// after the engine answered the stop (then a row still listed comes back).
const HOLD_AFTER_ANSWER_MS = 5000;

class HelperList {
  sid = $state(""); // the chat these records belong to ("" on the start screen)
  byId = new SvelteMap(); // helper id → proto.Helper, as the engine last sent it
}

export const helpers = new HelperList();

// The open sheet and helper view report themselves here for the probe
// (plugins/live.js): each sets a function while it is mounted.
export const probes = { sheet: null, view: null };

// -- Time --

// An RFC 3339 time from the engine, in ms. Go writes up to nine digits of a
// second; Date.parse is only sure of three.
export function timeOf(value) {
  if (!value) return NaN;
  return Date.parse(String(value).replace(/(\.\d{3})\d+/u, "$1"));
}

const pad = (n) => String(n).padStart(2, "0");

// since() (background.go:275-281): "<m>m<ss>s" under an hour, "<h>h<mm>m"
// from an hour on, the duration rounded to the second.
export function since(started, now = Date.now()) {
  const at = timeOf(started);
  const s = Number.isFinite(at) ? Math.max(0, Math.round((now - at) / 1000)) : 0;
  if (s >= 3600) return `${Math.floor(s / 3600)}h${pad(Math.floor(s / 60) % 60)}m`;
  return `${Math.floor(s / 60)}m${pad(s % 60)}s`;
}

// Wakes its readers at each wall-clock second while any read: the elapsed
// times are redrawn every second (2.15). Unlike the marks' ticker, it keeps
// going under reduced motion, as it moves no picture.
const everySecond = createSubscriber((update) => {
  let timer = null;
  const next = () => {
    timer = setTimeout(() => {
      update();
      next();
    }, 1000 - (Date.now() % 1000));
  };
  next();
  return () => clearTimeout(timer);
});

export function secondNow() {
  everySecond();
  return Date.now();
}

// -- Words --

// The Sub-agents row's info (background.go:258-267): "<cli>/<model>[/<effort>]
// [ · read-only] · <elapsed>". The fast flag is not shown, as in the terminal.
export function helperInfo(h, now = Date.now()) {
  let model = h.model ?? "";
  if (h.effort) model += `/${h.effort}`;
  if (h.read_only) model += " · read-only";
  return `${h.cli ?? ""}/${model} · ${since(h.started, now)}`;
}

// The model a helper runs, as the agent block the composer's model box and the
// helper view's footer read (owner, 2026-10-22): the model's own name from the
// provider its CLI names, with the helper's effort and fast tier, and that
// provider's type for its usage windows.
export function helperAgent(h, providers = []) {
  const list = Array.isArray(providers) ? providers : [];
  const cfg = { model: h?.model ?? "", reasoning_effort: h?.effort ?? "", provider: "" };
  if (h?.fast) cfg.service_tier = "fast";
  let model = { name: h?.model ?? "" };
  let kind = "";
  for (const p of list) {
    const cliName = String(p?.type ?? "")
      .replace(/-cli$/u, "")
      .replace(/-code$/u, "");
    if (h?.cli !== p?.id && h?.cli !== cliName) continue;
    model = (p.models ?? []).find((m) => m?.id === h.model) ?? model;
    cfg.provider = p.id;
    kind = p.type ?? "";
    break;
  }
  return { agent: { model, model_cfg: cfg }, kind };
}

// The tray entry's label (tasks.go:245-248).
export function helpersLabel(n) {
  return n === 1 ? "1 subagent" : `${n} subagents`;
}

// -- The two sheets and the tray entries --

// The sheet ids are the terminal's dialog ids (internal/ui/dialog/background.go:24-26).
export const SHEET = { helpers: "sub-agents", jobs: "background" };

// The two sheets replace each other, and opening one from the tray gives the
// tray's focus back to the field, where the sheet returns it when it closes
// (tasks.go:299-324).
export function openSheet(id) {
  if (ui.focus === "tray") focusField();
  for (const other of Object.values(SHEET)) if (other !== id) closeDialog(other);
  openDialog(id);
}

const ENTRIES = new Set(Object.keys(SHEET));
export const TRAY_HINT = { idle: "↓ manage", focused: "enter list · ←/→ select · ↑ back" };

// The entry the tray's keys act on, while the tray has them.
export function trayEntryFocused(id) {
  return ui.focus === "tray" && traySelected() === id;
}

// The hint after the entries (tasks.go:277-287), drawn at the end of the last
// of the two shown: "↓ manage", or the tray keys while one of them is focused.
export function trayHint(id) {
  const shown = traySections.filter((section) => ENTRIES.has(section.id));
  if (shown.length === 0 || shown[shown.length - 1].id !== id) return "";
  if (ui.focus !== "tray") return TRAY_HINT.idle;
  return ENTRIES.has(traySelected()) ? TRAY_HINT.focused : "";
}

// -- The list --

const held = new SvelteMap(); // helper id → ms the engine answered its stop (0 while on its way)
const eventAt = new Map(); // helper id → ms of its last helper event (a read older than it keeps it)
let readTurn = 0;
let reading = false;
let timer = null;

// The chat's running helpers, oldest first (tasks.go:56-69), without the ones
// stopped here and not yet gone.
export function runningHelpers() {
  const sid = helpers.sid;
  if (!sid || sid !== sessions.currentId) return [];
  const out = [];
  for (const h of helpers.byId.values()) {
    if (h.session_id === sid && h.status === "running" && !held.has(h.id)) out.push(h);
  }
  return out.sort((a, b) => (timeOf(a.started) || 0) - (timeOf(b.started) || 0));
}

export function helperById(id) {
  return helpers.byId.get(id) ?? null;
}

function anyRunning() {
  for (const h of helpers.byId.values()) if (h.status === "running") return true;
  return false;
}

function schedule() {
  clearTimeout(timer);
  timer = null;
  if (helpers.sid && anyRunning()) timer = setTimeout(read, READ_EVERY_MS);
}

async function read() {
  clearTimeout(timer);
  timer = null;
  const sid = helpers.sid;
  if (!sid) return;
  const turn = ++readTurn;
  const startedAt = Date.now();
  reading = true;
  let list = null;
  try {
    list = await api.helpers(sid);
  } catch {
    list = null; // a failed read keeps what is shown; the next one tries again
  }
  if (turn !== readTurn) return; // a newer read or another chat took over
  reading = false;
  if (Array.isArray(list)) replaceAll(list, startedAt);
  schedule();
}

// A read replaces the records, except those a helper event changed after the
// read started: the event is newer than what the engine read.
function replaceAll(list, startedAt) {
  const fresh = new Map();
  for (const h of list) if (h?.id) fresh.set(h.id, h);
  for (const id of [...helpers.byId.keys()]) {
    if (!fresh.has(id) && !((eventAt.get(id) ?? 0) > startedAt)) helpers.byId.delete(id);
  }
  for (const [id, h] of fresh) {
    if ((eventAt.get(id) ?? 0) > startedAt) continue;
    helpers.byId.set(id, h);
  }
  prune();
}

// Stopped rows come back to the list once the engine no longer runs them,
// or HOLD_AFTER_ANSWER_MS after it answered.
function prune(now = Date.now()) {
  for (const [id, answeredAt] of held) {
    const h = helpers.byId.get(id);
    if (!h || h.status !== "running" || (answeredAt > 0 && now - answeredAt >= HOLD_AFTER_ANSWER_MS)) held.delete(id);
  }
}

// Another chat shows (or none): everything starts again (2.15 State).
function showChat(sid) {
  if (sid === helpers.sid) return;
  readTurn += 1;
  reading = false;
  clearTimeout(timer);
  timer = null;
  helpers.sid = sid;
  helpers.byId.clear();
  eventAt.clear();
  held.clear();
  if (sid) read();
}

// -- Stop (DLG-229) --

// POST …/helpers/{hid}/stop, at once and with no confirmation; the row leaves
// at once. Success says "Stopped sub-agent <name>", failure the engine's
// message (background.go:172-178).
export async function stopHelper(h) {
  const sid = helpers.sid;
  if (!sid || !h?.id) return false;
  held.set(h.id, 0);
  try {
    await api.stopHelper(sid, h.id);
  } catch (error) {
    held.delete(h.id);
    notify("error", error?.message ?? String(error));
    return false;
  }
  if (held.has(h.id)) held.set(h.id, Date.now());
  notify("info", `Stopped sub-agent ${h.name}`);
  return true;
}

// -- Start --

let started = false;

export function startHelpers() {
  if (started) return;
  started = true;
  $effect.root(() => {
    $effect(() => {
      const sid = sessions.currentId;
      untrack(() => showChat(sid));
    });
  });
  onEvent("helper", (_kind, h) => {
    if (!h?.id || !helpers.sid || h.session_id !== helpers.sid) return;
    eventAt.set(h.id, Date.now());
    helpers.byId.set(h.id, h);
    prune();
    if (h.status === "running" && !timer && !reading) schedule();
  });
  onResync(() => read());
}

let watching = false;

// Two watches of the shown chat for the plugin: the agent's capabilities
// (can_background, which the 60-second hint needs) are read when a chat opens
// (2.15; MOD's store reads them again after every config_changed, 7.4), and a
// tray with nothing left in it gives the keys back to the typing box
// (tasks.go:101-108).
export function startWatches() {
  if (watching) return;
  watching = true;
  $effect.root(() => {
    $effect(() => {
      if (sessions.currentId) untrack(() => refreshCapabilities());
    });
    $effect(() => {
      if (ui.focus === "tray" && traySections.length === 0) untrack(() => focusField());
    });
  });
}
