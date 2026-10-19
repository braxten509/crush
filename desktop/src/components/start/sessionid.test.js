import { describe, expect, it } from "vitest";
import { ambiguousText, goQuote, hashId, idsMatchingHash, notFoundText, resolveSessionId, xxh3 } from "../../lib/sessionid.js";

// XXH3-64 of buf[:n] with buf[j] = (j + 1) % 251, from the C reference: the
// table testVecs64 in github.com/zeebo/xxh3@v1.1.0/compat_test.go:262-1287
// (four values a line, so n's value is on line 263 + n/4). One length or more
// from every path of the hash: 0, 1-3, 4-8, 9-16, 17-128 (each of its four
// steps), 129-240, and the long path with and without whole 1,024-byte blocks.
const VECTORS = {
  0: 0x2d06800538d394c2n,
  1: 0xe12ef9d2eb86ceebn,
  2: 0x08130b77ddef5807n,
  3: 0xebce9b7632ae733bn,
  4: 0x988b7b9033ac4622n,
  5: 0x59b68387649283c1n,
  8: 0x16f217ea16232297n,
  9: 0x17d143e7f447850an,
  15: 0xdfe323395ece706fn,
  16: 0xeb5aeb9a32450f6an,
  17: 0x6d458e1fff494078n,
  36: 0xf6030e21f331c40fn,
  64: 0xc82013245d8f2587n,
  100: 0xd53e74fac84fa8fbn,
  128: 0xce22cae9106851dfn,
  129: 0x7d4fc663f5958d40n,
  200: 0xbe90b85d651fc36an,
  240: 0xa5a910b2d7e065b0n,
  241: 0xb6515f490cdd4ce5n,
  255: 0xfce9bbd7cb663ed8n,
  256: 0x6c602b93da37d220n,
  500: 0x5122fee3b84f4d1bn,
  1024: 0x546f61a5b0b850c1n,
  1025: 0xa58696e72de6df58n,
  1088: 0x3e19d2125c286a5an,
  1089: 0xe6b8f8f8df938429n,
  2048: 0x97ca16b9cb0322d1n,
  2049: 0x2472452777c6f8d3n,
  3000: 0x50860bf64d1dac3cn,
  4095: 0x268198759d7bdf74n,
};

function input(n) {
  const bytes = new Uint8Array(n);
  for (let j = 0; j < n; j += 1) bytes[j] = (j + 1) % 251;
  return bytes;
}

describe("xxh3", () => {
  for (const [n, want] of Object.entries(VECTORS)) {
    it(`matches the C reference at ${n} bytes`, () => {
      expect(xxh3(input(Number(n))).toString(16)).toBe(want.toString(16));
    });
  }

  it("reads a view into a larger buffer from its own start", () => {
    const outer = new Uint8Array(300).fill(7);
    outer.set(input(129), 50);
    expect(xxh3(outer.subarray(50, 179))).toBe(VECTORS[129]);
  });
});

describe("hashId", () => {
  it("is 16 lowercase hex digits of the ID's UTF-8 bytes, zero-padded", () => {
    // xxhsum -H3 of the same 36 bytes gives XXH3_9343b10fcdc25ef1.
    expect(hashId("a3c1e2d4-0000-4000-8000-000000000001")).toBe("9343b10fcdc25ef1");
    // Length 2's value starts with a zero digit.
    expect(hashId("\x01\x02")).toBe("08130b77ddef5807");
    expect(hashId("")).toBe("2d06800538d394c2");
    expect(hashId("é")).toBe(xxh3(new Uint8Array([0xc3, 0xa9])).toString(16).padStart(16, "0"));
  });
});

// 17 chats: at least two hashes share a first hex digit (there are 16).
const IDS = ["a3c1e2d4-0000-4000-8000-000000000001", ...Array.from({ length: 16 }, (_, i) => `chat-${i}`)];
const FIRSTS = IDS.map((id) => hashId(id)[0]);
const SHARED = FIRSTS.find((d, i) => FIRSTS.indexOf(d) !== i);
const SHARING = IDS.filter((_, i) => FIRSTS[i] === SHARED);

describe("idsMatchingHash", () => {
  it("finds the one chat whose hash starts with the prefix, or with the whole hash", () => {
    expect(idsMatchingHash(IDS, "9343b1")).toEqual(["a3c1e2d4-0000-4000-8000-000000000001"]);
    expect(idsMatchingHash(IDS, "9343b10fcdc25ef1")).toEqual(["a3c1e2d4-0000-4000-8000-000000000001"]);
  });

  it("finds every chat sharing a prefix, in the list's order", () => {
    expect(SHARING.length).toBeGreaterThan(1);
    expect(idsMatchingHash(IDS, SHARED)).toEqual(SHARING);
  });

  it("is case-sensitive and finds nothing for a prefix no hash has", () => {
    expect(idsMatchingHash(IDS, "9343B1")).toEqual([]);
    expect(idsMatchingHash(IDS, "zz")).toEqual([]);
  });
});

describe("resolveSessionId", () => {
  const ids = () => IDS;
  const missing = async () => {
    throw { code: "not_found", message: "session not found" };
  };

  it("takes a whole id the engine knows, without hashing", async () => {
    const getSession = async (id) => ({ id, title: "x" });
    expect(await resolveSessionId("whole-id", { getSession, ids: () => [] })).toEqual({ id: "whole-id" });
  });

  it("takes a hash prefix of one chat", async () => {
    expect(await resolveSessionId("9343", { getSession: missing, ids })).toEqual({ id: "a3c1e2d4-0000-4000-8000-000000000001" });
  });

  it("names a prefix shared by several chats with the terminal's text", async () => {
    expect(await resolveSessionId(SHARED, { getSession: missing, ids })).toEqual({
      error: `session ID "${SHARED}" is ambiguous (${SHARING.length} matches)`,
    });
  });

  it("says when nothing matches, with the terminal's text", async () => {
    expect(await resolveSessionId("abc", { getSession: missing, ids })).toEqual({ error: "session not found: abc" });
  });
});

describe("texts", () => {
  it("are the terminal's (root.go:309,313)", () => {
    expect(notFoundText("abc")).toBe("session not found: abc");
    expect(ambiguousText("ab", 3)).toBe('session ID "ab" is ambiguous (3 matches)');
  });

  it("quote like Go's %q", () => {
    expect(goQuote('a"b\\c')).toBe('"a\\"b\\\\c"');
    expect(goQuote("tab\there\n")).toBe('"tab\\there\\n"');
    expect(goQuote("\x01\x7f")).toBe('"\\x01\\x7f"');
    expect(goQuote("héllo ✓")).toBe('"héllo ✓"');
    expect(goQuote("a b​")).toBe('"a\\u00a0b\\u200b"');
    expect(goQuote("\u{e0001}")).toBe('"\\U000e0001"');
  });
});
