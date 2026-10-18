// Plan markers (CHAT-033): the plan agent writes a start and a ready marker on
// lines of their own; they never show. Port of internal/ui/common/common.go:
// 21-118 rule for rule (PlanStartMarkerPresent, PlanReadyMarkerPresent,
// StripPlanMarkers), plus an offset map from the stripped text back to the
// stored text, so a selection in a plan still names stored offsets (phase 4
// branches cut M.Content().Text).
import { trimSpace } from "./strings.js";

export const PLAN_START_MARKER = "<!-- CRUSH_PLAN_START -->";
export const PLAN_READY_MARKER = "<!-- CRUSH_PLAN_READY -->";

// common.go:35 PlanStartMarkerPresent.
export function planStartPresent(text) {
  return markerPresent(text, PLAN_START_MARKER);
}

// common.go:45 PlanReadyMarkerPresent.
export function planReadyPresent(text) {
  return markerPresent(text, PLAN_READY_MARKER);
}

function markerPresent(text, marker) {
  return String(text).split("\n").some((line) => markerLine(line, marker));
}

// common.go:60 planMarkerLine: the line holds only the marker, optionally in
// inline-code backticks and white space.
function markerLine(line, marker) {
  return trimSpace(trimBackticks(trimSpace(line))) === marker;
}

function trimBackticks(s) {
  let start = 0;
  let end = s.length;
  while (start < end && s[start] === "`") start++;
  while (end > start && s[end - 1] === "`") end--;
  return s.slice(start, end);
}

// common.go:112 isCodeFenceLine.
function isCodeFenceLine(line) {
  const trimmed = trimSpace(line);
  if (!trimmed.startsWith("```")) return false;
  let i = 3;
  while (i < trimmed.length && trimmed[i] === "`") i++;
  const info = trimmed.slice(i);
  return !info.includes("`") && !info.includes(" ");
}

// common.go:79 StripPlanMarkers, as a plain string.
export function stripPlanMarkers(text) {
  return stripPlanMarkersMapped(text).text;
}

// common.go:71 StripPlanReadyMarker: the ready marker only.
export function stripPlanReadyMarker(text) {
  const input = String(text);
  return mapped(input, stripMarker(lines(input), PLAN_READY_MARKER)).text;
}

// StripPlanMarkers with a map: {text, toSource(i)}. toSource takes an offset
// in the stripped text (0..text.length) and gives the offset of the same
// character in the input; the end maps to the end.
export function stripPlanMarkersMapped(text) {
  const input = String(text);
  const first = stripMarker(lines(input), PLAN_START_MARKER);
  const second = stripMarker(first, PLAN_READY_MARKER);
  return mapped(input, second);
}

// The input as lines, each with the offset where it starts.
function lines(text) {
  const out = [];
  let start = 0;
  for (;;) {
    const nl = text.indexOf("\n", start);
    if (nl < 0) {
      out.push({ text: text.slice(start), at: start });
      return out;
    }
    out.push({ text: text.slice(start, nl), at: start });
    start = nl + 1;
  }
}

// common.go:83 stripPlanMarker over lines that remember their offsets.
function stripMarker(input, marker) {
  const kept = [];
  let inFence = false;
  for (let i = 0; i < input.length; i++) {
    if (markerLine(input[i].text, marker)) {
      // A marker fenced on the lines directly before and after loses its
      // fence too; inFence keeps a real code block's closing fence above it.
      if (inFence && kept.length > 0 && isCodeFenceLine(kept[kept.length - 1].text) && i + 1 < input.length && isCodeFenceLine(input[i + 1].text)) {
        kept.pop();
        i++;
        inFence = false;
      }
      continue;
    }
    if (isCodeFenceLine(input[i].text)) inFence = !inFence;
    kept.push(input[i]);
  }
  return kept;
}

function mapped(input, kept) {
  const text = kept.map((line) => line.text).join("\n");
  if (text.length === input.length) return { text, toSource: (i) => i };
  // Where each kept line starts in the output, for a binary search.
  const starts = [];
  let at = 0;
  for (const line of kept) {
    starts.push(at);
    at += line.text.length + 1;
  }
  function toSource(i) {
    if (kept.length === 0) return i <= 0 ? 0 : input.length;
    if (i >= text.length) {
      const last = kept[kept.length - 1];
      return last.at + last.text.length + (i - text.length);
    }
    let lo = 0;
    let hi = starts.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (starts[mid] <= i) lo = mid;
      else hi = mid - 1;
    }
    return kept[lo].at + (i - starts[lo]);
  }
  return { text, toSource };
}
