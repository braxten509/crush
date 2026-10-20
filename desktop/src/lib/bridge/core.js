// Tauri's page functions on Electron (owner 2026-10-20): invoke() and
// Channel, over the preload's line (electron/preload.cjs). vite.config.js
// points "@tauri-apps/api/core" here, so the app's code kept its imports.

let nextChannel = 1;

function bridge() {
  const line = globalThis.window?.crushBridge;
  if (!line) throw new Error("not inside the app window");
  return line;
}

// A stream of later messages for one call (a terminal's output, a log's new
// lines). It crosses to the helper as {"__channel": id}.
export class Channel {
  constructor(onmessage) {
    this.id = nextChannel++;
    this.onmessage = onmessage ?? (() => {});
    bridge().channel(this.id, (data) => this.onmessage(data));
  }

  toJSON() {
    return { __channel: this.id };
  }
}

// The command's answer; its error (an AppError's {code, message}, or text)
// as the rejection, the way Tauri gave it.
export async function invoke(cmd, args = {}) {
  const plain = JSON.parse(JSON.stringify(args ?? {}));
  const reply = await bridge().invoke(cmd, plain);
  if (reply && "err" in reply) throw reply.err;
  return reply?.ok ?? null;
}
