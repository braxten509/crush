// The viewer's red pen (owner, 2026-11-01): lines drawn on a picture that is
// attached but not sent yet. A line is a list of points in the picture's own
// pixels, so it sits in the same place at Fit and at Actual size.
//
//   PEN                       the pen's colour
//   penWidth(w, h)            a line's width in picture pixels for a w × h picture
//   drawLines(ctx, lines, w)  draws the lines on a canvas of the picture's size
//   bakeMarks(src, lines)     the picture with its lines, as {mime, base64, url}

export const PEN = "#e5322d";

export function penWidth(width, height) {
  return Math.max(3, Math.round(Math.max(width, height) / 220));
}

export function drawLines(context, lines, width) {
  context.strokeStyle = PEN;
  context.fillStyle = PEN;
  context.lineWidth = width;
  context.lineCap = "round";
  context.lineJoin = "round";
  for (const line of lines) {
    if (line.length === 1) {
      context.beginPath();
      context.arc(line[0].x, line[0].y, width / 2, 0, Math.PI * 2);
      context.fill();
      continue;
    }
    context.beginPath();
    context.moveTo(line[0].x, line[0].y);
    for (const point of line.slice(1)) context.lineTo(point.x, point.y);
    context.stroke();
  }
}

// PNG, JPEG and WebP keep their kind; anything else becomes a PNG.
export async function bakeMarks(src, lines) {
  const picture = new Image();
  picture.src = src;
  await picture.decode();
  const canvas = document.createElement("canvas");
  canvas.width = picture.naturalWidth;
  canvas.height = picture.naturalHeight;
  const context = canvas.getContext("2d");
  context.drawImage(picture, 0, 0);
  drawLines(context, lines, penWidth(canvas.width, canvas.height));
  const kind = /^data:(image\/(?:png|jpeg|webp))[;,]/iu.exec(src)?.[1]?.toLowerCase() ?? "image/png";
  const url = canvas.toDataURL(kind, 0.92);
  return { mime: kind, base64: url.slice(url.indexOf(",") + 1), url };
}
