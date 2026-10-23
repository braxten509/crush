// Run on the private KWin through gpu-screen; never on the owner's desktop.
const { app, BrowserWindow, dialog } = require("electron");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { execFile } = require("node:child_process");
const { promisify } = require("node:util");
const assert = require("node:assert/strict");
const run = promisify(execFile);
const pause = () => new Promise((resolve) => setTimeout(resolve, 100));
const root = path.resolve(__dirname, "..");
const source = fs.readFileSync(path.join(__dirname, "main.cjs"), "utf8");
const pin = new Function("process", "app", "console", "path", "os", "fs", "execFile",
  source.slice(source.indexOf("let kwinScripts ="), source.indexOf("function createWindow(")) + "; return kwinKeepAbove;")
  (process, app, console, path, os, fs, execFile);
let next = 0;
async function query(body) {
  const name = `dialog-check-${process.pid}-${++next}`;
  const marker = `${name}:`;
  const file = path.join(os.tmpdir(), `${name}.js`);
  fs.writeFileSync(file, `print(${JSON.stringify(marker)} + JSON.stringify((function () { ${body} })()));`);
  const call = async (object, method, ...args) => (await run("gdbus", ["call", "--session", "--dest", "org.kde.KWin", "--object-path", object, "--method", method, ...args])).stdout;
  const loaded = await call("/Scripting", "org.kde.kwin.Scripting.loadScript", file, name);
  const id = /\d+/.exec(loaded)[0];
  try {
    await call(`/Scripting/Script${id}`, "org.kde.kwin.Script.run");
    for (let i = 0; i < 30; i++) {
      const log = fs.readFileSync(path.join(root, "tests/out/kwin/kwin.log"), "utf8");
      const line = log.split("\n").find((line) => line.includes(marker));
      if (line) return JSON.parse(line.slice(line.indexOf(marker) + marker.length));
      await pause();
    }
    throw new Error(`No KWin reply: ${name}`);
  } finally {
    // KWin numbers scripts by its current list size. Keep query scripts
    // until this private compositor exits, so they cannot collide with a
    // retained dialog watcher after an earlier query is unloaded.
    fs.unlinkSync(file);
  }
}
const state = () => query(`return workspace.stackingOrder.map(w => ({ title: w.caption, above: w.keepAbove, parent: w.transientFor && w.transientFor.caption, pid: w.pid, normal: w.normalWindow, dialog: w.dialog, modal: w.modal }));`);
async function until(check) {
  let seen;
  for (let i = 0; i < 50; i++) {
    seen = await state();
    if (check(seen)) return seen;
    await pause();
  }
  throw new Error(`Window condition failed: ${JSON.stringify(seen)}`);
}
app.whenReady().then(async () => {
  const owner = new BrowserWindow({ title: "Pinned dialog owner", width: 700, height: 500 });
  await owner.loadURL("data:text/html,<h1>Dialog pin check</h1>");
  await until((s) => s.some((w) => w.title === "Pinned dialog owner"));
  owner.setAlwaysOnTop(true);
  pin(owner, true);
  await until((s) => s.some((w) => w.title === "Pinned dialog owner" && w.above));
  const picked = dialog.showOpenDialog(owner, { title: "Pinned picker check", properties: ["openDirectory"] });
  const pickerState = await until((s) => s.findIndex((w) => w.title === "Pinned picker check" && w.above) > s.findIndex((w) => w.title === "Pinned dialog owner"));
  console.log("PASS: native picker is pinned above its owner", JSON.stringify(pickerState));
  pin(owner, false);
  await until((s) => s.some((w) => w.title === "Pinned picker check") && s.filter((w) => /Pinned dialog owner|Pinned picker check/.test(w.title)).every((w) => !w.above));
  pin(owner, true);
  await until((s) => s.some((w) => w.title === "Pinned picker check" && w.above));
  await query(`const w = workspace.windowList().find(w => w.caption === "Pinned picker check"); w.closeWindow(); return true;`);
  assert.equal((await picked).canceled, true);
  const controller = new AbortController();
  const message = dialog.showMessageBox(owner, { title: "Pinned message check", message: "System message", signal: controller.signal });
  await until((s) => s.findIndex((w) => w.title === "Pinned message check" && w.above) > s.findIndex((w) => w.title === "Pinned dialog owner"));
  controller.abort();
  await message;
  console.log("PASS: native message and modal dialog stay above the pinned window; unpin restores dialogs");
  owner.destroy();
  app.exit(0);
}).catch((error) => { console.error(error); app.exit(1); });
