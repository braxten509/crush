const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

function signal() {
  const callbacks = new Set();
  return { connect: (fn) => callbacks.add(fn), disconnect: (fn) => callbacks.delete(fn), emit: (...args) => [...callbacks].forEach((fn) => fn(...args)) };
}

test("pin follows existing, later and nested system dialogs, and leaves other windows alone", async () => {
  const source = fs.readFileSync(`${__dirname}/main.cjs`, "utf8");
  const scripts = [];
  const host = vm.createContext({
    process: { pid: 123 }, app: { once: () => {} }, console, path: require("node:path"), os: require("node:os"),
    fs: { promises: { writeFile: async (_file, text) => scripts.push(text), rm: async () => {} } },
    execFile: (_cmd, _args, _options, done) => done(null, "(1,)"),
  });
  vm.runInContext(source.slice(source.indexOf("let kwinScripts ="), source.indexOf("function createWindow(")), host);
  const win = { getTitle: () => "Crush test", once: () => {}, isDestroyed: () => false };
  host.win = win;
  vm.runInContext("kwinKeepAbove(win, true)", host);
  await new Promise(setImmediate);
  const root = { pid: 123, normalWindow: true, caption: "Crush test", keepAbove: false, keepAboveChanged: signal(), transientChanged: signal(), closed: signal() };
  const other = { pid: 123, normalWindow: true, caption: "Other Crush", keepAbove: false, keepAboveChanged: signal(), transientChanged: signal(), closed: signal() };
  const child = (parent, extra = {}) => ({ pid: 999, normalWindow: false, transientFor: parent, keepAbove: false, transientChanged: signal(), closed: signal(), ...extra });
  const picker = child(root);
  const unrelated = child(other);
  const list = [root, other, picker, unrelated];
  const workspace = { windowList: () => list, activeWindow: root, windowAdded: signal(), windowRemoved: signal() };
  vm.runInNewContext(scripts[0], { workspace });
  assert.equal(root.keepAbove, true);
  assert.equal(picker.keepAbove, true, "existing picker follows pin, even from another process");
  assert.equal(unrelated.keepAbove, false, "another Crush window's dialog stays unchanged");
  const nested = child(picker);
  list.push(nested);
  workspace.windowAdded.emit(nested);
  assert.equal(nested.keepAbove, true, "later nested confirmation follows pin");
  const lateParent = child(null);
  list.push(lateParent);
  workspace.windowAdded.emit(lateParent);
  lateParent.transientFor = root;
  lateParent.transientChanged.emit();
  assert.equal(lateParent.keepAbove, true, "late parent assignment follows pin");
  root.keepAbove = false;
  root.keepAboveChanged.emit();
  assert.equal(picker.keepAbove, false);
  assert.equal(nested.keepAbove, false);
  assert.equal(lateParent.keepAbove, false);
  assert.equal(other.keepAbove, false);
  other.keepAbove = true;
  other.keepAboveChanged.emit();
  assert.equal(unrelated.keepAbove, true, "each window's dialogs follow its own pin");
  assert.equal(picker.keepAbove, false);
});

test("folder and error dialogs keep a live parent even when focus has moved", async () => {
  const source = fs.readFileSync(`${__dirname}/main.cjs`, "utf8");
  const parent = { isDestroyed: () => false };
  const shown = [];
  const host = vm.createContext({
    BrowserWindow: { getFocusedWindow: () => null }, windows: new Map([["main", parent]]),
    app: { isReady: () => true, exit: () => {} }, process: { env: {} }, console: { error: () => {} }, quitting: false,
    dialog: {
      showOpenDialog: async (win) => { shown.push(win); return { canceled: true }; },
      showErrorBox: () => shown.push(null),
      showMessageBoxSync: (win) => shown.push(win),
    },
  });
  vm.runInContext(source.slice(source.indexOf("function fail("), source.indexOf("function fromHelper(")), host);
  vm.runInContext(source.slice(source.indexOf("async function answer("), source.indexOf("// What the helper tells")), host);
  await vm.runInContext('answer({ op: "pick-folder" })', host);
  vm.runInContext('fail("test failure")', host);
  assert.deepEqual(shown, [parent, parent]);
});
