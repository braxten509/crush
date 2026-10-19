// What the Sessions and Open Project sheets show (desktop-design-5-6 2.5):
// each saved chat's info text "~/short/folder · 3 hours ago"
// (internal/ui/dialog/sessions/item.go:82-89), the relative time as
// go-humanize's Time words it, and whether a row belongs to this window's
// project.
import { clean, short } from "../leftlist/picker.js";

const SECOND = 1;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const MONTH = 30 * DAY;
const YEAR = 12 * MONTH;
const LONG_TIME = 37 * YEAR;

// go-humanize's defaultMagnitudes (times.go): below `under` seconds, the
// text, with %d the difference divided by `by`.
const MAGNITUDES = [
  { under: SECOND, text: "now", by: 1 },
  { under: 2 * SECOND, text: "1 second %s", by: 1 },
  { under: MINUTE, text: "%d seconds %s", by: SECOND },
  { under: 2 * MINUTE, text: "1 minute %s", by: 1 },
  { under: HOUR, text: "%d minutes %s", by: MINUTE },
  { under: 2 * HOUR, text: "1 hour %s", by: 1 },
  { under: DAY, text: "%d hours %s", by: HOUR },
  { under: 2 * DAY, text: "1 day %s", by: 1 },
  { under: WEEK, text: "%d days %s", by: DAY },
  { under: 2 * WEEK, text: "1 week %s", by: 1 },
  { under: MONTH, text: "%d weeks %s", by: WEEK },
  { under: 2 * MONTH, text: "1 month %s", by: 1 },
  { under: YEAR, text: "%d months %s", by: MONTH },
  { under: 18 * MONTH, text: "1 year %s", by: 1 },
  { under: 2 * YEAR, text: "2 years %s", by: 1 },
  { under: LONG_TIME, text: "%d years %s", by: YEAR },
  { under: Infinity, text: "a long while %s", by: 1 },
];

// humanize.Time(then) for unix seconds `then`, as of `now` (ms).
export function humanizeTime(then, now = Date.now()) {
  const nowSeconds = Math.floor(now / 1000);
  const diff = Math.abs(nowSeconds - then);
  const label = then <= nowSeconds ? "ago" : "from now";
  const magnitude = MAGNITUDES.find((m) => diff < m.under);
  return magnitude.text.replace("%d", String(Math.floor(diff / magnitude.by))).replace("%s", label);
}

// The row's info text (item.go:82-89).
export function infoText(row, home, now = Date.now()) {
  const when = humanizeTime(Number(row?.UpdatedAt ?? 0), now);
  return row?.Directory ? `${short(row.Directory, home)} · ${when}` : when;
}

// A saved chat of another folder opens in that folder's window (DLG-088).
export function inProject(row, projectPath) {
  if (!row?.Directory) return true;
  return clean(row.Directory) === clean(projectPath || "");
}
