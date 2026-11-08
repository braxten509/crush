// The viewer's red pen and text (owner, 2026-11-01, text 2026-11-08): marks
// drawn on a picture that is attached but not sent yet, or on a video frame.
// A mark is either a line (a list of points) or a note ({ text, x, y }), in
// the picture's own pixels, so it sits in the same place at every size. A
// note is white with a black shadow so it reads on any background; (x, y) is
// the left end of its middle line.
//
//   PEN                       the pen's colour
//   NOTE_FONT                 the notes' font family (the typing box uses it too)
//   penWidth(w, h)            a line's width in picture pixels for a w × h picture
//   noteSize(w, h)            a note's font size in picture pixels
//   isNote(mark)              whether a mark is a note
//   drawLines(ctx, marks, w)  draws the marks on a canvas of the picture's size
//   bakeMarks(src, marks)     the picture with its marks, as {mime, base64, url}

export const PEN = "#e5322d";
export const NOTE_FONT = "system-ui, sans-serif";
const NOTE_WEIGHT = 600;

export function penWidth(width, height) {
  return Math.max(3, Math.round(Math.max(width, height) / 220));
}

export function noteSize(width, height) {
  return Math.max(14, Math.round(Math.max(width, height) / 30));
}

export function isNote(mark) {
  return Boolean(mark) && !Array.isArray(mark) && typeof mark.text === "string";
}

function drawNote(context, note, size) {
  context.save();
  context.font = `${NOTE_WEIGHT} ${size}px ${NOTE_FONT}`;
  context.textBaseline = "middle";
  context.fillStyle = "#ffffff";
  context.shadowColor = "rgba(0, 0, 0, 0.95)";
  context.shadowBlur = Math.max(2, size / 6);
  context.shadowOffsetX = 0;
  context.shadowOffsetY = Math.max(1, size / 20);
  // Twice: one shadow alone is too faint on white.
  context.fillText(note.text, note.x, note.y);
  context.fillText(note.text, note.x, note.y);
  context.restore();
}

export function drawLines(context, marks, width) {
  const size = noteSize(context.canvas.width, context.canvas.height);
  context.strokeStyle = PEN;
  context.fillStyle = PEN;
  context.lineWidth = width;
  context.lineCap = "round";
  context.lineJoin = "round";
  for (const mark of marks) {
    if (isNote(mark)) {
      drawNote(context, mark, size);
      continue;
    }
    if (!mark.length) continue;
    if (mark.length === 1) {
      context.beginPath();
      context.arc(mark[0].x, mark[0].y, width / 2, 0, Math.PI * 2);
      context.fill();
      continue;
    }
    context.beginPath();
    context.moveTo(mark[0].x, mark[0].y);
    for (const point of mark.slice(1)) context.lineTo(point.x, point.y);
    context.stroke();
  }
}

// PNG, JPEG and WebP keep their kind; anything else becomes a PNG. Given
// maxBytes, a PNG larger than that becomes a JPEG.
export async function bakeMarks(src, marks, maxBytes = Infinity) {
  const picture = new Image();
  picture.src = src;
  await picture.decode();
  const canvas = document.createElement("canvas");
  canvas.width = picture.naturalWidth;
  canvas.height = picture.naturalHeight;
  const context = canvas.getContext("2d");
  context.drawImage(picture, 0, 0);
  drawLines(context, marks, penWidth(canvas.width, canvas.height));
  let kind = /^data:(image\/(?:png|jpeg|webp))[;,]/iu.exec(src)?.[1]?.toLowerCase() ?? "image/png";
  let url = canvas.toDataURL(kind, 0.92);
  if (kind === "image/png" && (url.length * 3) / 4 > maxBytes) {
    kind = "image/jpeg";
    url = canvas.toDataURL(kind, 0.9);
  }
  return { mime: kind, base64: url.slice(url.indexOf(",") + 1), url };
}
