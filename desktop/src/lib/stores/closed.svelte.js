// Closed chats (desktop-design 3.3), kept per engine data folder in the
// desktop settings file. Scaffold S3: the final exports with empty data;
// piece P4.1 fills them.

export const closed = $state({ ids: new Set() }); // from settings.closed_chats[engine data_dir]

// closed_set(project, id, true)
export async function close(sessionId) {}

// closed_set(project, id, false)
export async function reopen(sessionId) {}

export function isClosed(sessionId) {
  return false;
}
