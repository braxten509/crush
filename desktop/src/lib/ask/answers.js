// Sending a question form's answers (desktop-design-5-6 2.6 "Answering").
// `resolved: false` means another app answered first: the form closes
// silently either way.
import { api } from "../engine/api.js";
import { prompts } from "../stores/prompts.svelte.js";
import { closeSlot, notify, requestEnd, ui } from "../stores/ui.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { answerBody } from "./form.js";

// Only this request's form closes: a newer one that arrived meanwhile stays.
function done(request) {
  if (prompts.question?.id === request.id) prompts.question = null;
  if (ui.slot?.kind === "question" && ui.slot.props?.request?.id === request.id) closeSlot("question");
}

export async function submitAnswers(request, answers) {
  try {
    await api.answerQuestions(answerBody(request, answers));
    if (request.session_id === sessions.currentId && ui.follow) requestEnd();
    done(request);
    return true;
  } catch (error) {
    notify("error", error?.message ?? String(error));
    return false;
  }
}

export async function cancelQuestions(request) {
  try {
    await api.cancelQuestions();
  } catch (error) {
    notify("error", error?.message ?? String(error));
  }
  done(request);
}
