// The Question tool's header and body (CHAT-072). Port of chat/question.go:36-269
// and the questions decoder of agent/tools/question.go:27-55.
import { collapseSpace, decodeAs, parseJSON, utf8Length, utf8Prefix } from "./go.js";

const CHOICE = { id: "string", label: "string", description: "string", image: "string" };
const QUESTION = {
  label: "string",
  type: "string",
  question: "string",
  description: "string",
  choices: ["array", CHOICE],
  options: ["array", CHOICE],
};

// Models sometimes send the questions as a JSON string holding the array.
function questionsField(value, state) {
  let decoded = decodeAs(value, ["array", QUESTION]);
  if (!decoded.ok && typeof value === "string") {
    const parsed = parseJSON(value.trim());
    decoded = parsed === undefined ? { value: [], ok: false } : decodeAs(parsed, ["array", QUESTION]);
  }
  if (!decoded.ok) state.ok = false;
  return decoded.value;
}
questionsField.zero = [];

export const QUESTION_PARAMS = { questions: questionsField, confirm_title: "string", confirm_description: "string" };

// questionSummary: the first question, cut at 60 bytes; with more questions
// the first cut at 40 bytes and "(+N more)".
export function questionSummary(questions) {
  const n = questions?.length ?? 0;
  if (n === 0) return "";
  if (n === 1) {
    const text = questions[0].question;
    return utf8Length(text) > 60 ? utf8Prefix(text, 59) + "…" : text;
  }
  let first = questions[0].question;
  if (utf8Length(first) > 40) first = utf8Prefix(first, 39) + "…";
  return `${first} (+${n - 1} more)`;
}

// parseQuestionBlocks: the result split at "QN:" lines into {question,
// answer, notes}.
export function questionBlocks(content) {
  const raw = [];
  let current = [];
  for (const line of String(content ?? "").split("\n")) {
    if (line.startsWith("Q") && current.length > 0) {
      const rest = line.slice(1);
      const colon = rest.indexOf(":");
      if (colon > 0 && /^[0-9]+$/.test(rest.slice(0, colon))) {
        raw.push(current.join("\n"));
        current = [];
      }
    }
    current.push(line);
  }
  if (current.length > 0) raw.push(current.join("\n"));
  const blocks = [];
  for (let text of raw) {
    text = goTrim(text);
    if (text === "") continue;
    const block = { question: "", answer: "", notes: [] };
    const nl = text.indexOf("\n");
    if (nl >= 0) {
      block.question = goTrim(text.slice(0, nl));
      const colon = block.question.indexOf(": ");
      if (colon >= 0) block.question = block.question.slice(colon + 2);
      text = text.slice(nl + 1);
    }
    const notesAt = text.indexOf("\n\nNotes:");
    if (notesAt >= 0) {
      block.answer = goTrim(text.slice(0, notesAt));
      for (let note of text.slice(notesAt + "\n\nNotes:".length).split("\n")) {
        note = goTrim(note);
        if (note.startsWith("- ")) block.notes.push(note.slice(2));
      }
    } else block.answer = goTrim(text);
    blocks.push(block);
  }
  return blocks;
}

function goTrim(text) {
  return text.replace(/^[\t\n\v\f\r \u0085 ]+|[\t\n\v\f\r \u0085 ]+$/g, "");
}

const MARKERS = ["User answered:", "User selected:", "User provided:", "User skipped"];

// styleAnswer: the answer's parts, each {text, tone}: "yes" (Yes), "no" (No),
// "skipped" (Skipped), "value" (selected choices or typed text), "plain".
export function answerParts(answer) {
  const segments = [];
  let current = [];
  for (const line of goTrim(String(answer ?? "")).split("\n")) {
    if (MARKERS.some((m) => line.startsWith(m)) && current.length > 0) {
      segments.push(current.join("\n"));
      current = [];
    }
    current.push(line);
  }
  if (current.length > 0) segments.push(current.join("\n"));
  const parts = [];
  for (let seg of segments) {
    seg = goTrim(seg);
    if (seg === "") continue;
    if (seg === "User answered: yes") parts.push({ text: "Yes", tone: "yes" });
    else if (seg === "User answered: no") parts.push({ text: "No", tone: "no" });
    else if (seg.startsWith("User selected:")) {
      let selected = seg.startsWith("User selected: ") ? seg.slice("User selected: ".length) : seg;
      selected = selected.replace(/^[[\]"]+|[[\]"]+$/g, "").replaceAll('","', ", ");
      parts.push({ text: selected, tone: "value" });
    } else if (seg.startsWith("User provided:")) {
      const text = seg.startsWith("User provided: ") ? seg.slice("User provided: ".length) : seg;
      parts.push({ text: collapseSpace(text), tone: "value" });
    } else if (seg === "User skipped this question") parts.push({ text: "Skipped", tone: "skipped" });
    else parts.push({ text: seg, tone: "plain" });
  }
  return parts;
}

// The body: one "✓ question answer" line per block, then its "╰ note" lines.
export function questionBody(content) {
  if (!content) return null;
  const blocks = questionBlocks(content);
  if (blocks.length === 0) return null;
  return { type: "questions", blocks: blocks.map((b) => ({ ...b, parts: answerParts(b.answer) })) };
}
