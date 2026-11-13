// Crush's stand-in for OpenCode's bash tool. It runs commands the same way
// but can hand a running one to the background when the user presses Ctrl+B
// in Crush, which touches the file in CRUSH_BG_SIGNAL. The command keeps
// running and writes to a log the model can read later.
import { tool } from "@opencode-ai/plugin"
import { spawn } from "node:child_process"
import * as fs from "node:fs"
import * as os from "node:os"
import * as path from "node:path"

const signalFile = process.env.CRUSH_BG_SIGNAL ?? ""
const logDir = path.join(os.homedir(), ".cache", "crush", "background")
const maxOutput = 30_000
// How long a command may hold the turn before it continues in the
// background: the user's setting (options.auto_background_seconds), which
// Crush passes in CRUSH_BASH_AUTO_BACKGROUND_MS; 2 minutes without it. Tests
// shorten it the same way; the check runs often enough to notice a short one.
const autoBackgroundAfter = Number(process.env.CRUSH_BASH_AUTO_BACKGROUND_MS) || 2 * 60 * 1000
// A command with no timeout of its own is not stopped before that time.
const defaultTimeout = Math.max(2 * 60 * 1000, autoBackgroundAfter)
const checkEvery = Math.min(200, autoBackgroundAfter / 4)

// "2 minutes", "90 seconds": a time as the model reads it.
function span(ms: number) {
  const seconds = Math.round(ms / 1000)
  if (seconds % 60 === 0) return seconds === 60 ? "1 minute" : `${seconds / 60} minutes`
  return seconds === 1 ? "1 second" : `${seconds} seconds`
}

function backgroundSignal() {
  try {
    return signalFile ? fs.statSync(signalFile).mtimeMs : 0
  } catch {
    return 0
  }
}

export default tool({
  description: `Executes a given bash command with an optional timeout.

All commands run in the current working directory by default. Use the \`workdir\` parameter to run a command in a different directory instead of \`cd <directory> && <command>\`.

- Always quote file paths that contain spaces.
- Don't use this for reading, searching or editing files when a dedicated tool can do it.
- Output longer than ${maxOutput} characters is cut to its end.
- The default timeout is ${span(defaultTimeout)}; pass \`timeout\` (milliseconds) to change it, or run long-lived processes in the background (e.g. \`cmd > log 2>&1 &\`).
- The user can move a running command to the background. Its result then says so and names a log file; read that file later to check on it.
- Keep waits of 10 seconds or less in the foreground. Commands still running after ${span(autoBackgroundAfter)} automatically continue in the background, with output saved to a log.
- For git commits and PRs, write multi-line messages with a HEREDOC.`,
  args: {
    command: tool.schema.string().describe("The command to execute"),
    timeout: tool.schema.number().optional().describe("Optional timeout in milliseconds"),
    workdir: tool.schema
      .string()
      .optional()
      .describe("The working directory to run the command in. Defaults to the current directory. Use this instead of 'cd' commands."),
    description: tool.schema
      .string()
      .describe("Clear, concise description of what this command does in 5-10 words."),
  },
  async execute(args, ctx) {
    const initialSignal = backgroundSignal()
    await ctx.ask({
      permission: "bash",
      patterns: [args.command],
      always: [args.command.trim().split(/\s+/)[0] + " *"],
      metadata: {},
    })
    ctx.metadata({ title: args.description })

    fs.mkdirSync(logDir, { recursive: true })
    const log = path.join(logDir, `${Date.now()}-${Math.random().toString(36).slice(2, 8)}.log`)
    const fd = fs.openSync(log, "a")
    const cwd = args.workdir ? path.resolve(ctx.directory, args.workdir) : ctx.directory
    const started = Date.now()
    // Its own process group, so it can be stopped as a whole and outlives
    // OpenCode once backgrounded. The exit code lands at the end of the log.
    const child = spawn(
      "bash",
      ["-c", 'bash -c "$1" </dev/null; code=$?; printf "\\n[exit code: %s]\\n" "$code"; exit $code', "bash", args.command],
      { cwd, detached: true, stdio: ["ignore", fd, fd] },
    )
    fs.closeSync(fd)

    const outcome = await new Promise<"exit" | "background" | "auto-background" | "timeout" | "abort">((resolve) => {
      const limit = args.timeout ?? defaultTimeout
      const done = (r: "exit" | "background" | "auto-background" | "timeout" | "abort") => {
        clearInterval(tick)
        ctx.abort.removeEventListener("abort", onAbort)
        resolve(r)
      }
      const onAbort = () => done("abort")
      const tick = setInterval(() => {
        if (backgroundSignal() > initialSignal) return done("background")
        const elapsed = Date.now() - started
        // A timeout shorter than the background time stops the command;
        // past the background time it carries on in the background.
        if (elapsed > autoBackgroundAfter) return done("auto-background")
        if (elapsed > limit) done("timeout")
      }, checkEvery)
      ctx.abort.addEventListener("abort", onAbort)
      child.on("exit", () => done("exit"))
    })

    const stop = () => {
      try {
        process.kill(-child.pid!, "SIGTERM")
      } catch {}
      setTimeout(() => {
        try {
          process.kill(-child.pid!, "SIGKILL")
        } catch {}
      }, 3000).unref()
    }
    let output = fs.readFileSync(log, "utf8")
    const cut = (s: string) => (s.length > maxOutput ? "…(earlier output cut)\n" + s.slice(-maxOutput) : s)

    switch (outcome) {
      case "background":
      case "auto-background":
        child.unref()
        const reason = outcome === "background"
          ? "The user moved this command to the background (Ctrl+B)."
          : `This command ran for more than ${span(autoBackgroundAfter)} and continues in the background.`
        return `${cut(output)}\n\n[${reason} It is still running as process group ${child.pid}. Its output continues in ${log}, which ends with an "[exit code: N]" line when it finishes. Don't wait for it now; read that file later to check on it.]`
      case "timeout":
        stop()
        return `${cut(output)}\n\n[The command timed out after ${args.timeout ?? defaultTimeout} ms and was stopped.]`
      case "abort":
        stop()
        return `${cut(output)}\n\n[The command was stopped.]`
    }
    fs.rmSync(log, { force: true })
    const m = output.match(/\n\[exit code: (\d+)\]\n$/)
    const code = m ? Number(m[1]) : child.exitCode
    if (m) output = output.slice(0, m.index)
    output = cut(output.trim())
    return {
      title: args.description,
      output: code === 0 ? output : `${output}\n\nExit code: ${code}`,
      metadata: { exit: code, description: args.description },
    }
  },
})
