// The nested tools of an Agent or Agentic Fetch row (CHAT-078, CHAT-079):
// the call's children when the timeline gave them, else the tool calls of
// its helper chat, which the messages store keeps as "<message id>$$<tool
// call id>" (desktop-design 1.5.3). Port of model/session_load.go:401-476
// (each nested call with its result) and chat/tools.go:356-377 (its status).

// The calls of a helper chat's messages (in chat order) as ToolCallViews
// (lib/types.js). A call whose input still streams is "pending"; one without
// a result in a reply that was stopped is "cancelled"; else "running".
export function callsFromMessages(list) {
  const calls = [];
  const byId = new Map();
  for (const m of list ?? []) {
    if (!m) continue;
    const parts = m.parts ?? [];
    const finish = parts.find((p) => p?.type === "finish")?.data ?? null;
    for (const part of parts) {
      if (part?.type === "tool_call") {
        const d = part.data ?? {};
        const call = {
          id: String(d.id ?? ""),
          name: String(d.name ?? ""),
          input: String(d.input ?? ""),
          messageId: m.id,
          status: d.finished === false ? "pending" : finish?.reason === "canceled" ? "cancelled" : "running",
          result: null,
          hooks: [],
          children: [],
        };
        if (byId.has(call.id)) calls[calls.indexOf(byId.get(call.id))] = call;
        else calls.push(call);
        byId.set(call.id, call);
      } else if (part?.type === "tool_result") {
        const call = byId.get(part.data?.tool_call_id);
        if (call) call.result = part.data;
      }
    }
  }
  return calls;
}

// The messages of a loaded chat ({ids, byId}) in chat order.
export function chatList(chat) {
  if (!chat) return [];
  return (chat.ids ?? []).map((id) => chat.byId.get(id)).filter(Boolean);
}
