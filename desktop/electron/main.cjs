// Crush Desktop on Electron (owner 2026-10-20). Under Tauri the windows were
// WebKitGTK, which on the owner's 170 Hz screen moved a scrolled list only
// about 45 times a second; Chromium scrolls at the screen's rate. The screens
// (dist/) are unchanged. The Rust side runs as a helper program,
// crush-desktop-core (src-tauri), which keeps the engine connections, files,
// terminals, settings and alerts; this process owns the windows and the
// system parts only Electron has (dialogs, notifications, clipboard), and
// passes each page's invoke() to the helper. The messages are listed in
// src-tauri/src/host.rs.
"use strict";

const { app, BrowserWindow, Menu, Notification, clipboard, dialog, ipcMain, protocol } = require("electron");
const { execFile, spawn } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const readline = require("node:readline");

// -- Where things are: the source tree (electron/ next to dist/ and
// src-tauri/) or the installed folder (everything next to this file).

function found(...candidates) {
  return candidates.find((candidate) => candidate && fs.existsSync(candidate));
}

const ROOT = path.join(__dirname, "..");
const DIST = found(process.env.CRUSH_DESKTOP_DIST, path.join(__dirname, "dist"), path.join(ROOT, "dist"));
const CORE = found(
  process.env.CRUSH_DESKTOP_CORE,
  path.join(__dirname, "crush-desktop-core"),
  path.join(ROOT, "src-tauri", "target", "release", "crush-desktop-core"),
);
const ICON = found(path.join(__dirname, "icon.png"), path.join(ROOT, "src-tauri", "icons", "icon.png"));
const PAGE = "app://localhost/index.html";

// The page's rules (they were tauri.conf.json's): only the app's own files,
// pictures also from blob: and data:, no frames. WebAssembly may compile (not
// eval): the terminal's picture add-on decodes sixel and iTerm2 pictures with it.
const CSP = [
  "default-src 'self'",
  "script-src 'self' 'wasm-unsafe-eval'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' blob: data:",
  "font-src 'self' data:",
  "connect-src 'self'",
  "object-src 'none'",
  "base-uri 'none'",
  "form-action 'none'",
  "frame-src 'none'",
].join("; ");

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".woff2": "font/woff2",
  ".woff": "font/woff",
  ".ttf": "font/ttf",
  ".wasm": "application/wasm",
};

// -- The command line. Electron's own switches and the script path are not
// the app's; the helper reads the rest as the terminal's flags (main.rs).
const args = process.argv.slice(app.isPackaged ? 1 : 2).filter((arg) => !/^--(ozone|enable-features|disable-features|no-sandbox)/.test(arg));
// REM-017: a phone launch names itself in CRUSH_REMOTE_LAUNCH; it travels as
// --launch-id, so the single-instance hand-off (arguments only) keeps it.
if (process.env.CRUSH_REMOTE_LAUNCH !== undefined) {
  if (process.env.CRUSH_REMOTE_LAUNCH) args.push("--launch-id", process.env.CRUSH_REMOTE_LAUNCH);
  delete process.env.CRUSH_REMOTE_LAUNCH;
}
if (args.includes("--version")) {
  process.stdout.write(`crush-desktop ${app.getVersion()} (Electron ${process.versions.electron})\n`);
  process.exit(0);
}

// Chromium's own files (cache, page storage) stay out of the settings folder
// (~/.config/crush-desktop), which the helper owns.
// One app per user: a second start hands its flags to the first (windows.rs
// handoff). As under Tauri, whose lock on Linux is a session-bus name, there
// is no lock on Linux without a session bus (the test sandbox): each start is
// its own app there, with its own Chromium files. (Chromium itself writes
// "disabled:" into the variable when it is unset.)
const bus = process.env.DBUS_SESSION_BUS_ADDRESS ?? "";
const single = process.platform !== "linux" || (bus !== "" && !bus.startsWith("disabled:"));
if (process.platform === "linux") {
  const dataHome = process.env.XDG_DATA_HOME || path.join(os.homedir(), ".local", "share");
  app.setPath("userData", path.join(dataHome, "crush-desktop", single ? "electron" : `electron-${process.pid}`));
}
app.commandLine.appendSwitch("ozone-platform-hint", "auto");
// A frame shows only once its picture is painted (owner, 2026-10-20: "when I
// drag this and a lot of text scrolls, the entire chat goes blank until I
// stop"): dragging the chat's scroll bar jumps thousands of pixels a frame,
// and Chromium otherwise draws the parts it has not painted yet as empty
// paper (tests/driver/scroll_drag.py records them).
app.commandLine.appendSwitch("run-all-compositor-stages-before-draw");
// The window's class (X11) and app id (Wayland) is "crush-desktop", the name
// the menu entry and the test drivers know it by.
app.setName("crush-desktop");

if (single && !app.requestSingleInstanceLock({ argv: args, cwd: process.cwd() })) {
  app.quit();
  process.exit(0);
}

protocol.registerSchemesAsPrivileged([
  { scheme: "app", privileges: { standard: true, secure: true, supportFetchAPI: true, corsEnabled: true, stream: true } },
]);

// -- The helper.

let helper = null;
let nextCall = 1;
const calls = new Map(); // id → resolve
const windows = new Map(); // label → BrowserWindow
const labels = new Map(); // webContents id → label

function send(message) {
  if (helper?.stdin.writable) helper.stdin.write(JSON.stringify(message) + "\n");
}

function startHelper() {
  if (!CORE) fail("The Crush Desktop helper program (crush-desktop-core) is missing.");
  helper = spawn(CORE, args, { stdio: ["pipe", "pipe", "inherit"], cwd: process.cwd(), env: process.env });
  helper.on("error", (e) => fail(`The Crush Desktop helper could not start: ${e.message}`));
  helper.on("exit", (code, signal) => {
    helper = null;
    if (!quitting) fail(`The Crush Desktop helper stopped (${signal ?? `code ${code}`}).`);
  });
  readline.createInterface({ input: helper.stdout }).on("line", (line) => {
    let message;
    try {
      message = JSON.parse(line);
    } catch {
      console.error(`crush-desktop: an unreadable line from the helper: ${line.slice(0, 200)}`);
      return;
    }
    fromHelper(message);
  });
}

function fail(text) {
  console.error(`crush-desktop: ${text}`);
  if (app.isReady() && !process.env.CRUSH_DESKTOP_NO_DIALOGS) {
    const parent = BrowserWindow.getFocusedWindow() ?? [...windows.values()].find((win) => !win.isDestroyed());
    if (parent) dialog.showMessageBoxSync(parent, { type: "error", title: "Crush Desktop", message: text });
    else dialog.showErrorBox("Crush Desktop", text);
  }
  quitting = true;
  app.exit(1);
}

function fromHelper(message) {
  switch (message.t) {
    case "ready":
      createWindow("main");
      break;
    case "result": {
      const resolve = calls.get(message.id);
      calls.delete(message.id);
      resolve?.("err" in message ? { err: message.err } : { ok: message.ok ?? null });
      break;
    }
    case "emit":
      for (const [label, win] of windows) {
        if (message.label === null || message.label === label) win.webContents.send("crush-event", message.event, message.payload);
      }
      break;
    case "channel":
      windows.get(message.label)?.webContents.send("crush-channel", message.channel, message.data);
      break;
    case "ask":
      Promise.resolve()
        .then(() => answer(message))
        .then(
          (ok) => send({ t: "reply", id: message.id, ok: ok ?? null }),
          (e) => send({ t: "reply", id: message.id, err: String(e?.message ?? e) }),
        );
      break;
    case "do":
      act(message);
      break;
    default:
      console.error(`crush-desktop: an unknown message from the helper: ${message.t}`);
  }
}

// What the helper asks for and waits on.
async function answer(message) {
  switch (message.op) {
    case "window.create":
      createWindow(message.label);
      return true;
    case "notify":
      if (!Notification.isSupported()) throw new Error("this desktop shows no notifications");
      new Notification({ title: message.title, body: message.body, icon: ICON }).show();
      return true;
    case "pick-folder": {
      const parent = BrowserWindow.getFocusedWindow() ?? [...windows.values()].find((win) => !win.isDestroyed());
      const picked = await dialog.showOpenDialog(parent, {
        title: message.title,
        defaultPath: message.start || undefined,
        properties: ["openDirectory", "createDirectory"],
      });
      return picked.canceled ? "" : (picked.filePaths[0] ?? "");
    }
    case "clipboard.write":
      // Electron's clipboard calls hand back promises (Electron 44).
      await clipboard.writeText(message.text);
      // Read back, as the terminal does, so a lost write is tried again.
      return message.text === "" || (await clipboard.readText()) === message.text;
    case "clipboard.read":
      return (await clipboard.readText()) || null;
    default:
      throw new Error(`unknown ask ${message.op}`);
  }
}

// What the helper tells a window to do.
function act(message) {
  if (message.op !== "window") return;
  const win = windows.get(message.label);
  if (!win || win.isDestroyed()) return;
  switch (message.action) {
    case "unminimize":
      win.restore();
      break;
    case "show":
      win.show();
      break;
    case "focus":
      win.focus();
      break;
    case "destroy":
      win.destroy();
      break;
    case "eval":
      win.webContents.executeJavaScript(message.script).catch(() => {});
      break;
    case "attention":
      win.flashFrame(true);
      break;
    case "taskbar":
      app.setBadgeCount(message.badge ?? 0);
      // 2: the indeterminate mark where the desktop has one, -1: none.
      win.setProgressBar(message.busy ? 2 : -1);
      break;
  }
}

// -- Windows.

// On Linux the page draws the window's title bar itself (owner 2026-10-20,
// design D: the system bar's parts plus a pin that keeps the window above the
// others; system/TitleBar.svelte). Mac and Windows keep their own bars.
// Moving, snapping, double-click and the right-click window menu stay the
// system's: Electron hands presses on the bar's drag area to the compositor.
const OWN_BAR = process.platform === "linux";

// The window's state the bar shows: { bar, maximized, fullscreen, pinned }.
// Like the system's, the bar steps aside in full screen.
function windowState(win) {
  return { bar: OWN_BAR, maximized: win.isMaximized(), fullscreen: win.isFullScreen(), pinned: Boolean(pins.get(win)) };
}

function sendState(win) {
  if (!win.isDestroyed()) win.webContents.send("crush-window-state", windowState(win));
}

// -- The pin: keep the window above the others. On KDE under Wayland an app
// may not raise itself above the others, so the pin asks KWin to turn on its
// own "keep above" through a KWin script (owner's choice); elsewhere Electron
// does it. The pin is remembered per window label (owner's choice) in
// window-pins.json next to Chromium's files.

const pins = new WeakMap(); // BrowserWindow → pinned
const pendingPins = new WeakSet(); // windows still to pin once they show a title
const KWIN = process.platform === "linux" && process.env.XDG_SESSION_TYPE === "wayland" && /KDE/i.test(process.env.XDG_CURRENT_DESKTOP ?? "");

function pinFile() {
  return path.join(app.getPath("userData"), "window-pins.json");
}

function savedPins() {
  try {
    const saved = JSON.parse(fs.readFileSync(pinFile(), "utf8"));
    return saved && typeof saved === "object" ? saved : {};
  } catch {
    return {};
  }
}

function savePin(label, on) {
  const saved = savedPins();
  if (on) saved[label] = true;
  else delete saved[label];
  try {
    fs.writeFileSync(pinFile(), JSON.stringify(saved));
  } catch (e) {
    console.error(`crush-desktop: the pin could not be saved: ${e.message}`);
  }
}

function setPinned(win, on) {
  pins.set(win, on);
  win.setAlwaysOnTop(on);
  if (KWIN) kwinKeepAbove(win, on);
  const label = labels.get(win.webContents.id);
  if (label) savePin(label, on);
  sendState(win);
}

// The script picks this app's window: the only one, else the active one
// (the pin was just clicked there), else the one with this window's title.
let kwinScripts = 0;
let kwinDialogWatch = null;
function kwinKeepAbove(win, on) {
  const name = `crush-desktop-pin-${process.pid}-${++kwinScripts}`;
  const file = path.join(os.tmpdir(), `${name}.js`);
  const watch = !kwinDialogWatch;
  if (watch) kwinDialogWatch = name;
  const script = `(function () {
  const pid = ${process.pid};
  const title = ${JSON.stringify(win.getTitle())};
  const mine = workspace.windowList().filter((w) => w.pid === pid && w.normalWindow);
  const w = mine.length === 1 ? mine[0]
    : mine.find((w) => w.caption === title || w.caption.startsWith(title + " <")) || mine.find((w) => w === workspace.activeWindow);
  if (!w) throw new Error("the window to pin is missing");
  w.keepAbove = ${on ? "true" : "false"};
  if (!${watch}) return;
  // Portal dialogs can belong to another process. Follow the transient
  // parent chain, including confirmations opened over a file picker.
  const original = new Map();
  function sync() {
    workspace.windowList().forEach((child) => {
      const seen = new Set();
      let parent = child.transientFor;
      while (parent && parent.transientFor && !seen.has(parent)) {
        seen.add(parent);
        parent = parent.transientFor;
      }
      if (parent && parent.pid === pid && parent.normalWindow && parent.keepAbove) {
        if (!original.has(child)) original.set(child, child.keepAbove);
        child.keepAbove = true;
      } else if (original.has(child)) {
        child.keepAbove = original.get(child);
        original.delete(child);
      }
    });
  }
  function added(child) {
    child.transientChanged.connect(sync);
    child.closed.connect(() => original.delete(child));
    if (child.pid === pid && child.normalWindow) child.keepAboveChanged.connect(sync);
    sync();
  }
  workspace.windowList().forEach(added);
  workspace.windowAdded.connect(added);
})();
`;
  const gdbus = (call, ...rest) =>
    new Promise((resolve, reject) =>
      execFile("gdbus", ["call", "--session", "--dest", "org.kde.KWin", ...call, ...rest], { timeout: 5000 }, (e, out) =>
        e ? reject(e) : resolve(out),
      ),
    );
  const unload = () => gdbus(["--object-path", "/Scripting", "--method", "org.kde.kwin.Scripting.unloadScript"], name).catch(() => {});
  // One watcher covers all of this app's windows until the app quits.
  if (watch) app.once("will-quit", unload);
  fs.promises
    .writeFile(file, script)
    .then(() => gdbus(["--object-path", "/Scripting", "--method", "org.kde.kwin.Scripting.loadScript"], file, name))
    .then((out) => {
      const id = /-?\d+/.exec(out)?.[0];
      if (!id || Number(id) < 0) throw new Error(`KWin refused the script (${out.trim()})`);
      return gdbus(["--object-path", `/Scripting/Script${id}`, "--method", "org.kde.kwin.Script.run"]);
    })
    .catch((e) => {
      if (watch) kwinDialogWatch = null;
      unload();
      console.error(`crush-desktop: KWin did not take the pin: ${e.message}`);
    })
    .finally(() => {
      if (!watch) unload();
      fs.promises.rm(file, { force: true }).catch(() => {});
    });
}

function createWindow(label) {
  if (windows.has(label)) return windows.get(label);
  const win = new BrowserWindow({
    width: 1586,
    height: 992,
    minWidth: 900,
    minHeight: 560,
    title: "Crush",
    icon: ICON,
    frame: !OWN_BAR,
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      spellcheck: false,
    },
  });
  windows.set(label, win);
  labels.set(win.webContents.id, label);
  // The page names the window itself (system/title.svelte.js).
  win.on("page-title-updated", (event) => event.preventDefault());
  // The system close button: the page runs its close steps (lib/closing.js),
  // then asks the helper to close the window (window_close).
  win.on("close", (event) => {
    if (quitting) return;
    event.preventDefault();
    send({ t: "window", label, event: "close-requested" });
  });
  win.on("closed", () => {
    windows.delete(label);
    send({ t: "window", label, event: "destroyed" });
  });
  for (const event of ["maximize", "unmaximize", "enter-full-screen", "leave-full-screen", "always-on-top-changed"]) {
    win.on(event, () => sendState(win));
  }
  // A remembered pin comes back once the page names the window, which is
  // when it is on screen and KWin can tell it apart by its title.
  if (savedPins()[label]) {
    pins.set(win, true);
    pendingPins.add(win);
  }
  win.on("focus", () => send({ t: "window", label, event: "focus" }));
  win.on("blur", () => send({ t: "window", label, event: "blur" }));
  win.webContents.on("render-process-gone", (_event, details) =>
    send({ t: "window", label, event: "crashed", reason: details.reason }),
  );
  // The navigation guard (1.2.9): only the app's own pages. Links open in the
  // system's browser through the helper's open_link.
  win.webContents.on("will-navigate", (event, url) => {
    if (url.startsWith("app://localhost/")) return;
    event.preventDefault();
    refused(url);
  });
  win.webContents.setWindowOpenHandler(({ url }) => {
    refused(url);
    return { action: "deny" };
  });
  win.loadURL(PAGE);
  send({ t: "window", label, event: "created" });
  return win;
}

function refused(url) {
  let scheme = "unknown:";
  try {
    scheme = new URL(url).protocol;
  } catch {}
  send({ t: "refused", scheme });
}

// -- The page's side (preload.cjs).

ipcMain.on("crush-label", (event) => {
  event.returnValue = labels.get(event.sender.id) ?? "main";
});

ipcMain.handle("crush-invoke", (event, cmd, callArgs) => {
  const label = labels.get(event.sender.id);
  if (!label) return { err: "the window is gone" };
  if (!helper) return { err: "the helper is not running" };
  const id = nextCall++;
  return new Promise((resolve) => {
    calls.set(id, resolve);
    send({ t: "call", id, label, cmd, args: callArgs ?? {} });
  });
});

ipcMain.on("crush-window", (event, action, value) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  if (!win) return;
  if (action === "title") {
    win.setTitle(String(value));
    if (pendingPins.delete(win)) setPinned(win, true);
  } else if (action === "minimize") win.minimize();
  else if (action === "maximize") win.isMaximized() ? win.unmaximize() : win.maximize();
  // The close event runs the page's close steps (createWindow).
  else if (action === "close") win.close();
  else if (action === "pin") setPinned(win, Boolean(value));
  // After an update (the panel's update button): the app starts again on the
  // new files; the helper already holds the open chats (app_restart_hold).
  else if (action === "relaunch") {
    // A fixture run (the checks) stays up: the driver reads what happened.
    if (args.some((arg) => arg === "--fixture" || arg.startsWith("--fixture="))) {
      console.error("crush-desktop: relaunch asked (fixture run: kept running)");
      return;
    }
    quitting = true;
    app.relaunch({ args: relaunchArgs(value) });
    app.quit();
  }
});

// The command line of the start after an update: this one, with the
// window's folder and chat (where = {cwd, session}) in place of any folder,
// chat, --continue or --new-chat it began with (owner, 2026-10-21: the app
// came back in the folder it was first started in, not the window's).
// Without where, the same command line again.
function relaunchArgs(where) {
  if (!where) return process.argv.slice(1);
  const cwd = typeof where.cwd === "string" ? where.cwd : "";
  const session = typeof where.session === "string" ? where.session : "";
  const head = app.isPackaged ? 1 : 2; // a plain electron also takes the app's folder
  const out = process.argv.slice(1, head);
  const rest = process.argv.slice(head);
  const folder = new Set(["-c", "--cwd", "--project"]);
  const chat = new Set(["-s", "--session", "--launch-id"]);
  const switches = new Set(["-C", "--continue", "--new-chat"]);
  for (let i = 0; i < rest.length; i++) {
    const arg = rest[i];
    const name = arg.startsWith("--") ? arg.split("=")[0] : arg;
    const glued = /^-[cs]./.test(arg) ? arg[1] : ""; // -cDIR, -sID: the value glued on
    if ((folder.has(name) && cwd) || chat.has(name)) {
      if (!arg.includes("=")) i += 1; // its value is the next argument
      continue;
    }
    if (switches.has(name) || glued === "s" || (glued === "c" && cwd)) continue;
    out.push(arg);
  }
  if (cwd) out.push("--cwd", cwd);
  if (session) out.push("-s", session);
  return out;
}

ipcMain.on("crush-window-state", (event) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  event.returnValue = win ? windowState(win) : { bar: false, maximized: false, fullscreen: false, pinned: false };
});

// -- The app.

let quitting = false;

app.on("second-instance", (_event, argv, cwd, data) => {
  send({ t: "handoff", argv: ["crush-desktop", ...(data?.argv ?? argv.slice(1))], cwd: data?.cwd ?? cwd });
});

// The app ends with its last window (windows.rs close).
app.on("window-all-closed", () => {
  quitting = true;
  app.quit();
});

app.on("before-quit", () => {
  quitting = true;
});

app.on("will-quit", () => {
  // The helper ends when its input closes.
  helper?.stdin.end();
});

app.whenReady().then(() => {
  // No menu: its keys (reload, developer tools, close) are not the app's.
  Menu.setApplicationMenu(null);
  protocol.handle("app", serve);
  startHelper();
});

// app://localhost/<file>: a file of dist/, with the page's rules.
async function serve(request) {
  const url = new URL(request.url);
  let name = decodeURIComponent(url.pathname);
  if (name === "/" || name === "") name = "/index.html";
  const file = path.normalize(path.join(DIST, name));
  if (url.host !== "localhost" || !file.startsWith(DIST + path.sep)) return new Response("not found", { status: 404 });
  try {
    const body = await fs.promises.readFile(file);
    const headers = { "content-type": TYPES[path.extname(file)] ?? "application/octet-stream" };
    if (file.endsWith(".html")) headers["content-security-policy"] = CSP;
    return new Response(body, { headers });
  } catch {
    return new Response("not found", { status: 404 });
  }
}
