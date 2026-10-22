// Sub-agent chats (owner, 2026-10-21): a SUBAGENTS row's click opens the
// sub-agent's own chat as a normal chat (typing box, Stop, scroll); what is
// typed there goes to the sub-agent (the engine's tasks_chat.go). Its left-list
// row sits under its folder, marked Subagent, in this window only (not in the
// open chats shared with the terminal). The row leaves once the sub-agent is
// done and its chat is not shown; its × hides the row and never stops the
// sub-agent.
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";
import { conn } from "./connection.svelte.js";
import { allowHelperChat, onLeave, openChat, removeSession, sessions } from "./sessions.svelte.js";

export const helperChats = $state({ byId: {} }); // child chat id → {id, parent, name, dir, running}


// Opens helper h's (proto.Helper) chat. Resolves true when it shows.
export function openHelperChat(h) {
  const id = h?.child_session_id;
  if (!id) return Promise.resolve(false);
  helperChats.byId[id] = {
    id,
    parent: h.session_id ?? "",
    name: h.name || "sub-agent",
    dir: conn.project.path || conn.project.cwd || "",
    running: h.status === "running",
  };
  allowHelperChat(id, true);
  return openChat(id);
}

// The × : the row goes; a shown chat gives its place to its parent.
export function hideHelperChat(id) {
  const chat = helperChats.byId[id];
  if (!chat) return;
  const shown = sessions.currentId === id;
  drop(id);
  if (shown && chat.parent) openChat(chat.parent);
}

function drop(id) {
  delete helperChats.byId[id];
  allowHelperChat(id, false);
  if (sessions.currentId !== id) removeSession(id);
}

// Done and not shown: the row leaves.
function settle() {
  for (const chat of Object.values(helperChats.byId)) {
    if (!chat.running && chat.id !== sessions.currentId) drop(chat.id);
  }
}

// The helper event can be dropped (endpoints_live.go), so leaving a
// sub-agent's chat also reads the parents' helpers, then settles.
async function refresh() {
  const parents = new Set(Object.values(helperChats.byId).filter((c) => c.running && c.parent).map((c) => c.parent));
  await Promise.all([...parents].map(async (parent) => {
    let list;
    try {
      list = await api.helpers(parent);
    } catch {
      return; // the next switch or event tries again
    }
    for (const h of Array.isArray(list) ? list : []) {
      const chat = helperChats.byId[h?.child_session_id];
      if (chat) chat.running = h.status === "running";
    }
  }));
  settle();
}

let started = false;

export function startHelperChats() {
  if (started) return;
  started = true;
  onEvent("helper", (_kind, h) => {
    const chat = h?.child_session_id ? helperChats.byId[h.child_session_id] : null;
    if (!chat) return;
    chat.running = h.status === "running";
    settle();
  });
  // Leaving a sub-agent's chat settles its row; an effect on the shown chat
  // stopped re-running there (2026-10-21), the switch itself always runs.
  onLeave((sid) => {
    if (helperChats.byId[sid]) queueMicrotask(refresh);
  });
}

// The list's rows of the sub-agent chats, as openlist.listedRows draws a
// chat: here(dir) tells the window's folder, busy(id) and rowState(record,
// busy) what it is doing.
export function helperRows({ here = () => false, busy = () => false, rowState = () => null, selectedId = "" } = {}) {
  return Object.values(helperChats.byId).map((chat) => {
    const record = sessions.byId[chat.id];
    const doing = record ? rowState(record, busy(chat.id)) : null;
    return {
      kind: "chat",
      id: chat.id,
      title: record?.title || chat.name,
      agent: "",
      time: "",
      at: 0,
      depth: 0,
      selected: chat.id === selectedId,
      closed: false,
      state: doing?.text ?? "",
      stateKind: doing?.kind ?? "",
      branches: 0,
      expanded: false,
      folder: here(chat.dir) ? null : { dir: chat.dir },
      dir: chat.dir,
      helper: true,
      parent: chat.parent,
    };
  });
}
