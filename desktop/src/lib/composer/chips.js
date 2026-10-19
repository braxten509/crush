// The chip row as pure rules (TYPE-035 to TYPE-038, SIDE-026): what each chip
// shows and how many fit. A port of internal/ui/attachments/attachments.go.

// The chip of an attachment (lib/types.js ChipView). The icon follows the
// terminal (attachments.go:266-274): pictures, Markdown (a skill's content),
// then text for everything else.
export function attachmentChip(attachment) {
  const mime = attachment?.mime ?? "";
  const picture = mime.startsWith("image/");
  return {
    kind: picture ? "picture" : mime === "text/markdown" ? "skill" : "text",
    name: attachment?.name ?? "",
    mime,
    viewable: picture && Boolean(attachment?.base64),
    src: picture && attachment?.base64 ? `data:${mime};base64,${attachment.base64}` : undefined,
  };
}

// A kept skill's chip (SIDE-026): Chip draws "SKILL" before the name.
export function skillChip(skill) {
  return { kind: "skill", name: skill?.name || skill?.id || "", mime: "", viewable: false };
}

// How many chips fit in one row (Render, attachments.go:160-232). widths:
// each chip's width with its name cut to 15 characters; moreWidth(n): the
// width of "n more…"; gap: the space between items; fixed: a chip's width
// without its name; minName: the width of 5 characters. Returns
//   {shown, more, firstName}: the first `shown` chips, then "more more…" when
//   more > 0; firstName, when not null, is the room left for the first chip's
//   name (it alone did not fit).
export function fitChips({ widths, moreWidth, available, gap, fixed, minName }) {
  const count = widths.length;
  let offset = 0;
  for (let i = 0; i < count; i += 1) {
    const hidden = count - i;
    const hint = hidden > 1 ? gap + moreWidth(hidden - 1) : 0;
    const width = widths[i] + (i > 0 ? gap : 0);
    if (offset + width + hint > available) {
      if (i > 0) return { shown: i, more: hidden, firstName: null };
      const room = available - fixed - hint;
      if (hidden > 1 && room >= minName) return { shown: 1, more: hidden - 1, firstName: room };
      return { shown: 1, more: 0, firstName: Math.max(0, available - fixed) };
    }
    offset += width;
  }
  return { shown: count, more: 0, firstName: null };
}

export const moreText = (n) => `${n} more…`;

// Delete mode (Update, attachments.go:60-90): Ctrl+R enters it only when there
// are chips but is always taken; then Esc leaves, "r" removes them all, a digit
// removes that chip, and every other key is eaten.
//   → {enter} | {leave} | {clear} | {remove: index} | {eat} | null (not taken)
export function deleteModeKey(combo, { deleting, count }) {
  if (combo === "Ctrl+R") return count > 0 ? { enter: true } : { eat: true };
  if (!deleting) return null;
  if (combo === "Escape") return { leave: true };
  if (combo === "r") return { clear: true };
  if (/^[0-9]$/.test(combo)) return { remove: Number(combo) };
  return { eat: true };
}
