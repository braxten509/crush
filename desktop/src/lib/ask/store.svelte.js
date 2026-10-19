// ASK's open forms (desktop-design-5-6 2.6 "Arrival", 2.7 "Arrival"): the
// secure forms this window holds, one per form_id and metadata only (labels
// and statuses; values never come here), and the watcher that puts the shown
// chat's question batch or secure form in the typing box's place. A form that
// waits behind another slot form opens when the slot frees (S5's slot rule).
import SecureForm from "../../components/ask/SecureForm.svelte";
import QuestionForm from "../../components/ask/QuestionForm.svelte";
import { api } from "../engine/api.js";
import { prompts } from "../stores/prompts.svelte.js";
import { rootOf, sessions } from "../stores/sessions.svelte.js";
import { closeSlot, openSlot, ui } from "../stores/ui.svelte.js";

export const ask = $state({
  secure: {}, // form_id → proto.SecureForm
});

const rootFor = (sid) => rootOf(sid) || String(sid ?? "");

// The held secure form for the shown chat, oldest first.
export function shownSecureForm() {
  const shown = rootFor(sessions.currentId);
  if (!shown) return null;
  return Object.values(ask.secure).find((form) => rootFor(form.session_id) === shown) ?? null;
}

// A repeat for a held form_id changes nothing (u:ask.secure_repeat).
export function holdSecureForm(form) {
  if (!form?.form_id || ask.secure[form.form_id]) return;
  ask.secure[form.form_id] = form;
}

export function dropSecureForm(formId) {
  if (!ask.secure[formId]) return;
  delete ask.secure[formId];
  if (ui.slot?.kind === "secure" && ui.slot.props?.form?.form_id === formId) closeSlot("secure");
}

// GET secure-forms: the open forms for this window, replacing the held ones
// (a form already held takes the new field statuses).
export async function readSecureForms() {
  let list;
  try {
    list = await api.secureForms();
  } catch {
    return;
  }
  const open = new Map((Array.isArray(list) ? list : []).map((form) => [form.form_id, form]));
  for (const id of Object.keys(ask.secure)) if (!open.has(id)) dropSecureForm(id);
  for (const [id, form] of open) {
    if (ask.secure[id]) ask.secure[id].fields = form.fields;
    else ask.secure[id] = form;
  }
}

let questionRead = "";

// G-12: the shown chat waits for a question this window has not seen.
async function readPendingQuestion(sid) {
  questionRead = sid;
  try {
    const request = await api.pendingQuestion(sid);
    if (request?.id && sessions.currentId === sid && !prompts.question) prompts.question = request;
  } catch {
    // No route returns a pending request yet (2.6 "Arrival").
  }
}

let started = false;

export function watchAsk() {
  if (started) return;
  started = true;
  $effect.root(() => {
    // The question batch takes the slot; a newer one replaces it.
    $effect(() => {
      const request = prompts.question;
      const slot = ui.slot;
      if (!request) {
        if (slot?.kind === "question") closeSlot("question");
        return;
      }
      if (slot?.kind === "question" && slot.props?.request?.id === request.id) return;
      openSlot({ kind: "question", component: QuestionForm, props: { request } });
    });

    // The shown chat's secure form, once nothing else holds the slot.
    $effect(() => {
      const form = shownSecureForm();
      const slot = ui.slot;
      if (!form) {
        if (slot?.kind === "secure") closeSlot("secure");
        return;
      }
      if (slot?.kind === "secure" && slot.props?.form?.form_id === form.form_id) return;
      if (slot && slot.kind !== "secure" && (slot.kind === "question" || slot.kind === "permission")) return;
      openSlot({ kind: "secure", component: SecureForm, props: { form } });
    });

    // A chat switch: its open question (G-12) and the window's secure forms.
    $effect(() => {
      const sid = sessions.currentId;
      const waiting = sid ? sessions.byId[sid]?.pending_prompt === "question" : false;
      if (!sid) return;
      if (waiting && !prompts.question && questionRead !== sid) readPendingQuestion(sid);
      if (!waiting && questionRead === sid) questionRead = "";
    });
    $effect(() => {
      if (sessions.currentId) readSecureForms();
    });
  });
}
