// A question form's state (desktop-design-5-6 2.6; a port of
// internal/ui/dialog/questionform): one state per question, the answers
// taken when a question is answered or left, the tab labels, and what counts
// as answered. Pure functions over plain objects; the components keep them
// in $state.

export const CUSTOM_MAX = 500; // "Something else?" (DLG-166)
export const NOTE_MAX = 300; // Alt+N notes (DLG-168)
export const FREE_MAX = 1000; // free text (DLG-173)
export const KNOWN = new Set(["yes_no", "single_choice", "multi_choice", "free_text", "secure_entry"]);

// An unknown type shows as free text (form.go:109).
export const typeOf = (item) => (KNOWN.has(item?.type) ? item.type : "free_text");

// shortLabel (form.go:205-214): the first three words.
export function shortLabel(text) {
  return String(text ?? "").replaceAll("\n", " ").split(/\s+/).filter(Boolean).slice(0, 3).join(" ");
}

// The tab labels: each item's label or its first three words, then
// "Confirm" for a batch (form.go:130).
export function tabLabels(request) {
  const items = request?.questions ?? [];
  const labels = items.map((item) => item.label || shortLabel(item.question));
  return items.length > 1 ? [...labels, "Confirm"] : labels;
}

export function initialState(item) {
  const choices = item?.choices ?? [];
  const pre = new Set((item?.selected ?? []).filter((id) => choices.some((c) => c.id === id)));
  return {
    cursor: 0, // a choice's index, or choices.length for "Something else?"
    no: true, // yes/no starts on No (DLG-155)
    picked: choices.map((c) => pre.has(c.id)), // multi choice (G-13 preselects)
    custom: "", // "Something else?"
    text: "", // free text
    notes: {}, // choice id (or the question id) → note
  };
}

const withNotes = (resp, st) => (Object.keys(st.notes).length > 0 ? { ...resp, notes: { ...st.notes } } : resp);

// question.Answer for the item as its state stands (single.go:113-126,
// multi.go:139-155, yesno.go:110-120, freetext.go:99-104).
export function respond(item, st, { yes = null } = {}) {
  const base = { request_id: item.id, selected_ids: [], fill_in_text: "" };
  const choices = item.choices ?? [];
  switch (typeOf(item)) {
    case "yes_no":
      return withNotes({ ...base, yes: yes ?? !st.no }, st);
    case "single_choice": {
      const resp = { ...base };
      if (st.cursor < choices.length && choices.length > 0) resp.selected_ids = [choices[st.cursor].id];
      const custom = st.custom.trim();
      if (custom) resp.fill_in_text = custom;
      return withNotes(resp, st);
    }
    case "multi_choice": {
      const resp = { ...base, selected_ids: choices.filter((_, i) => st.picked[i]).map((c) => c.id) };
      const custom = st.custom.trim();
      if (custom) resp.fill_in_text = custom;
      return withNotes(resp, st);
    }
    default: {
      const text = st.text.trim();
      return text ? { ...base, fill_in_text: text } : null;
    }
  }
}

// isAnswered (form.go:221-233).
export function isAnswered(resp) {
  return Boolean(resp) && (resp.selected_ids.length > 0 || resp.fill_in_text !== "" || typeof resp.yes === "boolean");
}

// The QuestionAnswer body (form.go:411-432): one response per question, an
// empty one where nothing was answered; secure questions never carry a value.
export function answerBody(request, answers) {
  const items = request.questions ?? [];
  return {
    batch_request_id: request.id,
    responses: items.map((item, i) => {
      const resp = typeOf(item) === "secure_entry" ? null : answers[i];
      return resp ? resp : { request_id: item.id, selected_ids: [], fill_in_text: "" };
    }),
  };
}

// The Confirm page's bullets (confirm.go:59-64): labels resolved, secure
// answers as status only, notes not shown.
export function summaryLine(item, resp, secureStatus = "") {
  const label = item.label || shortLabel(item.question);
  if (typeOf(item) === "secure_entry") return `${label}: ${secureStatus || "(not entered)"}`;
  if (!isAnswered(resp)) return `${label}: (not answered)`;
  if (typeof resp.yes === "boolean") return `${label}: ${resp.yes ? "Yes" : "No"}`;
  const names = resp.selected_ids.map((id) => item.choices?.find((c) => c.id === id)?.label ?? id);
  if (resp.fill_in_text) names.push(resp.fill_in_text);
  return `${label}: ${names.join(", ")}`;
}
