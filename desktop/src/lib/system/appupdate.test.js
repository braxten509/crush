// The update's question (owner, 2026-11-12): the answer decides whether the
// engines stop before the app starts again, and Cancel updates nothing.
import { beforeEach, describe, expect, it, vi } from "vitest";

const steps = []; // what the update did, in order
const appUpdateRun = vi.fn(async () => (steps.push("run"), { ok: true, commit: "abc1234" }));
const appRestartHold = vi.fn(async () => true);
const enginesStop = vi.fn(async (dirs) => steps.push(`stop ${JSON.stringify(dirs)}`));
vi.mock("../engine/commands.js", () => ({ appRestartHold, appUpdateCheck: vi.fn(), appUpdateRun, enginesStop }));
vi.mock("../engine/events.js", () => ({ onEvent: () => {} }));
vi.mock("../closeask.js", () => ({ runningWork: async () => ({ chats: 1, helpers: 0, waiting: 0, dirs: ["", "/b"] }) }));
let answer = "keep";
vi.mock("../dialogs.js", () => ({ dialogRegistered: () => true, openDialog: (_id, props) => (props.answer(answer), true) }));
vi.mock("../stores/connection.svelte.js", () => ({ conn: { project: { cwd: "/shown" } } }));
vi.mock("../stores/sessions.svelte.js", () => ({ sessions: { currentId: "chat-1" } }));
vi.mock("../stores/ui.svelte.js", () => ({ notify: () => {} }));
vi.mock("@tauri-apps/api/webviewWindow", () => ({ getCurrentWebviewWindow: () => ({ relaunch: () => steps.push("relaunch") }) }));

let update;

beforeEach(async () => {
  vi.resetModules();
  vi.useFakeTimers();
  steps.length = 0;
  update = await import("./appupdate.svelte.js");
});

describe("the update's question about the engines", () => {
  it.each([
    ["restart", ["run", 'stop ["","/b"]', "relaunch"]],
    ["keep", ["run", "relaunch"]],
    ["cancel", []],
  ])("%s", async (choice, did) => {
    answer = choice;
    await update.updateApp();
    await vi.runAllTimersAsync();
    expect(steps).toEqual(did);
  });
});
