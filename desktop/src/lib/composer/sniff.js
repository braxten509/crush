// Go's http.DetectContentType for what the window itself turns into an
// attachment (a long paste, a pasted picture). The terminal names every
// attachment with it (mimeOf, internal/ui/model/paste.go:27-31) and the Rust
// side reads files with the same port (src-tauri/src/files.rs:236-416), so the
// engine gets one type from every path. Go 1.27.1, net/http/internal/sniff.go:66-195.

const bytes = (s) => Uint8Array.from(s, (c) => c.charCodeAt(0));

const html = (tag) => ({ html: bytes(tag) });
const masked = (mask, pat, ct, skipWs = false) => ({ mask: bytes(mask), pat: bytes(pat), ct, skipWs });
const exact = (sig, ct) => ({ exact: bytes(sig), ct });

const EOT_MASK = "\x00".repeat(34) + "\xFF\xFF";
const EOT_PAT = "\x00".repeat(34) + "LP";

// Go's table, in Go's order: the first match wins.
const SIGNATURES = [
  ...["<!DOCTYPE HTML", "<HTML", "<HEAD", "<SCRIPT", "<IFRAME", "<H1", "<DIV", "<FONT", "<TABLE", "<A", "<STYLE",
    "<TITLE", "<B", "<BODY", "<BR", "<P", "<!--"].map(html),
  masked("\xFF\xFF\xFF\xFF\xFF", "<?xml", "text/xml; charset=utf-8", true),
  exact("%PDF-", "application/pdf"),
  exact("%!PS-Adobe-", "application/postscript"),
  masked("\xFF\xFF\x00\x00", "\xFE\xFF\x00\x00", "text/plain; charset=utf-16be"),
  masked("\xFF\xFF\x00\x00", "\xFF\xFE\x00\x00", "text/plain; charset=utf-16le"),
  masked("\xFF\xFF\xFF\x00", "\xEF\xBB\xBF\x00", "text/plain; charset=utf-8"),
  exact("\x00\x00\x01\x00", "image/x-icon"),
  exact("\x00\x00\x02\x00", "image/x-icon"),
  exact("BM", "image/bmp"),
  exact("GIF87a", "image/gif"),
  exact("GIF89a", "image/gif"),
  masked("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF\xFF\xFF", "RIFF\x00\x00\x00\x00WEBPVP", "image/webp"),
  exact("\x89PNG\x0D\x0A\x1A\x0A", "image/png"),
  exact("\xFF\xD8\xFF", "image/jpeg"),
  masked("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF", "FORM\x00\x00\x00\x00AIFF", "audio/aiff"),
  masked("\xFF\xFF\xFF", "ID3", "audio/mpeg"),
  masked("\xFF\xFF\xFF\xFF\xFF", "OggS\x00", "application/ogg"),
  masked("\xFF\xFF\xFF\xFF\xFF\xFF\xFF\xFF", "MThd\x00\x00\x00\x06", "audio/midi"),
  masked("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF", "RIFF\x00\x00\x00\x00AVI ", "video/avi"),
  masked("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF", "RIFF\x00\x00\x00\x00WAVE", "audio/wave"),
  { mp4: true },
  exact("\x1A\x45\xDF\xA3", "video/webm"),
  masked(EOT_MASK, EOT_PAT, "application/vnd.ms-fontobject"),
  exact("\x00\x01\x00\x00", "font/ttf"),
  exact("OTTO", "font/otf"),
  exact("ttcf", "font/collection"),
  exact("wOFF", "font/woff"),
  exact("wOF2", "font/woff2"),
  exact("\x1F\x8B\x08", "application/x-gzip"),
  exact("PK\x03\x04", "application/zip"),
  exact("Rar!\x1A\x07\x00", "application/x-rar-compressed"),
  exact("Rar!\x1A\x07\x01\x00", "application/x-rar-compressed"),
  exact("\x00\x61\x73\x6D", "application/wasm"),
  { text: true },
];

const startsWith = (data, sig) => data.length >= sig.length && sig.every((b, i) => data[i] === b);
const binary = (b) => b <= 0x08 || b === 0x0b || (b >= 0x0e && b <= 0x1a) || (b >= 0x1c && b <= 0x1f);

function matches(sig, data, firstNonWs) {
  if (sig.html) {
    const rest = data.subarray(firstNonWs);
    if (rest.length < sig.html.length + 1) return null;
    for (let i = 0; i < sig.html.length; i += 1) {
      const b = sig.html[i];
      const d = b >= 0x41 && b <= 0x5a ? rest[i] & 0xdf : rest[i];
      if (b !== d) return null;
    }
    const end = rest[sig.html.length];
    return end === 0x20 || end === 0x3e ? "text/html; charset=utf-8" : null;
  }
  if (sig.mask) {
    const rest = sig.skipWs ? data.subarray(firstNonWs) : data;
    if (rest.length < sig.pat.length) return null;
    return sig.pat.every((p, i) => (rest[i] & sig.mask[i]) === p) ? sig.ct : null;
  }
  if (sig.exact) return startsWith(data, sig.exact) ? sig.ct : null;
  if (sig.mp4) {
    if (data.length < 12) return null;
    const box = ((data[0] << 24) >>> 0) + (data[1] << 16) + (data[2] << 8) + data[3];
    if (data.length < box || box % 4 !== 0 || !startsWith(data.subarray(4), bytes("ftyp"))) return null;
    for (let at = 8; at < box; at += 4) {
      if (at !== 12 && data[at] === 0x6d && data[at + 1] === 0x70 && data[at + 2] === 0x34) return "video/mp4";
    }
    return null;
  }
  return data.subarray(firstNonWs).every((b) => !binary(b)) ? "text/plain; charset=utf-8" : null;
}

// The type of data (a Uint8Array): at most its first 512 bytes decide, and
// something is always named ("application/octet-stream" when nothing matches).
export function detectContentType(data) {
  const head = data.subarray(0, Math.min(512, data.length));
  let firstNonWs = head.findIndex((b) => !(b === 0x09 || b === 0x0a || b === 0x0c || b === 0x0d || b === 0x20));
  if (firstNonWs < 0) firstNonWs = head.length;
  for (const sig of SIGNATURES) {
    const found = matches(sig, head, firstNonWs);
    if (found) return found;
  }
  return "application/octet-stream";
}
