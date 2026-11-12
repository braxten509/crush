import { afterAll, expect, mock, test } from "bun:test"
import * as fs from "node:fs"
import * as os from "node:os"
import * as path from "node:path"

// The real cap is 11 seconds; the tests use a short one so none waits long.
const autoBackgroundAfter = 400

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "crush-bash-test-"))
const previousHome = process.env.HOME
const previousSignal = process.env.CRUSH_BG_SIGNAL
const previousCap = process.env.CRUSH_BASH_AUTO_BACKGROUND_MS
process.env.HOME = scratch
process.env.CRUSH_BG_SIGNAL = path.join(scratch, "signal")
process.env.CRUSH_BASH_AUTO_BACKGROUND_MS = String(autoBackgroundAfter)
fs.writeFileSync(process.env.CRUSH_BG_SIGNAL, "")

const schema = { optional() { return this }, describe() { return this } }
mock.module("@opencode-ai/plugin", () => ({
  tool: Object.assign((spec: unknown) => spec, {
    schema: { string: () => schema, number: () => schema },
  }),
}))
const { default: bash } = await import("./tools/bash")

afterAll(() => {
  if (previousHome === undefined) delete process.env.HOME
  else process.env.HOME = previousHome
  if (previousSignal === undefined) delete process.env.CRUSH_BG_SIGNAL
  else process.env.CRUSH_BG_SIGNAL = previousSignal
  if (previousCap === undefined) delete process.env.CRUSH_BASH_AUTO_BACKGROUND_MS
  else process.env.CRUSH_BASH_AUTO_BACKGROUND_MS = previousCap
  fs.rmSync(scratch, { recursive: true, force: true })
})

async function execute(command: string) {
  return bash.execute({ command, description: "test command" }, {
    ask: async () => {},
    metadata: () => {},
    directory: scratch,
    abort: new AbortController().signal,
  } as any)
}

function stopBackground(result: unknown) {
  if (typeof result !== "string") return
  const match = result.match(/process group (\d+)/)
  if (match) {
    try { process.kill(-Number(match[1]), "SIGTERM") } catch {}
  }
}

test("a recent old Ctrl+B signal does not background a short command", async () => {
  const now = new Date()
  fs.utimesSync(process.env.CRUSH_BG_SIGNAL!, now, now)
  const result = await execute("sleep 0.25; echo done")
  stopBackground(result)
  expect(result).toMatchObject({ output: "done", metadata: { exit: 0 } })
})

test("a wait under the cap stays in the foreground", async () => {
  const result = await execute("sleep 0.2; echo done")
  stopBackground(result)
  expect(result).toMatchObject({ output: "done", metadata: { exit: 0 } })
}, 5_000)

test("a longer wait automatically backgrounds after the cap", async () => {
  const started = Date.now()
  const result = await execute("sleep 30; echo done")
  stopBackground(result)
  expect(Date.now() - started).toBeGreaterThan(autoBackgroundAfter)
  expect(result).toContain("process group")
}, 5_000)

test("an explicit Ctrl+B still backgrounds immediately", async () => {
  const started = Date.now()
  const timer = setTimeout(() => {
    const now = new Date()
    fs.utimesSync(process.env.CRUSH_BG_SIGNAL!, now, now)
  }, 100)
  const result = await execute("sleep 30")
  clearTimeout(timer)
  stopBackground(result)
  expect(result).toContain("process group")
  // Before the cap, so it was the signal and not the automatic move.
  expect(Date.now() - started).toBeLessThan(autoBackgroundAfter)
}, 5_000)
