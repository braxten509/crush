import { describe, expect, it, vi } from "vitest";

// The window's reports reach the engine one at a time, newest first in line:
// two in flight together could land out of order and leave the engine with
// the older one (review t1, 2026-10-21).
const api = vi.hoisted(() => ({ remotePresence: vi.fn() }));
vi.mock("../engine/api.js", () => ({ api }));

const { sendPresence } = await import("./presence.js");

describe("window reports", () => {
  it("sends one at a time, and only the newest of those waiting", async () => {
    let release;
    api.remotePresence.mockReturnValueOnce(new Promise((r) => (release = r))).mockResolvedValue(undefined);
    sendPresence({ sessionId: "s1", focused: true, plan: false });
    sendPresence({ sessionId: "s2", focused: true, plan: false });
    sendPresence({ sessionId: "s3", focused: false, plan: false });
    expect(api.remotePresence).toHaveBeenCalledTimes(1);
    release();
    await vi.waitFor(() => expect(api.remotePresence).toHaveBeenCalledTimes(2));
    expect(api.remotePresence.mock.calls.map(([body]) => [body.session_id, body.focused])).toEqual([
      ["s1", true],
      ["s3", false],
    ]);
  });
});
