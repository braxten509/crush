// The tray's to-do section (desktop-design 1.5.3); the to-dos themselves are
// sessions.byId[sid].todos. Scaffold S3: the pinned exports with empty
// bodies; piece P3.7 writes them.

export const todos = $state({ open: false, openedFor: {} }); // openedFor: sid → true once it opened by itself (SIDE-032)

// SIDE-032: only while the shown chat has unfinished to-dos.
export function toggle() {}

// { done, total, active: Todo | null }
export function progress(sid) {}
