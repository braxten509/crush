// The help footer's one line (desktop-design-5-6 2.1, DLG-009): a port of the
// terminal's ShortHelpLine (internal/ui/dialog/internal/kit/help.go:26-65),
// with drawn widths instead of terminal cells. Pairs are "key description",
// joined by " · ". Disabled pairs are left out. Pairs that do not fit are
// dropped from the end and replaced by "…" when it fits, so the line never
// wraps and never ends on a separator; the ellipsis takes a space before it
// only when a pair is shown.

export const HELP_SEPARATOR = " · ";
export const HELP_ELLIPSIS = "…";

// pairs: [{key, text, disabled?}]; width: the line's room; sizes: {pair(p) →
// the drawn width of "key text", separator, space, ellipsis} in the same unit.
// → {shown: the pairs drawn, in order, ellipsis: whether "…" follows them}
export function fitHelp(pairs, width, sizes) {
  const shown = [];
  if (!(width > 0)) return { shown, ellipsis: false };
  let total = 0;
  for (const pair of pairs ?? []) {
    if (pair.disabled) continue;
    const segment = (total > 0 ? sizes.separator : 0) + sizes.pair(pair);
    if (total + segment > width) {
      // The separator belongs to the dropped pair, so the line ends on a hint.
      const tail = total > 0 ? sizes.space + sizes.ellipsis : sizes.ellipsis;
      return { shown, ellipsis: total + tail <= width };
    }
    total += segment;
    shown.push(pair);
  }
  return { shown, ellipsis: false };
}

// The line as text, the way the terminal prints it (for the probe and tests).
export function helpText({ shown, ellipsis }) {
  const line = shown.map((pair) => `${pair.key} ${pair.text}`).join(HELP_SEPARATOR);
  if (!ellipsis) return line;
  return line ? `${line} ${HELP_ELLIPSIS}` : HELP_ELLIPSIS;
}
