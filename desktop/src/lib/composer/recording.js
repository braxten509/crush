import { baseName } from "./paste.js";

export const VIDEO_MIME = "video/mp4";
const VIDEO_MIMES = { mp4: "video/mp4", m4v: "video/mp4", webm: "video/webm", mkv: "video/x-matroska", mov: "video/quicktime" };

export function isVideo(attachment) {
  return attachment?.kind === "video";
}

// A dropped file kept on disk (owner, 2026-11-15): the AI gets its path, not
// its bytes.
export function isFileRef(attachment) {
  return attachment?.kind === "fileref";
}

function sizeText(bytes) {
  if (!bytes) return "";
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

export function clockTime(seconds) {
  const total = Math.max(0, Number(seconds) || 0);
  const minutes = Math.floor(total / 60);
  const rest = total - minutes * 60;
  return `${minutes}:${rest.toFixed(2).padStart(5, "0")}`;
}

// A video chip: a phone recording, or with `original` a dropped video, named
// after its file (attach_copy's answer, owner 2026-11-15).
export function videoAttachment(video, existing, original = null) {
  let most = 0;
  for (const a of existing) most = Math.max(most, Number(/^phone_video_(\d+)\.mp4$/u.exec(a?.name ?? "")?.[1] ?? 0));
  const name = original ? baseName(original) : `phone_video_${most + 1}.mp4`;
  const ending = /\.(\w+)$/u.exec(name)?.[1]?.toLowerCase() ?? "";
  return {
    kind: "video",
    name,
    mime: original ? (VIDEO_MIMES[ending] ?? VIDEO_MIME) : VIDEO_MIME,
    ...(original ? { dropped: true } : {}),
    path: video.path,
    base64: "",
    url: `app://localhost/recordings/${encodeURIComponent(video.file)}`,
    video: {
      width: video.width || 0,
      height: video.height || 0,
      duration: video.duration || 0,
      bytes: video.bytes || 0,
      frames: Array.isArray(video.frames) ? video.frames : [],
    },
    marks: [],
  };
}

// A dropped file as a file chip (attach_copy's answer): the copy, or the
// original when it was too big to copy.
export function fileAttachment(copy, original, mime) {
  return { kind: "fileref", name: baseName(original), mime: mime || "application/octet-stream", path: copy.path, base64: "", bytes: copy.bytes || 0 };
}

export function frameName(attachment, frame) {
  return `${attachment.name.replace(/\.[^.]*$/u, "")}_frame_${frame + 1}.png`;
}

export function expandRecordings(text, attachments) {
  if (!attachments.some((a) => isVideo(a) || isFileRef(a))) return { text, attachments };
  const out = [];
  const lines = [];
  for (const attachment of attachments) {
    if (isFileRef(attachment)) {
      const size = sizeText(attachment.bytes);
      lines.push(`Attached file ${attachment.name}: ${attachment.path}${size ? ` (${size})` : ""}`);
      continue;
    }
    if (!isVideo(attachment)) {
      out.push(attachment);
      continue;
    }
    const { width, height, duration, frames } = attachment.video;
    const facts = [width && height ? `${width}×${height}` : "", duration ? `${duration.toFixed(1)} s` : "", frames.length ? `${frames.length} frames` : ""].filter(Boolean).join(", ");
    const what = attachment.dropped ? "Video" : "Phone screen recording";
    lines.push(`${what} ${attachment.name}: ${attachment.path}${facts ? ` (${facts})` : ""}`);
    const marks = [...attachment.marks].sort((a, b) => a.frame - b.frame);
    for (const mark of marks) {
      const name = frameName(attachment, mark.frame);
      lines.push(`- Marked frame ${mark.frame + 1} at ${clockTime(mark.time)}, attached as ${name}`);
      out.push({ kind: "picture", name, mime: mark.mime, path: name, base64: mark.base64 });
    }
  }
  const note = lines.join("\n");
  const sent = { text: text ? `${text}\n\n${note}` : note, attachments: out };
  // The draft as typed, so a send given back to the field (failed, queued
  // and recalled, or held) returns the video chip and its marks.
  ORIGINALS.delete(sent.text);
  ORIGINALS.set(sent.text, { text, attachments: [...attachments] });
  while (ORIGINALS.size > KEEP_ORIGINALS) ORIGINALS.delete(ORIGINALS.keys().next().value);
  return sent;
}

const KEEP_ORIGINALS = 20;
const ORIGINALS = new Map(); // sent text → the draft it was made from

// The draft a returned prompt was made from, or the prompt itself.
export function originalDraft(draft) {
  const original = ORIGINALS.get(draft?.text ?? "");
  if (!original) return draft;
  ORIGINALS.delete(draft.text);
  return { text: original.text, attachments: [...original.attachments] };
}
