// The window title (desktop-design-5-6 2.18, NOTE-001): "<chat title> ·
// <project>", the chat title being the shown chat's title or "New session",
// the project the base name of the window's project folder. It follows chat
// switches and renames.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { projectDir } from "../stores/composer.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";

const base = (path) => String(path ?? "").replace(/\/+$/, "").split("/").pop() || "";

// The two parts apart, for the page's own title bar (TitleBar.svelte).
// The project part is the folder the chat's agent really works in: its own
// copy of the project (a git worktree it made) when it moved into one.
// Crush's own copies (<data>/worktrees/<chat id>) keep the project name.
export function titleParts() {
  const chat = sessions.byId[sessions.currentId];
  return { chat: chat?.title?.trim() || "New session", project: base(workFolder(chat) || projectDir()) };
}

function workFolder(chat) {
  const dir = String(chat?.work_dir ?? "").replace(/\/+$/, "");
  if (!dir || /\/worktrees\/[0-9a-f-]{36}$/i.test(dir)) return "";
  return dir;
}

export function titleText() {
  const { chat, project } = titleParts();
  return project ? `${chat} · ${project}` : chat;
}

let started = false;

export function startTitle() {
  if (started) return;
  started = true;
  let win = null;
  try {
    win = getCurrentWebviewWindow();
  } catch {
    return;
  }
  let last = "";
  $effect.root(() => {
    $effect(() => {
      const text = titleText();
      if (text === last) return;
      last = text;
      document.title = text;
      win.setTitle(text).catch(() => {});
    });
  });
}
