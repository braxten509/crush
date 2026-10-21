// The answers behind a "Your answers to the questions" line (owner,
// 2026-10-20: clicking it opens a pop-up with the questions and the answers;
// picked "just your picks"). The line's message holds what the question form
// sent back (internal/agent/tools/question.go formatAnswers): per question
// "Q<n>: <text>", then "User selected: [ids]", "User provided: <text>",
// "User answered: yes|no" or "User skipped this question", then any
// "Notes:" lines. The questions as asked (choice labels) come from the
// newest `crush ask` call before the line; without one, picks show as the ids
// the result names.

const MARKERS = ["User answered:", "User selected:", "User provided:", "User skipped", "Notes:"];

// The <result> part of a task notification, or the whole text.
function resultOf(text) {
  const at = text.indexOf("<result>");
  if (at < 0) return text;
  const rest = text.slice(at + "<result>".length);
  const end = rest.lastIndexOf("</result>");
  return (end < 0 ? rest : rest.slice(0, end)).replace(/^\n/, "");
}

// The answers in a question form's result: [{question, selected, provided,
// yes, skipped, notes: [{choice, text}]}], in order.
export function parseAnswers(text) {
  const out = [];
  let entry = null;
  let mode = ""; // "provided" | "notes"
  const current = () => {
    if (!entry) {
      entry = { question: "", selected: [], provided: "", yes: null, skipped: false, notes: [] };
      out.push(entry);
    }
    return entry;
  };
  for (const line of resultOf(String(text ?? "")).split("\n")) {
    const q = /^Q\d+: (.*)$/.exec(line);
    if (q && mode !== "provided") {
      entry = null;
      current().question = q[1];
      mode = "";
      continue;
    }
    if (line.startsWith("User answered: ")) {
      current().yes = line.slice("User answered: ".length).trim() === "yes";
      mode = "";
    } else if (line.startsWith("User selected: ")) {
      try {
        const ids = JSON.parse(line.slice("User selected: ".length));
        current().selected = Array.isArray(ids) ? ids.map(String) : [];
      } catch {
        current().selected = [];
      }
      mode = "";
    } else if (line.startsWith("User provided: ")) {
      current().provided = line.slice("User provided: ".length);
      mode = "provided";
    } else if (line.startsWith("User skipped")) {
      current().skipped = true;
      mode = "";
    } else if (line === "Notes:") {
      mode = "notes";
    } else if (mode === "notes" && line.startsWith("- ")) {
      const keyed = /^- \[([^\]]*)\]: (.*)$/.exec(line);
      current().notes.push(keyed ? { choice: keyed[1], text: keyed[2] } : { choice: "", text: line.slice(2) });
    } else if (mode === "provided" && !MARKERS.some((m) => line.startsWith(m))) {
      // Free text runs over lines until the next part; a blank line before
      // "Q<n>:" or "Notes:" ends it.
      if (line === "") mode = "provided-gap";
      else current().provided += `\n${line}`;
    } else if (mode === "provided-gap") {
      if (line !== "") current().provided += `\n\n${line}`;
      mode = "provided";
    }
  }
  for (const e of out) e.provided = e.provided.replace(/\s+$/, "");
  return out;
}

// The questions of a `crush ask` command ({questions: [...]} in its
// here-document), or null.
export function askedIn(command) {
  const text = String(command ?? "");
  const at = text.search(/\bcrush ask\b/);
  if (at < 0) return null;
  const doc = /<<-?\s*['"]?(\w+)['"]?[^\n]*\n([\s\S]*?)\n\s*\1\b/.exec(text.slice(at));
  if (!doc) return null;
  try {
    const parsed = JSON.parse(doc[2].trim());
    return Array.isArray(parsed?.questions) ? parsed.questions : null;
  } catch {
    return null;
  }
}

// A tool call's command text: its input's "command", or the input itself.
function commandOf(call) {
  const input = call?.input;
  if (typeof input !== "string") return input?.command ?? "";
  try {
    const parsed = JSON.parse(input);
    return typeof parsed?.command === "string" ? parsed.command : input;
  } catch {
    return input;
  }
}

// The questions of the newest `crush ask` call in earlier (messages, newest
// first), or null.
export function lastAsked(earlier) {
  for (const m of earlier ?? []) {
    if (m?.role !== "assistant") continue;
    const calls = (m.parts ?? []).filter((p) => p?.type === "tool_call").map((p) => p.data ?? {});
    for (let i = calls.length - 1; i >= 0; i--) {
      const asked = askedIn(commandOf(calls[i]));
      if (asked) return asked;
    }
  }
  return null;
}

const labelOf = (question, id) => (question?.choices ?? []).find((c) => String(c?.id) === id)?.label ?? id;

// What the pop-up shows: [{question, picks: [text], skipped, notes: [text]}].
// A question asked with other words than the result's is not trusted for
// labels.
export function answersView(text, earlier) {
  const answers = parseAnswers(text);
  const asked = lastAsked(earlier) ?? [];
  return answers.map((a, i) => {
    const q = asked[i];
    const same = q && (!a.question || String(q.question ?? "") === a.question);
    const picks = a.selected.map((id) => (same ? labelOf(q, id) : id));
    if (a.yes !== null) picks.push(a.yes ? "Yes" : "No");
    if (a.provided) picks.push(a.provided);
    const notes = a.notes.map((n) => (n.choice ? `${same ? labelOf(q, n.choice) : n.choice}: ${n.text}` : n.text));
    return { question: a.question || (same ? String(q.question ?? "") : ""), picks, skipped: a.skipped || (!picks.length && !notes.length), notes };
  });
}
