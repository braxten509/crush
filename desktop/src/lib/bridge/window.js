// Tauri's current window on Electron (owner 2026-10-20): the calls the app
// makes (listen, setTitle, minimize, label, file drops). vite.config.js points
// "@tauri-apps/api/webviewWindow" and "@tauri-apps/api/window" here.

function bridge() {
  const line = globalThis.window?.crushBridge;
  if (!line) throw new Error("not inside the app window");
  return line;
}

const current = {
  get label() {
    return bridge().label;
  },
  // The helper's events (host.rs emit); the callback gets {event, payload}.
  async listen(name, callback) {
    return bridge().listen(name, callback);
  },
  async setTitle(text) {
    bridge().window("title", text);
  },
  async minimize() {
    bridge().window("minimize");
  },
  // Files dropped on the window: {payload: {type: "drop", paths}}. A file
  // dropped anywhere else would make the page navigate to it, so every file
  // drag is taken here.
  async onDragDropEvent(callback) {
    const line = bridge();
    const files = (event) => [...(event.dataTransfer?.types ?? [])].includes("Files");
    const over = (event) => {
      if (files(event)) event.preventDefault();
    };
    const drop = (event) => {
      if (!files(event)) return;
      event.preventDefault();
      // A plain array: Electron's bridge carries File objects but not a FileList.
      const paths = line.pathsOf([...event.dataTransfer.files]);
      if (paths.length) callback({ payload: { type: "drop", paths } });
    };
    window.addEventListener("dragover", over);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragover", over);
      window.removeEventListener("drop", drop);
    };
  },
};

export function getCurrentWebviewWindow() {
  bridge();
  return current;
}

export const getCurrentWindow = getCurrentWebviewWindow;
