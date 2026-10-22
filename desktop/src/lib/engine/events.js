// The window's event loop (desktop-design 1.5.4). Engine data never arrives as
// a window event: Rust rings the "events-ready" doorbell when this window's
// batch goes from empty to not empty, and the loop pulls the batch with
// events_take once per animation frame. Stores register at module load:
// onEvent(type, fn) for the envelope types of 1.5.3, onResync(fn) for resyncs.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { events_take } from "./commands.js";

const handlers = new Map(); // envelope type → Set of fn(eventKind, payload)
const folderHandlers = new Map(); // envelope type → Set of fn(eventKind, payload, folder)
const folderResyncs = new Set(); // fn(folder)
const resyncs = new Set(); // fn(): Promise<void>
const holds = new Map(); // envelope type → how many withHeld calls hold it
let held = []; // entries buffered while their type is held, in arrival order

let booked = false; // a frame is booked or its take is in flight
let rangAgain = false; // the doorbell rang while a frame was booked

// fn(eventKind, payload): eventKind is "created", "updated" or "deleted";
// payload is the engine's object. Returns a function that unregisters it.
export function onEvent(type, fn) {
  if (!handlers.has(type)) handlers.set(type, new Set());
  handlers.get(type).add(fn);
  return () => handlers.get(type)?.delete(fn);
}

// Events of another folder's engine (the helper's watch.rs; owner,
// 2026-10-22: every listed chat stays drawn and current) reach only these:
// fn(eventKind, payload, folder). The window's own handlers never see them,
// since the window's stores hold its own folder's state.
export function onFolderEvent(type, fn) {
  if (!folderHandlers.has(type)) folderHandlers.set(type, new Set());
  folderHandlers.get(type).add(fn);
  return () => folderHandlers.get(type)?.delete(fn);
}

// fn(folder) runs when another folder's events may have been missed (its
// stream just started, or they overflowed): its drawn chats are read again.
export function onFolderResync(fn) {
  folderResyncs.add(fn);
  return () => folderResyncs.delete(fn);
}

// fn() runs on every resync entry and every connection event with resync:
// true. Stores read their data again inside it, holding their event types
// with withHeld so nothing that arrives meanwhile is lost or applied early.
export function onResync(fn) {
  resyncs.add(fn);
  return () => resyncs.delete(fn);
}

// Runs fn() while events of these types are buffered, then replays them in
// their order. Holds of the same type nest. Resolves to fn's result.
export async function withHeld(types, fn) {
  for (const type of types) holds.set(type, (holds.get(type) ?? 0) + 1);
  try {
    return await fn();
  } finally {
    for (const type of types) {
      const left = holds.get(type) - 1;
      if (left > 0) holds.set(type, left);
      else holds.delete(type);
    }
    const ready = held.filter((entry) => !holds.has(entry.type));
    held = held.filter((entry) => holds.has(entry.type));
    for (const entry of ready) deliver(entry);
  }
}

// Starts the loop. Outside the app window (unit tests, a plain browser) there
// is no doorbell, and nothing happens.
export async function startEvents() {
  try {
    await getCurrentWebviewWindow().listen("events-ready", book);
  } catch {
    return;
  }
  // Anything that came before the listener: one take now.
  book();
}

function book() {
  if (booked) {
    rangAgain = true;
    return;
  }
  booked = true;
  rangAgain = false;
  // A minimized window draws no frames, so a timer books the take too:
  // without it a finished chat's sound waited until the window was opened
  // again (owner, 2026-10-21). Whichever comes first takes the batch.
  let ran = false;
  const once = () => {
    if (ran) return;
    ran = true;
    frame();
  };
  requestAnimationFrame(once);
  setTimeout(once, FALLBACK_MS);
}

// Long enough that a shown window always takes on its frame first.
const FALLBACK_MS = 250;

async function frame() {
  let entries = [];
  try {
    entries = await events_take();
  } catch (error) {
    console.error("events_take failed", error);
  }
  try {
    apply(entries);
  } finally {
    const more = entries.length > 0 || rangAgain;
    booked = false;
    if (more) book();
  }
}

// Hands each entry, in order, to its handlers (1.4.2 shapes). Unknown types
// are ignored (engine design 0.1).
function apply(entries) {
  for (const entry of entries) {
    if (entry?.folder) applyFolder(entry);
    else if (entry?.kind === "resync") runResyncs();
    else if (entry?.kind === "event" && holds.has(entry.type)) held.push(entry);
    else if (entry?.kind === "event") deliver(entry);
  }
}

function deliver(entry) {
  for (const fn of handlers.get(entry.type) ?? []) {
    try {
      fn(entry.event, entry.payload);
    } catch (error) {
      console.error(`a ${entry.type} handler failed`, error);
    }
  }
  if (entry.type === "connection" && entry.payload?.resync === true) runResyncs();
}

function applyFolder(entry) {
  const folder = entry.folder;
  if (entry.kind === "resync") {
    for (const fn of folderResyncs) {
      try {
        Promise.resolve(fn(folder)).catch((error) => console.error("a folder resync failed", error));
      } catch (error) {
        console.error("a folder resync failed", error);
      }
    }
    return;
  }
  if (entry.kind !== "event") return;
  for (const fn of folderHandlers.get(entry.type) ?? []) {
    try {
      fn(entry.event, entry.payload, folder);
    } catch (error) {
      console.error(`another folder's ${entry.type} handler failed`, error);
    }
  }
}

function runResyncs() {
  for (const fn of resyncs) {
    try {
      Promise.resolve(fn()).catch((error) => console.error("a resync failed", error));
    } catch (error) {
      console.error("a resync failed", error);
    }
  }
}
