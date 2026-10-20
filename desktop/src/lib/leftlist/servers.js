// Server rows (desktop-design-5-6 2.22; LIST-028 to LIST-032, LIST-047),
// ported from the session host's (internal/ui/sessionhost/servers.go): the
// ports the project's chats' background jobs listen on (G-18's GET servers,
// read every 1 s while the window is connected), each port once, in list
// order. A program with two or more ports folds into one row; opened, its
// servers follow it, named after the program holding each port. Rows show
// only in the open list, as many as leave room for one chat.
import { api } from "../engine/api.js";
import { open_link } from "../engine/commands.js";
import { conn } from "../stores/connection.svelte.js";
import { rootOf, sessions } from "../stores/sessions.svelte.js";
import { leftList } from "./store.svelte.js";

export const SERVER_HINT = " ↵ open · x stop · esc back"; // servers.go:37

// G-18's ServerPort as a row's server: group is the background program it is
// under (its job, else its process: session_host.go:47); ref names it for a
// stop.
export function toServer(entry) {
  const port = Number(entry?.port);
  if (!Number.isInteger(port) || port <= 0) return null;
  const jobId = typeof entry.job_id === "string" ? entry.job_id : "";
  const pid = Number.isInteger(entry.pid) && entry.pid > 0 ? entry.pid : 0;
  return {
    port,
    session: String(entry.session_id ?? ""),
    name: String(entry.name ?? ""),
    owner: String(entry.program ?? ""),
    group: jobId || (pid ? String(pid) : ""),
    ref: jobId ? { job_id: jobId } : pid ? { pid } : null,
  };
}

// The row a server folds into with the other ports of its program.
export function foldOf(server) {
  return `${server.session}/${server.group}`;
}

export function urlOf(port) {
  return `http://localhost:${port}`;
}

// G-18's answer in list order (the chats' places in the list, then the
// answer's order), each port once (servers.go:48-62).
export function orderServers(answer, chatOrder) {
  const place = new Map(chatOrder.map((id, i) => [id, i]));
  const placed = (Array.isArray(answer) ? answer : [])
    .map((entry, i) => ({ server: toServer(entry), i }))
    .filter((x) => x.server)
    .map((x) => ({ ...x, at: place.get(rootOf(x.server.session) || x.server.session) ?? Infinity }))
    .sort((a, b) => a.at - b.at || a.i - b.i);
  const seen = new Set();
  const out = [];
  for (const { server } of placed) {
    if (seen.has(server.port)) continue;
    seen.add(server.port);
    out.push(server);
  }
  return out;
}

// serverRows (servers.go:64-92): {server, ports, child}; ports is how many a
// folded program holds (0 on a server's row), child marks a server listed
// under its opened program.
export function serverRows(servers, openFolds = {}) {
  const count = new Map();
  for (const s of servers) if (s.group) count.set(foldOf(s), (count.get(foldOf(s)) ?? 0) + 1);
  const rows = [];
  for (const s of servers) {
    const fold = foldOf(s);
    if (!s.group || count.get(fold) < 2) {
      rows.push({ server: s, ports: 0, child: false });
    } else if (!rows.some((r) => r.ports > 0 && foldOf(r.server) === fold)) {
      rows.push({ server: s, ports: count.get(fold), child: false });
      if (openFolds[fold]) {
        for (const c of servers) if (c.group && foldOf(c) === fold) rows.push({ server: c, ports: 0, child: true });
      }
    }
  }
  return rows;
}

// The rows the open list shows: as many as leave room for one chat (2.22,
// servers.go:94-104). None in the strip.
export function shownRows() {
  if (leftList.strip) return [];
  return serverRows(leftList.servers, leftList.openFolds).slice(0, Math.max(leftList.serverRoom ?? Infinity, 0));
}

// A row's label and right-hand text, as the session host draws them
// (draw.go:336-375): a fold "<name>" with "<N> ports"; a server "<name>"
// with ":<port>", or "localhost:<port>" alone when it has no name; a fold's
// servers are named after the program holding each port.
export function rowTexts(row) {
  const s = row.server;
  if (row.ports > 0) return { label: s.name, right: `${row.ports} ports` };
  const name = row.child ? s.owner : s.name;
  if (!name) return { label: `localhost:${s.port}`, right: "" };
  return { label: name, right: `:${s.port}` };
}

export function isPicked(row, picked = leftList.picked) {
  if (!picked) return false;
  if (row.ports > 0) return picked.fold === foldOf(row.server);
  return !picked.fold && picked.port === row.server.port;
}

export function pick(row) {
  leftList.picked = row.ports > 0 ? { fold: foldOf(row.server) } : { port: row.server.port };
}

export function unpick() {
  if (leftList.picked) leftList.picked = null;
}

// useRow (servers.go:153-169): a fold opens or closes; a server opens in the
// browser.
export function useRow(row) {
  if (row.ports === 0) {
    openServer(row.server.port);
    return;
  }
  const fold = foldOf(row.server);
  const next = { ...leftList.openFolds };
  if (next[fold]) delete next[fold];
  else next[fold] = true;
  leftList.openFolds = next;
}

// http://localhost:<port> in the browser through open_link (P3.2); a failure
// is only logged (servers.go:39-46).
export function openServer(port) {
  const url = urlOf(port);
  open_link(url, conn.project.path || conn.project.cwd).catch((error) =>
    console.warn("Could not open the server's link", url, error?.message ?? error),
  );
}

let stops = 0;
let reads = 0;

// stopServer (servers.go:171-179; session_host.go:80-95): a direct job kill
// through the engine; the rows of that program leave at once and the next
// read shows whatever still listens.
export function stopServer(server) {
  unpick();
  stops += 1;
  const fold = foldOf(server);
  leftList.servers = leftList.servers.filter((s) => (server.group ? foldOf(s) !== fold : s.port !== server.port));
  if (!server.session || !server.ref) return;
  api.killJob(server.session, server.ref).catch((error) => console.error("stopping a server failed", error?.message ?? error));
}

// One read of G-18; an answer older than a newer read or a stop is dropped.
export async function readServers() {
  const mine = ++reads;
  const stopsBefore = stops;
  let answer = [];
  try {
    answer = await api.servers();
  } catch {
    answer = []; // no route (an older engine) or no engine: no rows
  }
  if (mine !== reads || stopsBefore !== stops) return;
  leftList.servers = orderServers(answer, sessions.order);
}

// serverKey (servers.go:181-210), while a shown row is picked: Enter uses it,
// X or Shift+X stops it, ↑ and ↓ move between the rows, Esc lets go; any
// other key lets go and goes on. True when it took the key.
export function serverKey(event) {
  if (!leftList.picked) return false;
  const shown = shownRows();
  const at = shown.findIndex((row) => isPicked(row));
  if (at < 0) {
    unpick();
    return false;
  }
  const plain = !event.ctrlKey && !event.altKey && !event.metaKey;
  switch (plain ? event.key : "") {
    case "Enter":
      pick(shown[at]);
      useRow(shown[at]);
      return true;
    case "x":
    case "X":
      stopServer(shown[at].server);
      return true;
    case "ArrowUp":
      if (event.shiftKey) break;
      pick(shown[Math.max(at - 1, 0)]);
      return true;
    case "ArrowDown":
      if (event.shiftKey) break;
      pick(shown[Math.min(at + 1, shown.length - 1)]);
      return true;
    case "Escape":
      unpick();
      return true;
  }
  unpick();
  return false;
}

// A click on a row (input.go:219-243): the stop slot stops the picked row; a
// fold, or the picked server, is picked and used; another server is picked.
export function clickRow(row, onStop = false) {
  if (onStop && isPicked(row)) {
    stopServer(row.server);
    return;
  }
  if (isPicked(row) || row.ports > 0) {
    pick(row);
    useRow(row);
    return;
  }
  pick(row);
}
