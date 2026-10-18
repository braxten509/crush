// Branch links between chats (desktop-design 3.3). Scaffold S3: the final
// exports with empty data; piece P4.1 fills them.

export const links = $state({ byId: {}, byParentMessage: {}, originOf: {} }); // originOf: branch session id → origin ChatLink

// api.chatLinks(); on start, on resync, after any session "deleted" event (engine-design.md:2310)
export async function loadLinks() {}

// chat_link created
export function upsertLink(link) {}

// Decorations for the renderer: [{kind: "branch-link", start, end, data: link}]
export function linksFor(messageId) {
  return [];
}

// The origin link's parent_session_id, or null (top level).
export function parentOf(sessionId) {
  return null;
}
