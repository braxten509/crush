// The typing box: drafts, mode, popups, live `!` commands and badges
// (desktop-design 1.5.3). Scaffold S3: the pinned exports with empty bodies;
// piece P3.7 writes them.

export const composer = $state({
  drafts: {}, // sid ("" = the landing page) → { text, attachments: Attachment[] } (lib/types.js)
  mode: "normal", // "normal" | "plan" | "yolo" | "bang": the one mark shown (TYPE-002)
  placeholder: "", // TYPE-003
  switching: false, // a mode switch is in flight (TYPE-008)
  sending: false,
  deleteMode: false, // TYPE-038
  popup: null, // null | { kind: "@" | "$", query, rows: [], focused }
  shells: {}, // request_id → ShellState (lib/types.js): live `!` commands, read by lib/timeline.js (P3.5)
  planHandoff: null, // null | { sessionId, open } (TYPE-052)
  badges: [], // [{ id, text }] drawn after the mode mark; phase 6 adds the Remote badge (SIDE-025)
});

// text null removes the badge.
export function setBadge(id, text) {}

// TYPE-004, TYPE-041 to TYPE-049
export async function send() {}

// TYPE-020
export async function runShell(command) {}

// Esc while busy (TYPE-046, TYPE-021)
export async function stop() {}

// Ctrl+Enter (TYPE-047)
export async function interruptAndSend() {}

export function setText(text) {}

// Phase 5's file picker uses it too (Add Image, DLG-255).
export function addAttachment(attachment) {}

export function removeAttachment(index) {}

// Phase 5's palette skill entries (CMD-057, CMD-058).
export function attachSkill({ id, name }) {}

// True when the shown chat's draft has attachments or kept skills (TYPE-039).
export function hasChips() {
  return false;
}

// The field, unless a slot form is open (then the form).
export function focusField() {}
