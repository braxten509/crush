// The page's line to Electron (main.cjs), in place of Tauri's: invoke() to
// the helper, the helper's events and channel messages back, and the two
// window calls the page makes itself. The page reaches it through
// src/lib/bridge/, which keeps Tauri's function names.
"use strict";

const { contextBridge, ipcRenderer, webUtils } = require("electron");

const label = ipcRenderer.sendSync("crush-label");
const listeners = new Map(); // event name → Set of callbacks
const channels = new Map(); // channel id → callback

ipcRenderer.on("crush-event", (_event, name, payload) => {
  for (const callback of listeners.get(name) ?? []) callback({ event: name, payload });
});

ipcRenderer.on("crush-channel", (_event, id, data) => channels.get(id)?.(data));

contextBridge.exposeInMainWorld("crushBridge", {
  label,
  // { ok } or { err }: the error crosses as the helper sent it.
  invoke: (cmd, args) => ipcRenderer.invoke("crush-invoke", cmd, args),
  listen(name, callback) {
    if (!listeners.has(name)) listeners.set(name, new Set());
    listeners.get(name).add(callback);
    return () => listeners.get(name)?.delete(callback);
  },
  channel(id, callback) {
    channels.set(id, callback);
  },
  window: (action, value) => ipcRenderer.send("crush-window", action, value),
  // The paths of files dropped on the window.
  pathsOf: (files) => [...files].map((file) => webUtils.getPathForFile(file)).filter(Boolean),
});
