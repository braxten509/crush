// The thought peek after "Thinking" (CHAT-022): the newest line of the
// model's thinking, never the thinking itself (CHAT-023, CHAT-024). A port of
// internal/ui/chat/thought_line.go:30-55 and the message helpers it reads
// (internal/message/content.go:220-240, 339-342).
import { stripAnsi } from "./render/ansi.js";

const partsOf = (m, type) => (m?.parts ?? []).filter((p) => p?.type === type);
const finished = (m) => (m?.parts ?? []).some((p) => p?.type === "finish");
const textOf = (m) => partsOf(m, "text")[0]?.data?.text ?? "";

// Message.ReasoningContent: the reasoning parts joined, the first block's
// start, and a finish time only when every block finished.
export function reasoningOf(m) {
  const blocks = partsOf(m, "reasoning").map((p) => p.data ?? {});
  if (blocks.length === 0) return { thinking: "", started_at: 0, finished_at: 0 };
  let thinking = blocks[0].thinking ?? "";
  let finishedAt = blocks[0].finished_at ?? 0;
  for (const block of blocks.slice(1)) {
    if (block.thinking) thinking = thinking ? `${thinking}\n\n${block.thinking}` : block.thinking;
    if (!block.finished_at) finishedAt = 0;
    else if (finishedAt !== 0) finishedAt = block.finished_at;
  }
  return { thinking, started_at: blocks[0].started_at ?? 0, finished_at: finishedAt };
}

// Message.IsThinking: thinking started and not ended, no reply text yet, and
// the message is not finished.
export function isThinking(m) {
  const r = reasoningOf(m);
  return (r.started_at > 0 || r.thinking !== "") && !r.finished_at && textOf(m) === "" && !finished(m);
}

// The line to show, or "": empty once the message finished or with the
// setting off; a CLI's held-back line (activity_detail) wins over the
// streamed thinking.
export function thoughtLine(m, { enabled = true } = {}) {
  if (!m || finished(m) || !enabled) return "";
  if (m.activity_detail) return cleanThoughtLine(m.activity_detail);
  if (isThinking(m)) return cleanThoughtLine(reasoningOf(m).thinking);
  return "";
}

const MARKS = " \t*_#`";

// The last non-blank line, without the Markdown marks models put around their
// thinking headings.
export function cleanThoughtLine(text) {
  let s = stripAnsi(String(text ?? "")).replace(/[ \t\r\n]+$/, "");
  const at = s.lastIndexOf("\n");
  if (at >= 0) s = s.slice(at + 1);
  let start = 0;
  let end = s.length;
  while (start < end && MARKS.includes(s[start])) start++;
  while (end > start && MARKS.includes(s[end - 1])) end--;
  return s.slice(start, end);
}

// Whether the setting shows the line (options.tui.thought_line, default on:
// internal/config/options.go:24-33).
export function thoughtLineEnabled(config) {
  return config?.options?.tui?.thought_line !== false;
}
