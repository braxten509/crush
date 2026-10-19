// This window's terminal tabs (desktop-design-5-6 2.25, 7.4). Placeholder
// from scaffold S5 with the pinned exports; TERM owns it (7.3).

// [{id, title, cwd, exited}]
export const terminals = $state([]);

// → {id, exited: Promise<{code}>}
export function openTerminal({ cwd, argv, title }) {
  return { id: "", exited: Promise.resolve({ code: null }) };
}

export function showTerminal(_id) {}

export function askCloseTerminal(_id) {}

export async function closeAllTerminals() {}
