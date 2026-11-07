export const VIDEO_MIME = "video/mp4";

export function isVideo(attachment) {
  return attachment?.kind === "video";
}

export function clockTime(seconds) {
  const total = Math.max(0, Number(seconds) || 0);
  const minutes = Math.floor(total / 60);
  const rest = total - minutes * 60;
  return `${minutes}:${rest.toFixed(2).padStart(5, "0")}`;
}

export function videoAttachment(video, existing) {
  let most = 0;
  for (const a of existing) most = Math.max(most, Number(/^phone_video_(\d+)\.mp4$/u.exec(a?.name ?? "")?.[1] ?? 0));
  const name = `phone_video_${most + 1}.mp4`;
  return {
    kind: "video",
    name,
    mime: VIDEO_MIME,
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

export function frameName(attachment, frame) {
  return `${attachment.name.replace(/\.mp4$/u, "")}_frame_${frame + 1}.png`;
}

export function expandRecordings(text, attachments) {
  const videos = attachments.filter(isVideo);
  if (videos.length === 0) return { text, attachments };
  const out = [];
  const lines = [];
  for (const attachment of attachments) {
    if (!isVideo(attachment)) {
      out.push(attachment);
      continue;
    }
    const { width, height, duration, frames } = attachment.video;
    const facts = [width && height ? `${width}×${height}` : "", duration ? `${duration.toFixed(1)} s` : "", frames.length ? `${frames.length} frames` : ""].filter(Boolean).join(", ");
    lines.push(`Phone screen recording ${attachment.name}: ${attachment.path}${facts ? ` (${facts})` : ""}`);
    const marks = [...attachment.marks].sort((a, b) => a.frame - b.frame);
    for (const mark of marks) {
      const name = frameName(attachment, mark.frame);
      lines.push(`- Marked frame ${mark.frame + 1} at ${clockTime(mark.time)}, attached as ${name}`);
      out.push({ kind: "picture", name, mime: mark.mime, path: name, base64: mark.base64 });
    }
  }
  const note = lines.join("\n");
  return { text: text ? `${text}\n\n${note}` : note, attachments: out };
}
