import { afterAll, expect, mock, test } from "bun:test"
import * as fs from "node:fs"
import * as os from "node:os"
import * as path from "node:path"

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "crush-bash-test-"))
const previousHome = process.env.HOME
const previousSignal = process.env.CRUSH_BG_SIGNAL
process.env.HOME = scratch
process.env.CRUSH_BG_SIGNAL = path.join(scratch, "signal")
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
  const result = await execute("sleep 0.5; echo done")
  stopBackground(result)
  expect(result).toMatchObject({ output: "done", metadata: { exit: 0 } })
})

test("a ten-second wait stays in the foreground", async () => {
  const result = await execute("sleep 10; echo done")
  stopBackground(result)
  expect(result).toMatchObject({ output: "done", metadata: { exit: 0 } })
}, 15_000)

test("a longer wait automatically backgrounds after ten seconds", async () => {
  const started = Date.now()
  const result = await execute("sleep 30; echo done")
  stopBackground(result)
  expect(Date.now() - started).toBeGreaterThan(10_000)
  expect(result).toContain("more than 10 seconds")
  expect(result).toContain("process group")
}, 15_000)

test("an explicit Ctrl+B still backgrounds immediately", async () => {
  const timer = setTimeout(() => {
    const now = new Date()
    fs.utimesSync(process.env.CRUSH_BG_SIGNAL!, now, now)
  }, 300)
  const result = await execute("sleep 30")
  clearTimeout(timer)
  stopBackground(result)
  expect(result).toContain("Ctrl+B")
}, 5_000)
