// The window title (desktop-design-5-6 2.18, NOTE-001): "<chat title> ·
// <project>", the chat title being the shown chat's title or "New session",
// the project the base name of the window's project folder. It follows chat
// switches and renames.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { projectDir } from "../stores/composer.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";

const base = (path) => String(path ?? "").replace(/\/+$/, "").split("/").pop() || "";

export function titleText() {
  const chat = sessions.byId[sessions.currentId]?.title?.trim() || "New session";
  const project = base(projectDir());
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
