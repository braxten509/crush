// START-021: `crush-desktop -s` takes a whole chat ID, a whole hash or the
// start of a hash, as the terminal does (resolveWorkspaceSessionID,
// internal/cmd/root.go:295-315, and sessionsMatchingHash,
// internal/cmd/session.go:218-226). A chat's hash is session.HashID
// (internal/session/session.go:29-33): XXH3-64 of the ID's UTF-8 bytes with
// no seed, as 16 lowercase hex digits. XXH3 is ported from the library the
// terminal uses, github.com/zeebo/xxh3 v1.1.0 (hash64.go, accum_generic.go,
// utils.go, consts.go), with BigInt for the 64-bit sums and products.

const M64 = (1n << 64n) - 1n;
const STRIPE = 64;
const BLOCK = 1024;

const PRIME32_1 = 2654435761n;
const PRIME32_2 = 2246822519n;
const PRIME32_3 = 3266489917n;
const PRIME64_1 = 11400714785074694791n;
const PRIME64_2 = 14029467366897019727n;
const PRIME64_3 = 1609587929392839161n;
const PRIME64_4 = 9650029242287828579n;
const PRIME64_5 = 2870177450012600261n;

// The default secret (consts.go:18-31).
const KEY = new DataView(
  new Uint8Array([
    0xb8, 0xfe, 0x6c, 0x39, 0x23, 0xa4, 0x4b, 0xbe, 0x7c, 0x01, 0x81, 0x2c, 0xf7, 0x21, 0xad, 0x1c,
    0xde, 0xd4, 0x6d, 0xe9, 0x83, 0x90, 0x97, 0xdb, 0x72, 0x40, 0xa4, 0xa4, 0xb7, 0xb3, 0x67, 0x1f,
    0xcb, 0x79, 0xe6, 0x4e, 0xcc, 0xc0, 0xe5, 0x78, 0x82, 0x5a, 0xd0, 0x7d, 0xcc, 0xff, 0x72, 0x21,
    0xb8, 0x08, 0x46, 0x74, 0xf7, 0x43, 0x24, 0x8e, 0xe0, 0x35, 0x90, 0xe6, 0x81, 0x3a, 0x26, 0x4c,
    0x3c, 0x28, 0x52, 0xbb, 0x91, 0xc3, 0x00, 0xcb, 0x88, 0xd0, 0x65, 0x8b, 0x1b, 0x53, 0x2e, 0xa3,
    0x71, 0x64, 0x48, 0x97, 0xa2, 0x0d, 0xf9, 0x4e, 0x38, 0x19, 0xef, 0x46, 0xa9, 0xde, 0xac, 0xd8,
    0xa8, 0xfa, 0x76, 0x3f, 0xe3, 0x9c, 0x34, 0x3f, 0xf9, 0xdc, 0xbb, 0xc7, 0xc7, 0x0b, 0x4f, 0x1d,
    0x8a, 0x51, 0xe0, 0x4b, 0xcd, 0xb4, 0x59, 0x31, 0xc8, 0x9f, 0x7e, 0xc9, 0xd9, 0x78, 0x73, 0x64,
    0xea, 0xc5, 0xac, 0x83, 0x34, 0xd3, 0xeb, 0xc3, 0xc5, 0x81, 0xa0, 0xff, 0xfa, 0x13, 0x63, 0xeb,
    0x17, 0x0d, 0xdd, 0x51, 0xb7, 0xf0, 0xda, 0x49, 0xd3, 0x16, 0x55, 0x26, 0x29, 0xd4, 0x68, 0x9e,
    0x2b, 0x16, 0xbe, 0x58, 0x7d, 0x47, 0xa1, 0xfc, 0x8f, 0xf8, 0xb8, 0xd1, 0x7a, 0xd0, 0x31, 0xce,
    0x45, 0xcb, 0x3a, 0x8f, 0x95, 0x16, 0x04, 0x28, 0xaf, 0xd7, 0xfb, 0xca, 0xbb, 0x4b, 0x40, 0x7e,
  ]).buffer,
);

const key64 = (offset) => KEY.getBigUint64(offset, true);
const key32 = (offset) => BigInt(KEY.getUint32(offset, true));

function mul(a, b) {
  return (a * b) & M64;
}

function add(a, b) {
  return (a + b) & M64;
}

// The 128-bit product, its high and low halves xored (mulFold64, utils.go:126-129).
function mulFold(a, b) {
  const product = a * b;
  return ((product >> 64n) ^ product) & M64;
}

function rotl(x, r) {
  return ((x << r) | (x >> (64n - r))) & M64;
}

function swap64(x) {
  let out = 0n;
  for (let i = 0n; i < 8n; i += 1n) out = (out << 8n) | ((x >> (8n * i)) & 0xffn);
  return out;
}

function avalancheXXH64(x) {
  x ^= x >> 33n;
  x = mul(x, PRIME64_2);
  x ^= x >> 29n;
  x = mul(x, PRIME64_3);
  x ^= x >> 32n;
  return x;
}

function avalancheXXH3(x) {
  x ^= x >> 37n;
  x = mul(x, 0x165667919e3779f9n);
  x ^= x >> 32n;
  return x;
}

function rrmxmx(h, length) {
  h ^= rotl(h, 49n) ^ rotl(h, 24n);
  h = mul(h, 0x9fb21c651e98df25n);
  h ^= add(h >> 35n, length);
  h = mul(h, 0x9fb21c651e98df25n);
  h ^= h >> 28n;
  return h;
}

// One 64-byte stripe into the eight accumulators (accum_generic.go:15-76).
function accumulateStripe(accs, data, at, keyAt) {
  for (let lane = 0; lane < 8; lane += 2) {
    const v0 = data.getBigUint64(at + 8 * lane, true);
    const k0 = v0 ^ key64(keyAt + 8 * lane);
    const v1 = data.getBigUint64(at + 8 * lane + 8, true);
    const k1 = v1 ^ key64(keyAt + 8 * lane + 8);
    accs[lane] = add(accs[lane], add((k0 & 0xffffffffn) * (k0 >> 32n), v1));
    accs[lane + 1] = add(accs[lane + 1], add((k1 & 0xffffffffn) * (k1 >> 32n), v0));
  }
}

function scramble(accs) {
  for (let i = 0; i < 8; i += 1) {
    let acc = accs[i];
    acc ^= acc >> 47n;
    acc ^= key64(128 + 8 * i);
    accs[i] = mul(acc, PRIME32_1);
  }
}

// Inputs over 240 bytes (hashAny's default case and accumScalar).
function hashLong(data, length) {
  const accs = [PRIME32_3, PRIME64_1, PRIME64_2, PRIME64_3, PRIME64_4, PRIME32_2, PRIME64_5, PRIME32_1];
  let at = 0;
  let left = length;
  while (left > BLOCK) {
    for (let i = 0; i < 16; i += 1) {
      accumulateStripe(accs, data, at, 8 * i);
      at += STRIPE;
      left -= STRIPE;
    }
    scramble(accs);
  }
  const stripes = Math.floor((left - 1) / STRIPE);
  for (let i = 0; i < stripes; i += 1) {
    accumulateStripe(accs, data, at, 8 * i);
    at += STRIPE;
    left -= STRIPE;
  }
  // The last stripe is the input's last 64 bytes, against the secret at 121.
  accumulateStripe(accs, data, length - STRIPE, 121);
  let acc = mul(BigInt(length), PRIME64_1);
  for (let i = 0; i < 8; i += 2) {
    acc = add(acc, mulFold(accs[i] ^ key64(11 + 8 * i), accs[i + 1] ^ key64(19 + 8 * i)));
  }
  return avalancheXXH3(acc);
}

// XXH3-64 with no seed, of a Uint8Array; a BigInt.
export function xxh3(bytes) {
  const data = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const l = bytes.byteLength;
  const length = BigInt(l);
  const u64 = (offset) => data.getBigUint64(offset, true);
  if (l <= 16) {
    if (l > 8) {
      const lo = u64(0) ^ (key64(24) ^ key64(32));
      const hi = u64(l - 8) ^ (key64(40) ^ key64(48));
      return avalancheXXH3(add(add(add(length, swap64(lo)), hi), mulFold(lo, hi)));
    }
    if (l > 3) {
      const input1 = BigInt(data.getUint32(0, true));
      const input2 = BigInt(data.getUint32(l - 4, true));
      const input64 = add(input2, input1 << 32n);
      return rrmxmx(input64 ^ (key64(8) ^ key64(16)), length);
    }
    if (l === 0) return 0x2d06800538d394c2n;
    const c1 = BigInt(bytes[0]);
    const c2 = BigInt(bytes[l >> 1]);
    const c3 = BigInt(bytes[l - 1]);
    const combined = (c1 << 16n) | (c2 << 24n) | c3 | (length << 8n);
    return avalancheXXH64(combined ^ (key32(0) ^ key32(4)));
  }
  if (l <= 128) {
    let acc = mul(length, PRIME64_1);
    const pair = (a, b, k) => mulFold(u64(a) ^ key64(k), u64(b) ^ key64(k + 8));
    if (l > 32) {
      if (l > 64) {
        if (l > 96) {
          acc = add(acc, pair(48, 56, 96));
          acc = add(acc, pair(l - 64, l - 56, 112));
        }
        acc = add(acc, pair(32, 40, 64));
        acc = add(acc, pair(l - 48, l - 40, 80));
      }
      acc = add(acc, pair(16, 24, 32));
      acc = add(acc, pair(l - 32, l - 24, 48));
    }
    acc = add(acc, pair(0, 8, 0));
    acc = add(acc, pair(l - 16, l - 8, 16));
    return avalancheXXH3(acc);
  }
  if (l <= 240) {
    let acc = mul(length, PRIME64_1);
    for (let i = 0; i < 8; i += 1) {
      acc = add(acc, mulFold(u64(16 * i) ^ key64(16 * i), u64(16 * i + 8) ^ key64(16 * i + 8)));
    }
    acc = avalancheXXH3(acc);
    const top = l & ~15;
    for (let i = 128; i < top; i += 16) {
      acc = add(acc, mulFold(u64(i) ^ key64(i - 125), u64(i + 8) ^ key64(i - 117)));
    }
    acc = add(acc, mulFold(u64(l - 16) ^ key64(119), u64(l - 8) ^ key64(127)));
    return avalancheXXH3(acc);
  }
  return hashLong(data, l);
}

const encoder = new TextEncoder();

// session.HashID: 16 lowercase hex digits.
export function hashId(id) {
  return xxh3(encoder.encode(String(id))).toString(16).padStart(16, "0");
}

// The ids whose hash starts with prefix, in the order given (sessionsMatchingHash).
export function idsMatchingHash(ids, prefix) {
  return ids.filter((id) => hashId(id).startsWith(prefix));
}

export function notFoundText(id) {
  return `session not found: ${id}`;
}

export function ambiguousText(id, count) {
  return `session ID ${goQuote(id)} is ambiguous (${count} matches)`;
}

// The chat a launch id names: {id} or {error} with the terminal's text. A
// whole id is asked for first (getSession, any failure falls through, as
// root.go:296-298), then the hashes of the listed chats (ids()).
export async function resolveSessionId(id, { getSession, ids }) {
  try {
    const found = await getSession(id);
    if (found?.id) return { id: found.id };
  } catch {
    // Not a whole id: try the hashes.
  }
  const matches = idsMatchingHash(ids(), id);
  if (matches.length === 1) return { id: matches[0] };
  if (matches.length === 0) return { error: notFoundText(id) };
  return { error: ambiguousText(id, matches.length) };
}

// Go's %q (strconv.Quote): a double-quoted literal with Go escapes; printable
// characters (letters, marks, numbers, punctuation, symbols, the ASCII space)
// stay as they are.
const PRINTABLE = /^[\p{L}\p{M}\p{N}\p{P}\p{S} ]$/u;
const NAMED = { "\x07": "\\a", "\b": "\\b", "\f": "\\f", "\n": "\\n", "\r": "\\r", "\t": "\\t", "\v": "\\v" };

export function goQuote(text) {
  let out = '"';
  for (const ch of String(text)) {
    const code = ch.codePointAt(0);
    if (ch === '"' || ch === "\\") out += "\\" + ch;
    else if (PRINTABLE.test(ch)) out += ch;
    else if (NAMED[ch]) out += NAMED[ch];
    else if (code < 0x20 || code === 0x7f) out += "\\x" + code.toString(16).padStart(2, "0");
    else if (code >= 0xd800 && code <= 0xdfff) out += "\\ufffd";
    else if (code < 0x10000) out += "\\u" + code.toString(16).padStart(4, "0");
    else out += "\\U" + code.toString(16).padStart(8, "0");
  }
  return out + '"';
}
